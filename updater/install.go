package updater

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// InstallMode must be chosen explicitly by the consumer's packaging code.
type InstallMode string

const (
	PortableInstall InstallMode = "portable"
	BundleInstall   InstallMode = "bundle"
	ManagedInstall  InstallMode = "managed"
	helperFlag                  = "--tuohi-update-helper-v1"
	helperReady                 = "tuohi-update-ready"
)

// RestartOptions describes a consumer-owned installation and the next launch.
// Target is the absolute installed executable (PortableInstall), or .app root
// (BundleInstall). Args excludes argv[0]; it is explicit to avoid replaying
// one-shot actions or credentials from the original command line. Environment is
// inherited. WorkingDir defaults to Target's parent. Timeout bounds the helper's
// wait for parent exit (default five minutes, maximum one hour).
type RestartOptions struct {
	Mode       InstallMode
	Target     string
	Args       []string
	WorkingDir string
	Timeout    time.Duration
}

// Pending is a successfully prepared restart. Cancel aborts it while the parent
// remains alive. Do not defer Cancel when proceeding with shutdown. ResultPath
// names a private result/recovery record retained beside the installation. The
// caller owns removal after inspecting it and any retained backup.
type Pending struct {
	cmd    *exec.Cmd
	done   chan struct{}
	mu     sync.Mutex
	err    error
	result string
	stage  string
	work   string
}

func (p *Pending) ResultPath() string    { return p.result }
func (p *Pending) Done() <-chan struct{} { return p.done }

// Cancel kills the waiting helper and waits for its exit. It never tears down
// the application or its windows. Once the parent exits it cannot call Cancel.
func (p *Pending) Cancel() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	err := p.cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		err = nil
	}
	<-p.done
	return errors.Join(err, os.RemoveAll(p.stage), os.RemoveAll(p.work))
}

// Wait waits for the helper to finish, without cancelling it on ctx expiry.
func (p *Pending) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.err
	}
}

// Result is an installation outcome, not an authenticated release source. Never
// use its sequence alone as a trusted rollback floor; compare the running
// release's compiled sequence. RecoveryDir may contain a backup after failure.
type Result struct {
	State       string `json:"state"`
	Version     string `json:"version"`
	Sequence    uint64 `json:"sequence"`
	Error       string `json:"error,omitempty"`
	RecoveryDir string `json:"recovery_dir"`
}

// ReadResult reads at most 64 KiB from a consumer-selected result record.
func ReadResult(path string) (Result, error) {
	var r Result
	f, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if err != nil {
		return r, err
	}
	if len(b) > 64<<10 {
		return r, ErrInvalid
	}
	err = strictJSON(b, &r)
	return r, err
}

type installPlan struct {
	Parent    int
	Options   RestartOptions
	Config    Config
	Envelope  []byte
	Stage     string
	Work      string
	OldDigest string
	NewDigest string
	Relaunch  string
	Deadline  time.Time
}

// Schedule prepares a same-filesystem replacement and starts a copy of the
// CURRENT executable as a helper. Every consumer must call HandleHelper before
// normal startup. Success means the helper is watching a stable native process
// lifetime; it does not mean installation is complete. The consumer must then
// save state, quit normally, release single-instance ownership and exit. ctx only
// governs preparation/readiness; cancellation after success requires Cancel.
// Concurrent Schedule calls on one stage return ErrBusy rather than queueing.
func (s *Staged) Schedule(ctx context.Context, opts RestartOptions) (_ *Pending, err error) {
	if !s.mu.TryLock() {
		return nil, ErrBusy
	}
	defer s.mu.Unlock()
	if s.closed || s.scheduled {
		return nil, ErrBusy
	}
	if opts.Mode == ManagedInstall {
		return nil, ErrManaged
	}
	if opts.Mode != PortableInstall && opts.Mode != BundleInstall {
		return nil, ErrUnsupported
	}
	if (opts.Mode == PortableInstall && s.r.asset.Kind != Executable) || (opts.Mode == BundleInstall && (s.r.asset.Kind != AppBundle || runtime.GOOS != "darwin")) {
		return nil, ErrUnsupported
	}
	if _, err = s.r.c.verify(s.r.envelope, time.Now()); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(opts.Target) {
		return nil, fmt.Errorf("%w: absolute target required", ErrInvalid)
	}
	opts.Target = filepath.Clean(opts.Target)
	if opts.Mode == BundleInstall {
		if err = checkBundleStage(opts.Target, s.dir); err != nil {
			return nil, err
		}
	}
	if opts.Mode == PortableInstall && runtime.GOOS == "darwin" && strings.Contains(opts.Target, ".app"+string(filepath.Separator)) {
		return nil, ErrUnsupported
	}
	current, err := os.Executable()
	if err != nil {
		return nil, err
	}
	runningTarget := opts.Target
	if opts.Mode == BundleInstall {
		runningTarget, err = bundleExecutable(ctx, opts.Target, s.r.c.cfg.Application)
		if err != nil {
			return nil, err
		}
	}
	oldInfo, err := regularFile(runningTarget)
	if err != nil {
		return nil, err
	}
	currentInfo, err := os.Stat(current)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(oldInfo, currentInfo) {
		return nil, fmt.Errorf("%w: target is not the running executable", ErrInvalid)
	}
	if oldInfo.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return nil, ErrUnsupported
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Timeout < time.Second || opts.Timeout > time.Hour {
		return nil, fmt.Errorf("%w: timeout", ErrInvalid)
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = filepath.Dir(opts.Target)
	}
	if !filepath.IsAbs(opts.WorkingDir) {
		return nil, fmt.Errorf("%w: working directory", ErrInvalid)
	}
	opts.Args = append([]string(nil), opts.Args...)
	work, err := os.MkdirTemp(filepath.Dir(opts.Target), ".tuohi-update-")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(work)
		}
	}()
	mode := oldInfo.Mode().Perm()
	if runtime.GOOS != "windows" && mode&0111 == 0 {
		return nil, ErrUnsupported
	}
	artifact := filepath.Join(s.dir, "artifact")
	artifactHash, err := fileDigest(ctx, artifact)
	if err != nil {
		return nil, err
	}
	if artifactHash != s.r.asset.SHA256 {
		return nil, fmt.Errorf("%w: staged artifact changed", ErrInvalid)
	}
	var newHash string
	if opts.Mode == BundleInstall {
		candidate := filepath.Join(work, "candidate.app")
		if err = extractBundle(ctx, artifact, candidate); err != nil {
			return nil, err
		}
		if _, err = validateBundle(ctx, opts.Target, candidate, s.r.c.cfg.Application); err != nil {
			return nil, err
		}
		newHash, err = treeDigest(ctx, candidate)
	} else {
		newHash, err = copyFile(ctx, artifact, filepath.Join(work, "candidate"), mode)
	}
	if err != nil {
		return nil, err
	}
	oldHash, err := installDigest(ctx, opts.Mode, opts.Target)
	if err != nil {
		return nil, err
	}
	helper := filepath.Join(s.dir, "helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	helperRoot := helper
	if opts.Mode == BundleInstall {
		helperRoot = filepath.Join(s.dir, "helper.app")
		if err = cloneBundle(ctx, opts.Target, helperRoot); err != nil {
			return nil, err
		}
		helper, err = validateBundle(ctx, helperRoot, opts.Target, s.r.c.cfg.Application)
		if err != nil {
			_ = os.RemoveAll(helperRoot)
			return nil, err
		}
	} else {
		_, err = copyFile(ctx, runningTarget, helper, 0700)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		if !keep {
			_ = os.RemoveAll(helperRoot)
		}
	}()
	cfg := s.r.c.cfg
	cfg.Fetch = nil
	cfg.StageDir = ""
	stage, err := filepath.Abs(s.dir)
	if err != nil {
		return nil, err
	}
	p := installPlan{Parent: os.Getpid(), Options: opts, Config: cfg, Envelope: s.r.envelope, Stage: stage, Work: work, OldDigest: oldHash, NewDigest: newHash, Relaunch: runningTarget, Deadline: time.Now().Add(opts.Timeout)}
	pending, err := startHelper(ctx, helper, p)
	if err != nil {
		return nil, err
	}
	s.scheduled = true
	keep = true
	return pending, nil
}

// Config's callback is process-local and is never included in a helper plan.
// A separate wire type avoids serializing funcs while preserving pinned keys.
type wireConfig struct {
	Application, Channel string
	CurrentSequence      uint64
	Keys                 map[string]ed25519.PublicKey
	Kind                 Kind
	MaxArtifactSize      int64
}
type wirePlan struct {
	Parent                                      int
	Options                                     RestartOptions
	Config                                      wireConfig
	Envelope                                    []byte
	Stage, Work, OldDigest, NewDigest, Relaunch string
	Deadline                                    time.Time
}

func (p installPlan) wire() wirePlan {
	c := p.Config
	return wirePlan{p.Parent, p.Options, wireConfig{c.Application, c.Channel, c.CurrentSequence, c.Keys, c.Kind, c.MaxArtifactSize}, p.Envelope, p.Stage, p.Work, p.OldDigest, p.NewDigest, p.Relaunch, p.Deadline}
}
func (p wirePlan) plan() installPlan {
	c := p.Config
	return installPlan{p.Parent, p.Options, Config{Application: c.Application, Channel: c.Channel, CurrentSequence: c.CurrentSequence, Keys: c.Keys, Kind: c.Kind, MaxArtifactSize: c.MaxArtifactSize}, p.Envelope, p.Stage, p.Work, p.OldDigest, p.NewDigest, p.Relaunch, p.Deadline}
}

func startHelper(ctx context.Context, path string, p installPlan) (*Pending, error) {
	b, err := json.Marshal(p.wire())
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, helperFlag)
	cmd.Stdin = bytes.NewReader(b)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, err
	}
	pending := &Pending{cmd: cmd, done: make(chan struct{}), result: filepath.Join(p.Work, "result.json"), stage: p.Stage, work: p.Work}
	ready := make(chan error, 1)
	go func() {
		line, e := bufio.NewReader(stdout).ReadString('\n')
		if e == nil && line != helperReady+"\n" {
			switch line {
			case "tuohi-update-error:busy\n":
				e = ErrBusy
			case "tuohi-update-error:unsupported\n":
				e = ErrUnsupported
			default:
				e = ErrInvalid
			}
		}
		ready <- e
	}()
	// The reader owns stdout until readiness; Wait cannot close it prematurely.
	prepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case err = <-ready:
	case <-prepCtx.Done():
		err = prepCtx.Err()
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stdout.Close()
		return nil, fmt.Errorf("tuohi: updater: helper readiness: %w", err)
	}
	go func() { e := cmd.Wait(); pending.mu.Lock(); pending.err = e; pending.mu.Unlock(); close(pending.done) }()
	return pending, nil
}

// HandleHelper must be called at the very beginning of main, before flags,
// windows, servers or single-instance acquisition. When handled is true, return
// from main after reporting err; never continue normal startup. A helper reads
// its private plan from stdin, watches its parent, installs and relaunches.
// It never calls os.Exit. Normal invocation returns (false,nil) immediately.
func HandleHelper(ctx context.Context) (handled bool, err error) {
	if len(os.Args) != 2 || os.Args[1] != helperFlag {
		return false, nil
	}
	return true, runHelper(ctx, os.Stdin, os.Stdout)
}

func runHelper(ctx context.Context, in io.Reader, out io.Writer) (err error) {
	ready := false
	defer func() {
		if err != nil && !ready {
			code := "invalid"
			if errors.Is(err, ErrBusy) {
				code = "busy"
			}
			if errors.Is(err, ErrUnsupported) {
				code = "unsupported"
			}
			_, _ = fmt.Fprintln(out, "tuohi-update-error:"+code)
		}
	}()
	b, err := io.ReadAll(io.LimitReader(in, 2*manifestLimit+1))
	if err != nil {
		return err
	}
	var wire wirePlan
	if len(b) > 2*manifestLimit || strictJSON(b, &wire) != nil {
		return ErrInvalid
	}
	p := wire.plan()
	if p.Parent < 1 || p.Parent == os.Getpid() || p.Parent != os.Getppid() || !filepath.IsAbs(p.Work) || !filepath.IsAbs(p.Stage) || !filepath.IsAbs(p.Options.Target) || (p.Options.Mode != PortableInstall && p.Options.Mode != BundleInstall) || p.Deadline.After(time.Now().Add(time.Hour)) {
		return ErrInvalid
	}
	ctx, cancel := context.WithDeadline(ctx, p.Deadline)
	defer cancel()
	lock, err := lockInstall(p.Options.Target)
	if err != nil {
		return err
	}
	defer lock.Close()
	watch, err := watchProcess(p.Parent)
	if err != nil {
		return err
	}
	defer watch.Close()
	// A private result record intentionally contains no args, env or metadata URL.
	result := Result{State: "failed", RecoveryDir: p.Work}
	defer func() {
		if err != nil {
			result.Error = err.Error()
		}
		_ = writeResult(filepath.Join(p.Work, "result.json"), result)
	}()
	c := &Client{cfg: p.Config}
	r, err := c.verify(p.Envelope, time.Now())
	if err != nil {
		return err
	}
	result.Version = r.Version()
	result.Sequence = r.Sequence()
	if (p.Options.Mode == PortableInstall && (r.asset.Kind != Executable || r.asset.SHA256 != p.NewDigest || p.Relaunch != p.Options.Target)) || (p.Options.Mode == BundleInstall && (r.asset.Kind != AppBundle || runtime.GOOS != "darwin")) {
		return ErrInvalid
	}
	if p.Options.Mode == BundleInstall {
		expected, e := bundleExecutable(ctx, p.Options.Target, p.Config.Application)
		if e != nil {
			return e
		}
		if p.Relaunch != expected {
			return ErrInvalid
		}
	}
	artifactHash, err := fileDigest(ctx, filepath.Join(p.Stage, "artifact"))
	if err != nil {
		return err
	}
	if artifactHash != r.asset.SHA256 {
		return ErrInvalid
	}
	if _, err = fmt.Fprintln(out, helperReady); err != nil {
		return err
	}
	ready = true
	if err = watch.Wait(ctx); err != nil {
		result.State = "cancelled"
		return err
	}
	if _, err = c.verify(p.Envelope, time.Now()); err != nil {
		result.State = "rolled-back"
		return restartOld(p, err, startInstalled)
	}
	result.State, err = install(p, os.Rename, startInstalled)
	if err == nil {
		// Windows cannot unlink its running helper image. Keep the private stage
		// there; the consumer may reap it after helper exit. Other systems clean it.
		if runtime.GOOS != "windows" {
			_ = os.RemoveAll(p.Stage)
		}
	}
	return err
}

func startInstalled(p installPlan) error {
	cmd := exec.Command(p.Relaunch, p.Options.Args...)
	cmd.Dir = p.Options.WorkingDir
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// install retains its private work directory and backup on uncertain failure.
// Ordinary failures restore and relaunch the old installation. A power loss
// between directory moves can require manual restoration from backup.
func install(p installPlan, move func(string, string) error, start func(installPlan) error) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	old, err := installDigest(ctx, p.Options.Mode, p.Options.Target)
	if err != nil {
		return "failed", err
	}
	if old != p.OldDigest {
		return "failed", ErrTargetChanged
	}
	candidate := candidatePath(p)
	backup := backupPath(p)
	newHash, err := installDigest(ctx, p.Options.Mode, candidate)
	if err != nil {
		return "rolled-back", restartOld(p, err, start)
	}
	if newHash != p.NewDigest {
		return "rolled-back", restartOld(p, fmt.Errorf("%w: prepared candidate changed", ErrInvalid), start)
	}
	if p.Options.Mode == BundleInstall {
		if _, err = validateBundle(ctx, p.Options.Target, candidate, p.Config.Application); err != nil {
			return "rolled-back", restartOld(p, err, start)
		}
	}
	if _, err = os.Lstat(backup); !os.IsNotExist(err) {
		return "rolled-back", restartOld(p, fmt.Errorf("%w: backup exists", ErrInvalid), start)
	}
	if err = move(p.Options.Target, backup); err != nil {
		return "rolled-back", restartOld(p, err, start)
	}
	if err = move(candidate, p.Options.Target); err != nil {
		if restore := move(backup, p.Options.Target); restore != nil {
			return "failed", errors.Join(err, fmt.Errorf("restore backup: %w", restore))
		}
		return "rolled-back", errors.Join(err, start(p))
	}
	if err = start(p); err != nil {
		if undo := move(p.Options.Target, candidate); undo != nil {
			return "launch-failed", errors.Join(err, undo)
		}
		if restore := move(backup, p.Options.Target); restore != nil {
			return "failed", errors.Join(err, restore)
		}
		return "rolled-back", errors.Join(err, start(p))
	}
	// Retain backup until a subsequent successful normal launch can decide to
	// remove it. Start succeeding does not establish application health.
	return "installed", nil
}

func restartOld(p installPlan, cause error, start func(installPlan) error) error {
	h, err := installDigest(context.Background(), p.Options.Mode, p.Options.Target)
	if err != nil {
		return errors.Join(cause, err)
	}
	if h != p.OldDigest {
		return errors.Join(cause, ErrTargetChanged)
	}
	return errors.Join(cause, start(p))
}

func regularFile(path string) (os.FileInfo, error) {
	i, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: regular file required", ErrInvalid)
	}
	return i, nil
}

func fileDigest(ctx context.Context, path string) (string, error) {
	if _, err := regularFile(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, &contextReader{ctx: ctx, r: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(ctx context.Context, src, dst string, mode os.FileMode) (string, error) {
	if _, err := regularFile(src); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		_ = out.Close()
		if !complete {
			_ = os.Remove(dst)
		}
	}()
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(out, h), &contextReader{ctx: ctx, r: in}); err != nil {
		return "", err
	}
	if err = out.Chmod(mode.Perm()); err != nil {
		return "", err
	}
	if err = out.Sync(); err != nil {
		return "", err
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	complete = true
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeResult(path string, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "result-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
