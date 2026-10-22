package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if handled, err := HandleHelper(context.Background()); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testPlan(t *testing.T) installPlan {
	t.Helper()
	d := t.TempDir()
	target := filepath.Join(d, "app")
	work := filepath.Join(d, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "candidate"), []byte("new"), 0700); err != nil {
		t.Fatal(err)
	}
	old, _ := fileDigest(context.Background(), target)
	next, _ := fileDigest(context.Background(), filepath.Join(work, "candidate"))
	return installPlan{Options: RestartOptions{Mode: PortableInstall, Target: target}, Work: work, OldDigest: old, NewDigest: next, Relaunch: target}
}

func TestInstallPreservesTargetAndBackup(t *testing.T) {
	for _, scenario := range []string{"success", "changed-target", "changed-candidate", "replace-failure", "launch-failure", "restore-failure"} {
		t.Run(scenario, func(t *testing.T) {
			p := testPlan(t)
			moves := 0
			launches := 0
			injected := errors.New("injected failure")
			if scenario == "changed-target" {
				_ = os.WriteFile(p.Options.Target, []byte("foreign"), 0700)
			}
			if scenario == "changed-candidate" {
				_ = os.WriteFile(filepath.Join(p.Work, "candidate"), []byte("tampered"), 0700)
			}
			state, err := install(p, func(src, dst string) error {
				moves++
				if (scenario == "replace-failure" || scenario == "restore-failure") && moves == 2 {
					return injected
				}
				if scenario == "restore-failure" && moves == 3 {
					return injected
				}
				return os.Rename(src, dst)
			}, func(p installPlan) error {
				launches++
				if scenario == "launch-failure" && launches == 1 {
					return injected
				}
				return nil
			})
			b, readErr := os.ReadFile(p.Options.Target)
			switch scenario {
			case "success":
				if err != nil || state != "installed" || string(b) != "new" || launches != 1 {
					t.Fatalf("%s %v %q %d", state, err, b, launches)
				}
				backup, e := os.ReadFile(filepath.Join(p.Work, "backup"))
				if e != nil || string(backup) != "old" {
					t.Fatal("missing recovery backup")
				}
			case "changed-target":
				if !errors.Is(err, ErrTargetChanged) || string(b) != "foreign" || launches != 0 {
					t.Fatalf("%s %v %q %d", state, err, b, launches)
				}
			case "changed-candidate":
				if !errors.Is(err, ErrInvalid) || string(b) != "old" {
					t.Fatalf("%s %v %q", state, err, b)
				}
			case "replace-failure", "launch-failure":
				if !errors.Is(err, injected) || state != "rolled-back" || string(b) != "old" {
					t.Fatalf("%s %v %q", state, err, b)
				}
			case "restore-failure":
				if !errors.Is(err, injected) || !os.IsNotExist(readErr) {
					t.Fatalf("%s %v %v", state, err, readErr)
				}
				if _, e := os.Stat(filepath.Join(p.Work, "backup")); e != nil {
					t.Fatal("lost old application", e)
				}
			}
		})
	}
}

func TestScheduleRefusesUnsafeTargetAndManagedInstall(t *testing.T) {
	f := newFixture(t)
	r, err := f.c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, opts := range []RestartOptions{{Mode: ManagedInstall}, {Mode: PortableInstall, Target: "relative"}, {Mode: PortableInstall, Target: filepath.Join(t.TempDir(), "absent")}} {
		if _, err = s.Schedule(context.Background(), opts); err == nil {
			t.Fatal("accepted", opts)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Schedule(ctx, RestartOptions{Mode: PortableInstall}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.mu.Lock()
	if _, err = s.Schedule(context.Background(), RestartOptions{Mode: PortableInstall}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	s.mu.Unlock()
}

// This is an actual consumer-shaped process. The helper is an independent copy
// of its executable; the replacement is the test executable with new launch
// arguments. It verifies the old process still owns the target before exit.
func TestUpdaterWorker(t *testing.T) {
	mode := os.Getenv("TUOHI_UPDATER_WORKER")
	if mode == "" {
		return
	}
	dir := os.Getenv("TUOHI_UPDATER_DIR")
	if mode == "relaunch" {
		if err := os.WriteFile(filepath.Join(dir, "launched"), []byte(strings.Join(os.Args[1:], "\n")), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	f := newFixture(t)
	// t.TempDir cleanup must not remove a handed-off stage on parent exit.
	stageRoot := filepath.Join(dir, "stage")
	if err := os.Mkdir(stageRoot, 0700); err != nil {
		t.Fatal(err)
	}
	f.c.cfg.StageDir = stageRoot
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	f.data = data
	h := sha256.Sum256(data)
	f.m.Assets[0].Size = int64(len(data))
	f.m.Assets[0].SHA256 = hex.EncodeToString(h[:])
	f.envelope = f.sign(t, f.m)
	r, err := f.c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The relaunched app inherits this environment, but explicit args still
	// travel through the private helper plan and must survive quoting intact.
	if err = os.Setenv("TUOHI_UPDATER_WORKER", "relaunch"); err != nil {
		t.Fatal(err)
	}
	p, err := s.Schedule(context.Background(), RestartOptions{Mode: PortableInstall, Target: target, Args: []string{"-test.run=TestUpdaterWorker", "--", "a room with spaces", "literal;$value"}, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "result-path"), []byte(p.ResultPath()), 0600); err != nil {
		t.Fatal(err)
	}
	if mode == "cancel" {
		if err = p.Cancel(); err != nil {
			t.Fatal(err)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	// While this process is alive the helper must not move the executable.
	time.Sleep(200 * time.Millisecond)
	if _, err = os.Stat(filepath.Join(filepath.Dir(p.ResultPath()), "backup")); !os.IsNotExist(err) {
		t.Fatal("helper installed before parent exit", err)
	}
	// No os.Exit here: testing's ordinary return exits the worker process.
}

func TestPortableSubprocessRestart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real subprocess install exercised on native Linux; other platforms need native runners")
	}
	for _, mode := range []string{"install", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			d := t.TempDir()
			current, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(d, "desktop")
			if _, err = copyFile(context.Background(), current, target, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, target, "-test.run=TestUpdaterWorker")
			cmd.Env = append(os.Environ(), "TUOHI_UPDATER_WORKER="+mode, "TUOHI_UPDATER_DIR="+d)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("worker: %v\n%s", err, output)
			}
			resultPath, err := os.ReadFile(filepath.Join(d, "result-path"))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				if _, err = os.Stat(filepath.Join(d, "launched")); !os.IsNotExist(err) {
					t.Fatal("cancelled helper relaunched", err)
				}
				return
			}
			var result Result
			for {
				result, err = ReadResult(string(resultPath))
				_, launched := os.Stat(filepath.Join(d, "launched"))
				if err == nil && launched == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("helper completion: %v %v", err, ctx.Err())
				case <-time.After(20 * time.Millisecond):
				}
			}
			if result.State != "installed" || result.Sequence != 2 {
				t.Fatalf("%+v", result)
			}
			args, err := os.ReadFile(filepath.Join(d, "launched"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), "a room with spaces\nliteral;$value") {
				t.Fatalf("arguments changed: %q", args)
			}
			if _, err = os.Stat(filepath.Join(filepath.Dir(string(resultPath)), "backup")); err != nil {
				t.Fatal("no rollback backup", err)
			}
		})
	}
}

func TestNativeWatchCancellation(t *testing.T) {
	w, err := watchProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err = w.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestInstallationLockSerializesStages(t *testing.T) {
	target := filepath.Join(t.TempDir(), "app")
	a, err := lockInstall(target)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := lockInstall(target); !errors.Is(err, ErrBusy) {
		if b != nil {
			_ = b.Close()
		}
		t.Fatal("overlapping installer accepted", err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := lockInstall(target)
	if err != nil {
		t.Fatal("lock retained after helper exit", err)
	}
	_ = b.Close()
}
