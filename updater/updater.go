// Package updater verifies, stages and installs explicitly selected desktop
// updates. It is optional: the window package does not import it. Consumers own
// transport, consent, release policy and graceful application shutdown.
//
// Metadata and artifacts are authenticated with pinned Ed25519 keys. This is a
// single-publisher protocol, not a threshold-signature or key-recovery system.
// See README.md for the protocol, installation modes and threat model.
package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid       = errors.New("tuohi: updater: invalid update")
	ErrNoUpdate      = errors.New("tuohi: updater: no newer update")
	ErrUnsupported   = errors.New("tuohi: updater: unsupported installation")
	ErrManaged       = errors.New("tuohi: updater: installation managed externally")
	ErrBusy          = errors.New("tuohi: updater: operation already in progress")
	ErrTargetChanged = errors.New("tuohi: updater: installation changed")
)

// Kind identifies the signed artifact's format. Archives other than the
// specified bundle ZIP are not executable artifacts.
type Kind string

const (
	Executable      Kind = "executable"
	AppBundle       Kind = "app-bundle-zip"
	manifestLimit        = 1 << 20
	signatureDomain      = "tuohi-update-manifest-v1\x00"
)

// Manifest is the JSON payload inside an Envelope. Sequence increases for every
// release on a channel; Version is display text, never a security comparison.
type Manifest struct {
	Schema      int       `json:"schema"`
	Application string    `json:"application"`
	Channel     string    `json:"channel"`
	Version     string    `json:"version"`
	Sequence    uint64    `json:"sequence"`
	Expires     time.Time `json:"expires"`
	Assets      []Asset   `json:"assets"`
}

// Asset describes one platform artifact. SHA256 is lowercase hexadecimal.
// Size is the exact compressed/download size, not an estimate.
type Asset struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   Kind   `json:"kind"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Envelope authenticates the exact Payload bytes, prefixed with the protocol
// domain. JSON encodes Payload and Signature using standard base64.
type Envelope struct {
	KeyID     string `json:"key_id"`
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

// Fetch opens an HTTPS resource. Implementations must honor ctx, bound redirects,
// reject downgrade redirects and non-success responses, and close resources on
// error. updater closes a successful stream. Transport errors are returned
// without formatting the URL; consumers must avoid logging credentials.
type Fetch func(ctx context.Context, url string) (io.ReadCloser, error)

// Config is copied by New. Keys must be pinned in trusted application code or
// configuration, never obtained from the update feed. CurrentSequence must be
// compiled into the running release; consumers can raise it using a durable
// highest-installed sequence to prevent deliberate executable downgrades.
type Config struct {
	Application     string
	Channel         string
	CurrentSequence uint64
	FeedURL         string
	Keys            map[string]ed25519.PublicKey
	Fetch           Fetch
	Kind            Kind   // empty means Executable
	MaxArtifactSize int64  // zero means 256 MiB
	StageDir        string // existing private parent directory; empty uses OS temp
}

// Client is immutable and safe for concurrent use if its Fetch is. It does no
// background polling and never installs, prompts or exits automatically.
type Client struct{ cfg Config }

// New validates configuration without network or filesystem side effects.
func New(cfg Config) (*Client, error) {
	if cfg.Application == "" || cfg.Channel == "" || cfg.Fetch == nil || !httpsURL(cfg.FeedURL) || len(cfg.Keys) == 0 {
		return nil, fmt.Errorf("%w: configuration", ErrInvalid)
	}
	if cfg.Kind == "" {
		cfg.Kind = Executable
	}
	if cfg.Kind != Executable && cfg.Kind != AppBundle {
		return nil, ErrUnsupported
	}
	if cfg.Kind == AppBundle && runtime.GOOS != "darwin" {
		return nil, ErrUnsupported
	}
	if cfg.MaxArtifactSize == 0 {
		cfg.MaxArtifactSize = 256 << 20
	}
	if cfg.MaxArtifactSize < 1 || cfg.MaxArtifactSize > 1<<40 {
		return nil, fmt.Errorf("%w: artifact limit", ErrInvalid)
	}
	keys := make(map[string]ed25519.PublicKey, len(cfg.Keys))
	for id, key := range cfg.Keys {
		if id == "" || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: public key", ErrInvalid)
		}
		keys[id] = append(ed25519.PublicKey(nil), key...)
	}
	cfg.Keys = keys
	return &Client{cfg: cfg}, nil
}

// Release holds authenticated metadata. Accessors return values, never mutable
// slices or maps backed by this handle. Only a Release from this client can be
// staged by it; consumers cannot construct a trusted release from display data.
type Release struct {
	c        *Client
	envelope []byte
	manifest Manifest
	asset    Asset
}

func (r *Release) Version() string  { return r.manifest.Version }
func (r *Release) Sequence() uint64 { return r.manifest.Sequence }
func (r *Release) Artifact() Asset  { return r.asset }

// Check fetches at most 1 MiB of signed metadata and returns a matching newer
// release. ErrNoUpdate means its sequence is no greater than CurrentSequence.
func (c *Client) Check(ctx context.Context) (*Release, error) {
	b, err := c.read(ctx, c.cfg.FeedURL, manifestLimit)
	if err != nil {
		return nil, err
	}
	return c.verify(b, time.Now())
}

func (c *Client) verify(b []byte, now time.Time) (*Release, error) {
	var e Envelope
	if len(b) > manifestLimit || strictJSON(b, &e) != nil {
		return nil, fmt.Errorf("%w: envelope", ErrInvalid)
	}
	key := c.cfg.Keys[e.KeyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte(signatureDomain), e.Payload...), e.Signature) {
		return nil, fmt.Errorf("%w: signature", ErrInvalid)
	}
	var m Manifest
	if strictJSON(e.Payload, &m) != nil || m.Schema != 1 || m.Application != c.cfg.Application || m.Channel != c.cfg.Channel || m.Version == "" || m.Sequence == 0 || !m.Expires.After(now) || len(m.Assets) == 0 || len(m.Assets) > 64 {
		return nil, fmt.Errorf("%w: manifest", ErrInvalid)
	}
	seen := make(map[string]bool)
	var selected *Asset
	for _, a := range m.Assets {
		identity := a.OS + "/" + a.Arch + "/" + string(a.Kind)
		digest, err := hex.DecodeString(a.SHA256)
		if a.OS == "" || a.Arch == "" || (a.Kind != Executable && a.Kind != AppBundle) || !httpsURL(a.URL) || a.Size < 1 || a.Size > 1<<40 || err != nil || len(digest) != sha256.Size || a.SHA256 != strings.ToLower(a.SHA256) || seen[identity] {
			return nil, fmt.Errorf("%w: asset", ErrInvalid)
		}
		seen[identity] = true
		if a.OS == runtime.GOOS && a.Arch == runtime.GOARCH && a.Kind == c.cfg.Kind {
			v := a
			selected = &v
		}
	}
	if m.Sequence <= c.cfg.CurrentSequence {
		return nil, ErrNoUpdate
	}
	if selected == nil {
		return nil, ErrUnsupported
	}
	if selected.Size > c.cfg.MaxArtifactSize {
		return nil, fmt.Errorf("%w: artifact exceeds limit", ErrInvalid)
	}
	return &Release{c: c, envelope: bytes.Clone(b), manifest: m, asset: *selected}, nil
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.Opaque == ""
}

func (c *Client) read(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := c.cfg.Fetch(ctx, raw)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil download", ErrInvalid)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: r}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w: download exceeds limit", ErrInvalid)
	}
	return b, ctx.Err()
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(b)
	if e := r.ctx.Err(); e != nil {
		return n, e
	}
	return n, err
}

// Progress is delivered synchronously on the staging goroutine, without locks.
// Callbacks must not block indefinitely; UI consumers marshal it themselves.
type Progress struct{ Received, Total int64 }

// Staged owns a private verified download. Close removes it unless it was
// successfully handed to an update helper, which then owns cleanup. Do not copy.
type Staged struct {
	mu        sync.Mutex
	r         *Release
	dir       string
	scheduled bool
	closed    bool
}

// Stage downloads and verifies a release without changing the installation.
// Cancellation/error removes partial files. Fetch must interrupt blocked reads
// when ctx is cancelled. The returned handle must eventually be closed or used.
func (r *Release) Stage(ctx context.Context, progress func(Progress)) (_ *Staged, err error) {
	if _, err = r.c.verify(r.envelope, time.Now()); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(r.c.cfg.StageDir, "tuohi-update-")
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	f, err := os.OpenFile(filepath.Join(dir, "artifact"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	src, err := r.c.cfg.Fetch(ctx, r.asset.URL)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, fmt.Errorf("%w: nil download", ErrInvalid)
	}
	defer func() { _ = src.Close() }()
	h := sha256.New()
	w := &progressWriter{w: io.MultiWriter(f, h), total: r.asset.Size, callback: progress}
	n, err := io.Copy(w, io.LimitReader(&contextReader{ctx: ctx, r: src}, r.asset.Size+1))
	if err != nil {
		return nil, err
	}
	if n != r.asset.Size || hex.EncodeToString(h.Sum(nil)) != r.asset.SHA256 {
		return nil, fmt.Errorf("%w: artifact integrity", ErrInvalid)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return &Staged{r: r, dir: dir}, nil
}

type progressWriter struct {
	w               io.Writer
	received, total int64
	callback        func(Progress)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > p.total-p.received {
		return 0, fmt.Errorf("%w: oversized artifact", ErrInvalid)
	}
	n, err := p.w.Write(b)
	p.received += int64(n)
	if p.callback != nil {
		p.callback(Progress{p.received, p.total})
	}
	return n, err
}

func (s *Staged) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.scheduled {
		return nil
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return err
	}
	s.closed = true
	return nil
}

// strictJSON rejects duplicate keys, unknown fields and trailing values. The
// duplicate pass matters even for authenticated metadata: independent producer
// implementations must agree on exactly what was signed.
func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var scan func(int, reflect.Type) error
	scan = func(depth int, typ reflect.Type) error {
		if depth > 32 {
			return ErrInvalid
		}
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'):
			fields := map[string]reflect.Type{}
			if typ.Kind() == reflect.Struct {
				for i := 0; i < typ.NumField(); i++ {
					f := typ.Field(i)
					if !f.IsExported() {
						continue
					}
					name := strings.Split(f.Tag.Get("json"), ",")[0]
					if name == "-" {
						continue
					}
					if name == "" {
						name = f.Name
					}
					fields[name] = f.Type
				}
			} else if typ.Kind() != reflect.Map {
				return ErrInvalid
			}
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return ErrInvalid
				}
				seen[key] = true
				var fieldType reflect.Type
				if typ.Kind() == reflect.Map {
					fieldType = typ.Elem()
				} else {
					fieldType = fields[key]
					if fieldType == nil {
						return ErrInvalid
					}
				}
				if err = scan(depth+1, fieldType); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case json.Delim('['):
			if typ.Kind() != reflect.Slice && typ.Kind() != reflect.Array {
				return ErrInvalid
			}
			for d.More() {
				if err = scan(depth+1, typ.Elem()); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := scan(0, reflect.TypeOf(v)); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
