package main

import (
	"context"
	"errors"
	"os"
	"strings"
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
