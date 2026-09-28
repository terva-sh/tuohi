//go:build linux || freebsd || netbsd

package tuohi

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// The GTK backend runs on one OS thread, so the GUI scenarios run in TestMain
// (the main goroutine) and stash results; the TestXxx functions only assert.
// Needs an X display - run under xvfb-run on CI.

var (
	resBridge      atomic.Value // string
	resErrorUnbind atomic.Value // string
	resRichTypes   atomic.Value // string
	resEmbed       atomic.Value // string
	resWaitClose   atomic.Value // string
)

// hasDisplay reports whether a windowing system is available.
func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// webkitRunnable reports whether WebKitGTK can spawn its helper processes
// here, and why not when it cannot. The GUI scenarios crash the whole test
// binary (SIGABRT inside WebKit) when it cannot: the bubblewrap sandbox needs
// an unprivileged user namespace, which many containers forbid, and the
// helpers place their sockets under XDG_RUNTIME_DIR, which some sandboxes
// mount read-only. bwrap's own exit code is the ground truth for the first
// half - the /proc/sys knobs lie when the restriction comes from
// seccomp/AppArmor - so probe it directly. The probe binds the host root
// read-only: without a bind, bwrap starts in an empty root where no command
// exists, and fails wherever it is installed. A machine without bwrap is NOT a
// skip reason: WebKitGTK then runs its helpers unsandboxed.
func webkitRunnable() (bool, string) {
	bwrap, err := exec.LookPath("bwrap")
	if err == nil {
		truePath, err := exec.LookPath("true")
		if err != nil {
			return false, "no true(1) on PATH to probe bubblewrap with"
		}
		out, err := exec.Command(bwrap, "--unshare-user", "--ro-bind", "/", "/", "--", truePath).CombinedOutput()
		if err != nil {
			return false, fmt.Sprintf("bubblewrap cannot create a user namespace: %v: %s", err, bytes.TrimSpace(out))
		}
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && !isWritableDir(d) {
		return false, "XDG_RUNTIME_DIR " + d + " is not writable, so WebKit cannot create its helper sockets"
	}
	return true, ""
}

// guiAvailable reports whether the GTK/WebKitGTK stack can actually run here:
// the shared libraries load, a display is present AND WebKitGTK can spawn its
// helper processes. Without all three, the GUI scenarios are skipped so
// `go test ./...` stays green on a headless box, on one without WebKitGTK in
// the loader path (minimal containers, Nix, etc.) and in restricted sandboxes
// (no unprivileged user namespaces, read-only XDG_RUNTIME_DIR) instead of
// failing. CI installs the libraries and runs under xvfb, which sets DISPLAY.
// The reason names the first check that failed, for the skip message.
func guiAvailable() (bool, string) {
	if !hasDisplay() {
		return false, "no display: neither DISPLAY nor WAYLAND_DISPLAY is set; run under xvfb-run"
	}
	if ok, why := webkitRunnable(); !ok {
		return false, why
	}
	if err := ensureInit(); err != nil {
		return false, "WebKitGTK did not load: " + err.Error() + "; install libwebkit2gtk-4.1-0 or libwebkitgtk-6.0-4"
	}
	return true, ""
}

// guiSkipReason is why the GUI scenarios did not run, empty when they did.
var guiSkipReason string

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	// -short never probes: the probe runs bwrap and loads the GTK stack.
	ok, why := false, "GUI scenarios skipped under -short"
	if !testing.Short() {
		ok, why = guiAvailable()
	}
	guiSkipReason = why
	if ok {
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resEmbed.Store(embedScenario())
		resWaitClose.Store(waitCloseScenario())
		resOriginGate.Store(originGateScenario())
		resFrameGate.Store(frameGateScenario())
		resNavPolicy.Store(navPolicyScenario())
		resLoopbackApp.Store(loopbackAppScenario())
		resDataURL.Store(dataURLScenario())
	}
	os.Exit(m.Run())
}

// embedScenario embeds a web view into a caller-provided GtkWindow and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func embedScenario() string {
	err := ensureInit()
	if err != nil {
		return "init error: " + err.Error()
	}
	if !gtkInit() {
		return "gtk_init failed"
	}
	host := gtkNewWindow()
	if host == 0 {
		return "host window nil"
	}
	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := native(w).ownsWindow
	w.Close()
	// Host must still be alive after Close (this call would fault on a freed
	// widget), then tear it down ourselves. gtk_window_resize is GTK3-only;
	// GTK4 has no resize and gtkWindowResize is nil there.
	if gtk4 {
		gtkWindowSetDefaultSize(host, 300, 200)
	} else {
		gtkWindowResize(host, 300, 200)
	}
	gtkWindowClose(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func bridgeScenario() string {
	w := &View{Debug: true, Width: 700, Height: 500}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("add", func(a, b float64) float64 { return a + b })
	_ = w.w.Bind("hello", func(s string) string { return "hi " + s })
	_ = w.w.Bind("done", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	time.AfterFunc(15*time.Second, func() { w.Close() })

	native(w).loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var s = await window.add(20, 22);
    var h = await window.hello("x");
    window.done(s + "|" + h);
  } catch(e) { window.done("ERR:" + e); }
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func errorUnbindScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	_ = w.w.Bind("boom", func() (string, error) { return "", errors.New("kaboom") })
	_ = w.w.Bind("temp", func() string { return "x" })
	_ = w.w.Unbind("temp")
	time.AfterFunc(15*time.Second, func() { w.Close() })

	native(w).loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  var msg = 'temp=' + (typeof window.temp);
  try { await window.boom(); msg += ' boom=nope'; }
  catch(e){ msg += ' boom=' + e; }
  window.report(msg);
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

type point struct{ X, Y int }

func richTypesScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	_ = w.w.Bind("echoPoint", func(p point) point { return point{p.X + 1, p.Y + 1} })
	_ = w.w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(15*time.Second, func() { w.Close() })

	native(w).loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var p = await window.echoPoint({X:1, Y:2});
    var s = await window.sum([1,2,3,4]);
    window.report('p=' + p.X + ',' + p.Y + ' s=' + s);
  } catch(e) { window.report('ERR:' + e); }
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

// requireGUI skips a GUI assertion when its scenario did not run, or fails it
// when TUOHI_REQUIRE_GUI=1. `just test-gui` sets that, so a run meant to
// exercise the GUI cannot pass by testing nothing.
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got != "" {
		return
	}
	if os.Getenv("TUOHI_REQUIRE_GUI") == "1" {
		t.Fatalf("GUI scenario did not run, and TUOHI_REQUIRE_GUI=1: %s", guiSkipReason)
	}
	t.Skip(guiSkipReason)
}

func TestBridge(t *testing.T) {
	got, _ := resBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

// TestLinuxBackendOverride pins down the APPKIT_BACKEND contract: the two
// documented values map to the two stacks, an unset variable means
// auto-detection, and anything else is ignored (with a warning) rather than
// failing. Pure env parsing - no display needed.
func TestLinuxBackendOverride(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", backendAuto},
		{envBackendGTK4, backendGTK4},
		{envBackendGTK3, backendGTK3},
		{"gtk4", backendAuto},          // close but not a documented value
		{"WEBKITGTK-6.0", backendAuto}, // values are case-sensitive
	}
	for _, c := range cases {
		t.Setenv("APPKIT_BACKEND", c.env)
		if got := linuxBackendOverride(); got != c.want {
			t.Errorf("APPKIT_BACKEND=%q: got %d, want %d", c.env, got, c.want)
		}
	}
}

// waitCloseScenario verifies the app-level lifecycle: App.Wait runs the UI
// loop and returns when the owned window is closed (the engine reports the
// window close into the App scope).
func waitCloseScenario() string {
	// Exit true: Wait ends when the owned window closes (see App.Exit).
	app := &App{Exit: true}
	w := &View{Width: 400, Height: 300}
	if err := app.Show(w); err != nil {
		return "view error: " + err.Error()
	}
	// Smoke the window-state controls from a background goroutine (their
	// marshaling path). The minimize -> Show sequence mirrors the "restore
	// after minimizing" bug: Show must de-iconify first, not just present.
	time.AfterFunc(300*time.Millisecond, func() {
		w.Maximize()
		w.Unmaximize()
		w.Minimize()
	})
	time.AfterFunc(900*time.Millisecond, func() {
		w.Show() // de-iconifies (unminimizes) and presents
	})
	time.AfterFunc(1200*time.Millisecond, func() {
		dispatchMain(func() { w.Close() })
	})
	if err := app.Wait(); err != nil {
		return "wait error: " + err.Error()
	}
	return "wait-ok"
}

func TestWaitReturnsAfterLastWindowCloses(t *testing.T) {
	got, _ := resWaitClose.Load().(string)
	requireGUI(t, got)
	if got != "wait-ok" {
		t.Fatalf("wait/close scenario = %q, want %q", got, "wait-ok")
	}
}
