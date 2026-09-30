package main

import (
	"testing"
	"time"

	"github.com/terva-sh/tuohi/examples/internal/run"
)

func TestMain(m *testing.M) { run.Main(m, main) }

// TestCheck opens the window on the program's own server and waits for the
// page to report that it read from both the server and Go.
func TestCheck(t *testing.T) {
	out := run.Program(t, time.Minute, "-check")
	t.Log(out)
}
