// Command selfupdate demonstrates an optional updater at the desktop edge.
// Build an installed executable to exercise real updates; go run's temporary
// executable is not a persistent installation. See updater/README.md.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/terva-sh/tuohi"
	"github.com/terva-sh/tuohi/instance"
	"github.com/terva-sh/tuohi/updater"
)

// Set in each release with -ldflags '-X main.releaseSequence=42'. Version
// ordering uses this compiled value, never a display version or result record.
var releaseSequence = "1"

//go:embed page.html
var content embed.FS

func main() {
	if err := runProgram(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runProgram() error {
	if handled, err := updater.HandleHelper(context.Background()); handled {
		return err
	}
	feed := flag.String("feed", "", "HTTPS signed update feed")
	publicKey := flag.String("public-key", "", "base64 publisher public key (not a secret)")
	keyID := flag.String("key-id", "publisher", "pinned publisher key ID")
	application := flag.String("application", "com.example.tuohi.selfupdate", "release application/bundle identifier")
	mode := flag.String("install-mode", "managed", "portable, bundle or managed; selected by packaging")
	target := flag.String("target", "", "absolute installed executable or .app root")
	check := flag.Bool("check", false, "exercise signed staging and normal UI shutdown without installing")
	flag.Parse()
	sequence, err := strconv.ParseUint(releaseSequence, 10, 64)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var client *updater.Client
	if *check {
		client, err = checkClient()
	} else if *feed != "" {
		key, e := base64.StdEncoding.DecodeString(*publicKey)
		if e != nil {
			return errors.New("invalid publisher public key")
		}
		kind := updater.Executable
		if updater.InstallMode(*mode) == updater.BundleInstall {
			kind = updater.AppBundle
		}
		client, err = updater.New(updater.Config{Application: *application, Channel: "stable", CurrentSequence: sequence, FeedURL: *feed, Keys: map[string]ed25519.PublicKey{*keyID: key}, Kind: kind, Fetch: httpsFetch()})
	}
	if err != nil {
		return err
	}
	if *target == "" {
		*target, err = os.Executable()
		if err != nil {
			return err
		}
	}
	id := *application
	if *check {
		id += ".check." + strconv.Itoa(os.Getpid())
	}
	lock, err := instance.Acquire(id, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	app := &tuohi.App{Name: "tuohi updates", FS: content, Exit: true}
	view := &tuohi.View{Title: "Desktop updates", URL: "app://app/page.html", Frame: true, Width: 640, Height: 480}
	var op sync.Mutex
	var stage *updater.Staged
	closed := false
	verified := false
	var readyVersion string
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	stateDir := filepath.Join(cache, "tuohi-selfupdate")
	view.Bind = map[string]any{
		"updates.prepare": func() (string, error) {
			if !op.TryLock() {
				return "", updater.ErrBusy
			}
			defer op.Unlock()
			if closed {
				return "", context.Canceled
			}
			if client == nil {
				return "", errors.New("configure a release feed and pinned public key first")
			}
			if stage != nil {
				if e := stage.Close(); e != nil {
					return "", e
				}
				stage = nil
			}
			r, e := client.Check(ctx)
			if e != nil {
				return "", e
			}
			s, e := r.Stage(ctx, func(p updater.Progress) { view.Emit("download", p) })
			if e != nil {
				return "", e
			}
			stage = s
			readyVersion = r.Version()
			return readyVersion, nil
		},
		"updates.install": func() error {
			if !op.TryLock() {
				return updater.ErrBusy
			}
			defer op.Unlock()
			if closed {
				return context.Canceled
			}
			if *check {
				return errors.New("check mode never installs")
			}
			if stage == nil {
				return errors.New("prepare an update first")
			}
			// This is where a real app asks its model to persist unsaved work.
			// Save failures must return before scheduling/closing anything.
			p, e := stage.Schedule(ctx, updater.RestartOptions{Mode: updater.InstallMode(*mode), Target: *target, Args: []string{"-feed", *feed, "-public-key", *publicKey, "-key-id", *keyID, "-application", *application, "-install-mode", *mode, "-target", *target}})
			if e != nil {
				return e
			}
			// Keep the recovery path across restart. A real app can include it in
			// its existing settings transaction. It is not a trusted sequence floor.
			if e = os.MkdirAll(stateDir, 0700); e == nil {
				e = os.WriteFile(filepath.Join(stateDir, "result-path"), []byte(p.ResultPath()), 0600)
			}
			if e != nil {
				_ = p.Cancel()
				stage = nil
				return e
			}
			app.Quit() // Wait returns; normal cleanup below runs before process exit.
			return nil
		},
		"updates.checked": func(version string) {
			if !*check {
				return
			}
			op.Lock()
			verified = stage != nil && version == readyVersion
			op.Unlock()
			app.Quit()
		},
	}
	if *check {
		view.URL += "#check"
		go func() {
			select {
			case <-ctx.Done():
			case <-time.After(20 * time.Second):
				app.Quit()
			}
		}()
	}
	if err = app.Show(view); err != nil {
		return err
	}
	err = app.Wait()
	// Cancel transport first, then wait for any binding operation to finish.
	cancel()
	op.Lock()
	closed = true
	if stage != nil {
		err = errors.Join(err, stage.Close())
	}
	op.Unlock()
	view.Close()
	err = errors.Join(err, lock.Release())
	if *check {
		if !verified {
			return errors.Join(err, errors.New("page did not verify staging"))
		}
		// A relaunched consumer must be able to acquire this same instance ID.
		again, e := instance.Acquire(id, nil)
		if e != nil {
			return errors.Join(err, e)
		}
		err = errors.Join(err, again.Release())
		fmt.Println("verified staging; UI exited; instance released")
	}
	return err
}

func httpsFetch() updater.Fetch {
	return fetchWithClient(updateHTTPClient())
}

func updateHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("update redirect refused")
		}
		return nil
	}}
}

func fetchWithClient(client *http.Client) updater.Fetch {
	return func(ctx context.Context, url string) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil || req.URL.Scheme != "https" || req.URL.User != nil {
			return nil, errors.New("invalid update request")
		}
		response, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("update request failed")
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return nil, fmt.Errorf("update server status %d", response.StatusCode)
		}
		return response.Body, nil
	}
}

// checkClient uses a fresh in-memory test publisher and a harmless staged
// payload. It never installs, accesses a real update service or persists a key.
func checkClient() (*updater.Client, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	data := []byte("reference payload; check mode never installs")
	h := sha256.Sum256(data)
	asset := updater.Asset{OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: updater.Executable, URL: "https://updates.invalid/payload", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}
	m := updater.Manifest{Schema: 1, Application: "test.selfupdate", Channel: "stable", Version: "example-2", Sequence: 2, Expires: time.Now().Add(time.Hour), Assets: []updater.Asset{asset}}
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	envelope, err := json.Marshal(updater.Envelope{KeyID: "test", Payload: payload, Signature: ed25519.Sign(key, append([]byte("tuohi-update-manifest-v1\x00"), payload...))})
	if err != nil {
		return nil, err
	}
	return updater.New(updater.Config{Application: m.Application, Channel: m.Channel, CurrentSequence: 1, FeedURL: "https://updates.invalid/feed", Keys: map[string]ed25519.PublicKey{"test": pub}, Fetch: func(ctx context.Context, url string) (io.ReadCloser, error) {
		b := data
		if url == "https://updates.invalid/feed" {
			b = envelope
		}
		return io.NopCloser(bytes.NewReader(b)), ctx.Err()
	}})
}
