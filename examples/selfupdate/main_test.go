package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/tuohi/examples/internal/run"
	"github.com/terva-sh/tuohi/updater"
)

func TestMain(m *testing.M) {
	// Test executables must recognize the real helper invocation before the
	// example harness substitutes its own normal-launch arguments.
	if handled, err := updater.HandleHelper(context.Background()); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	run.Main(m, main)
}

func TestDesktopStagingAndShutdown(t *testing.T) {
	output := run.Program(t, time.Minute, "-check")
	if !strings.Contains(output, "verified staging; UI exited; instance released") {
		t.Fatal(output)
	}
}

func TestCheckFeedCanOnlyStageHarmlessPayload(t *testing.T) {
	c, err := checkClient()
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Stage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r.Version() != "example-2" || r.Sequence() != 2 {
		t.Fatal("unexpected reference release")
	}
}

func TestHTTPSFetchHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := httpsFetch()(ctx, "https://updates.invalid/feed"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTransportRejectsBadStatusAndDowngradeRedirects(t *testing.T) {
	var insecureRequests atomic.Int32
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { insecureRequests.Add(1) }))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/downgrade":
			http.Redirect(w, r, insecure.URL, http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "artifact")
		}
	}))
	defer secure.Close()
	client := updateHTTPClient()
	client.Transport = secure.Client().Transport
	fetch := fetchWithClient(client)
	for _, path := range []string{"/bad", "/downgrade", "/loop"} {
		if r, err := fetch(context.Background(), secure.URL+path); err == nil {
			_ = r.Close()
			t.Fatalf("accepted %s", path)
		}
	}
	if insecureRequests.Load() != 0 {
		t.Fatal("followed HTTPS downgrade")
	}
	r, err := fetch(context.Background(), secure.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "artifact" {
		t.Fatal(string(b), err)
	}
}
