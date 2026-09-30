package main

import (
	"testing"
	"time"

	"github.com/terva-sh/tuohi/examples/internal/run"
)

func TestMain(m *testing.M) { run.Main(m, main) }

// TestSelfTest runs the showcase's scripted suite in a real window: every
// bridge form, events, clipboard, autostart, the drag region, window state
// and notifications, as `go run ./showcase --selftest` does. The showcase
// gives up on a page that is not ready in 20 seconds or a suite that has not
// reported in 60 more.
func TestSelfTest(t *testing.T) {
	out := run.Program(t, 2*time.Minute, "-selftest")
	t.Log(out)
}
