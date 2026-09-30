// Package run runs an example the way its user does, as a process of its own,
// for the examples' tests.
//
// An example's TestMain hands itself to Main. Its test then calls Program,
// which starts the test binary again with the example's arguments; in that
// child, Main runs the example's main function on the main goroutine, where
// tuohi requires it on macOS, and main exits the process. Nothing about the
// example changes to make it testable.
package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// childEnv carries the example's arguments into the child, and marks it as one.
const childEnv = "TUOHI_EXAMPLE_ARGS"

// Main is the example's TestMain. In a child started by Program it runs main
// with the arguments Program was given, and exits 0 if main returns; in the
// test process it runs the tests.
func Main(m *testing.M, main func()) {
	if args, ok := os.LookupEnv(childEnv); ok {
		os.Args = append(os.Args[:1], strings.Fields(args)...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Program runs the example with args in a child process and returns its
// combined output, failing the test when the child exits non-zero or outlives
// timeout.
//
// The example opens a window, so Program skips unless TUOHI_REQUIRE_GUI=1
// says one can open here, as `just test-gui` and every GUI job in CI do. The
// child gets its own XDG directories, so an example that registers autostart
// or writes a desktop file writes it there.
func Program(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("opens a window; skipped under -short")
	}
	if os.Getenv("TUOHI_REQUIRE_GUI") != "1" {
		t.Skip("opens a window; set TUOHI_REQUIRE_GUI=1 where one can open, as `just test-gui` does")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	home := t.TempDir()
	cmd.Env = append(os.Environ(),
		childEnv+"="+strings.Join(args, " "),
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("still running after %v:\n%s", timeout, out)
	}
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	return string(out)
}
