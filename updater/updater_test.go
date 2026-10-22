package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	c        *Client
	m        Manifest
	key      ed25519.PrivateKey
	data     []byte
	envelope []byte
	closed   int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, data: []byte("new executable contents")}
	h := sha256.Sum256(f.data)
	f.m = Manifest{Schema: 1, Application: "test.app", Channel: "stable", Version: "2.0", Sequence: 2, Expires: time.Now().Add(time.Hour), Assets: []Asset{{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: Executable, URL: "https://updates.invalid/app", Size: int64(len(f.data)), SHA256: hex.EncodeToString(h[:])}}}
	f.envelope = f.sign(t, f.m)
	f.c, err = New(Config{Application: "test.app", Channel: "stable", CurrentSequence: 1, FeedURL: "https://updates.invalid/feed", Keys: map[string]ed25519.PublicKey{"one": pub}, StageDir: t.TempDir(), Fetch: func(ctx context.Context, url string) (io.ReadCloser, error) {
		data := f.data
		if strings.HasSuffix(url, "feed") {
			data = f.envelope
		}
		return &countClose{Reader: bytes.NewReader(data), close: func() { f.closed++ }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type countClose struct {
	io.Reader
	close func()
}

func (c *countClose) Close() error { c.close(); return nil }
func (f *fixture) sign(t *testing.T, m Manifest) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return f.signBytes(t, b)
}
func (f *fixture) signBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	e := Envelope{KeyID: "one", Payload: b, Signature: ed25519.Sign(f.key, append([]byte(signatureDomain), b...))}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMetadataTrustBoundary(t *testing.T) {
	for _, name := range []string{"tamper", "unknown-key", "expired", "wrong-app", "wrong-channel", "wrong-platform", "rollback", "unknown-field", "duplicate", "trailing", "duplicate-asset", "size", "http", "credentials", "digest", "schema"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			m := f.m
			want := ErrInvalid
			switch name {
			case "tamper":
				var e Envelope
				_ = json.Unmarshal(f.envelope, &e)
				e.Payload = append(e.Payload, ' ')
				f.envelope, _ = json.Marshal(e)
			case "unknown-key":
				var e Envelope
				_ = json.Unmarshal(f.envelope, &e)
				e.KeyID = "other"
				f.envelope, _ = json.Marshal(e)
			case "expired":
				m.Expires = time.Now().Add(-time.Second)
			case "wrong-app":
				m.Application = "other"
			case "wrong-channel":
				m.Channel = "nightly"
			case "wrong-platform":
				m.Assets[0].Arch = "imaginary"
				want = ErrUnsupported
			case "rollback":
				m.Sequence = 1
				want = ErrNoUpdate
			case "unknown-field":
				f.envelope = f.signBytes(t, []byte(`{"surprise":true}`))
			case "duplicate":
				b, _ := json.Marshal(m)
				f.envelope = f.signBytes(t, append([]byte(`{"sequence":99,`), b[1:]...))
			case "trailing":
				b, _ := json.Marshal(m)
				f.envelope = f.signBytes(t, append(b, []byte(` {}`)...))
			case "duplicate-asset":
				m.Assets = append(m.Assets, m.Assets[0])
			case "size":
				m.Assets[0].Size = f.c.cfg.MaxArtifactSize + 1
			case "http":
				m.Assets[0].URL = "http://updates.invalid/app"
			case "credentials":
				m.Assets[0].URL = "https://user:secret@updates.invalid/app"
			case "digest":
				m.Assets[0].SHA256 = "abcdef"
			case "schema":
				m.Schema = 2
			}
			switch name {
			case "tamper", "unknown-key", "unknown-field", "duplicate", "trailing":
			default:
				f.envelope = f.sign(t, m)
			}
			if _, err := f.c.Check(context.Background()); !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			if f.closed != 1 {
				t.Fatalf("stream not closed: %d", f.closed)
			}
		})
	}
}

func TestManifestBoundAndConfigurationCopy(t *testing.T) {
	f := newFixture(t)
	key := append(ed25519.PublicKey(nil), f.key.Public().(ed25519.PublicKey)...)
	keys := map[string]ed25519.PublicKey{"one": key}
	cfg := f.c.cfg
	cfg.Keys = keys
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clear(key)
	delete(keys, "one")
	if _, err = c.Check(context.Background()); err != nil {
		t.Fatal("configuration not copied", err)
	}
	f.envelope = bytes.Repeat([]byte(" "), manifestLimit+1)
	if _, err = c.Check(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestStageIntegrityCancellationAndCleanup(t *testing.T) {
	for _, name := range []string{"valid", "corrupt", "short", "oversized", "cancel", "expiry"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, err := f.c.Check(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "corrupt":
				f.data[0] ^= 1
			case "short":
				f.data = f.data[:3]
			case "oversized":
				f.data = append(f.data, 'x')
			case "cancel":
				cancel()
			case "expiry":
				r.manifest.Expires = time.Now().Add(-time.Hour)
				r.envelope = f.sign(t, r.manifest)
			}
			var last Progress
			s, err := r.Stage(ctx, func(p Progress) { last = p })
			entries, _ := os.ReadDir(f.c.cfg.StageDir)
			if name != "valid" {
				if err == nil {
					t.Fatal("accepted invalid artifact")
				}
				if len(entries) != 0 {
					t.Fatal("partial stage leaked")
				}
				if name == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if last.Received != int64(len(f.data)) || last.Total != int64(len(f.data)) {
				t.Fatal(last)
			}
			p := filepath.Join(s.dir, "artifact")
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatal(info.Mode())
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(p); !os.IsNotExist(err) {
				t.Fatal("stage not removed", err)
			}
			if f.closed != 2 {
				t.Fatal("streams not closed", f.closed)
			}
		})
	}
}

func TestStageCancellationDuringRead(t *testing.T) {
	f := newFixture(t)
	r, err := f.c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.c.cfg.Fetch = func(context.Context, string) (io.ReadCloser, error) {
		return io.NopCloser(&cancelReader{cancel: cancel}), nil
	}
	if _, err = r.Stage(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(f.c.cfg.StageDir)
	if len(entries) != 0 {
		t.Fatal("partial download retained")
	}
}

type cancelReader struct{ cancel context.CancelFunc }

func (r *cancelReader) Read(b []byte) (int, error) { r.cancel(); return copy(b, "x"), nil }

func TestStrictJSON(t *testing.T) {
	for _, b := range []string{`{"key_id":"a","key_id":"b"}`, `{} {}`, `{"x":[{"y":1,"y":2}]}`, `null`, strings.Repeat("[", 40) + strings.Repeat("]", 40)} {
		var e Envelope
		if strictJSON([]byte(b), &e) == nil && b != "null" {
			t.Fatalf("accepted %q", b)
		}
	}
}
