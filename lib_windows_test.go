package tuohi

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// The WebView2/COM backend runs on one OS thread, so the GUI scenarios run in
// TestMain (the main goroutine) and stash their results; the TestXxx functions
// only assert. Needs the Edge WebView2 Runtime (present on windows-latest CI).

var (
	resWinBridge      atomic.Value // string
	resWinErrorUnbind atomic.Value // string
	resWinRichTypes   atomic.Value // string
	resWinEmbed       atomic.Value // string
	resWinClose       atomic.Value // string
)

// guiAvailable reports whether the Edge WebView2 Runtime is installed. Without
// it the GUI scenarios are skipped so `go test ./...` stays green instead of
// failing on a machine without the runtime; windows-latest CI ships it.
func guiAvailable() bool {
	if ensureCOMInit() != nil {
		return false
	}
	_, err := findEmbeddedBrowserDLL()
	return err == nil
}

// requireGUI skips a GUI assertion when its scenario did not run, or fails it
// when TUOHI_REQUIRE_GUI=1. GitHub CI sets that, because its runner ships the
// WebView2 Runtime, so a run there cannot pass by testing nothing.
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got != "" {
		return
	}
	if os.Getenv("TUOHI_REQUIRE_GUI") == "1" {
		t.Fatal("GUI scenario did not run, and TUOHI_REQUIRE_GUI=1: Edge WebView2 Runtime not available")
	}
	t.Skip("Edge WebView2 Runtime not available")
}

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	repeatStatus := 0
	if !testing.Short() && guiAvailable() {
		resWinBridge.Store(winBridgeScenario())
		resWinErrorUnbind.Store(winErrorUnbindScenario())
		resWinRichTypes.Store(winRichTypesScenario())
		resWinEmbed.Store(winEmbedScenario())
		resOriginGate.Store(originGateScenario())
		resFrameGate.Store(frameGateScenario())
		resNavPolicy.Store(navPolicyScenario())
		resLoopbackApp.Store(loopbackAppScenario())
		resDataURL.Store(dataURLScenario())
		resGoroutineCalls.Store(goroutineCallsScenario())
		resOutsideLinks.Store(outsideLinksScenario())
		resTitle.Store(titleScenario())
		resReplyTrust.Store(replyTrustScenario())
		resPermissions.Store(permissionsScenario())
		if n, _ := strconv.Atoi(os.Getenv("TUOHI_REPEAT_DATAURL")); n > 0 {
			repeatStatus = repeatDataURL(n)
		}
		resWinClose.Store(winCloseViaUIScenario()) // last: it ends with WM_QUIT
	}
	code := m.Run()
	if code == 0 {
		code = repeatStatus
	}
	os.Exit(code)
}

// winCloseViaUIScenario simulates the user closing the window (WM_CLOSE, as the
// X button / Alt+F4 send) and asserts that Run() returns. Without the WM_CLOSE
// -> PostQuitMessage path, Run would block forever, so a watchdog distinguishes
// "Run ended because of the close" from "Run had to be force-terminated".
func winCloseViaUIScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	hwnd := native(w).window

	var watchdogFired atomic.Bool
	time.AfterFunc(2*time.Second, func() { postMessageW(hwnd, wmClose, 0, 0) })
	time.AfterFunc(40*time.Second, func() { watchdogFired.Store(true); w.Close() })

	native(w).loadHTML(`<!DOCTYPE html><html><body>close test</body></html>`)
	w.w.Run() // must return once WM_CLOSE posts WM_QUIT

	if watchdogFired.Load() {
		return "hung (WM_CLOSE did not end Run)"
	}
	// WM_DESTROY releases the web view on the UI thread, so nothing is left
	// for a Close that arrives after the loop has ended.
	if nw := native(w); nw.controller != 0 || nw.environment != 0 {
		return "web view not released by WM_DESTROY"
	}
	return "closed"
}

// winEmbedScenario embeds a web view into a caller-provided HWND and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func winEmbedScenario() string {
	err := ensureWinInit()
	if err != nil {
		return "init error: " + err.Error()
	}
	host := createWindowExW(0, utf16("STATIC"), utf16("host"), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 320, 240, 0, 0, getModuleHandleW(0), 0)
	if host == 0 {
		return "host window nil"
	}
	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := native(w).ownsWindow

	// The embedded View must follow the host window. SetWindowPos SENDS
	// WM_SIZE synchronously through the subclass chain, so right after it
	// returns the controller bounds must match the new client rect - no
	// message pump involved.
	setWindowPos(host, 0, 0, 0, 500, 400, swpNoZOrder|swpNoActivate|swpNoMove)
	var want, got rect
	getClientRect(host, &want)
	asController(native(w).controller).getBounds(&got)
	if got != want {
		w.Close()
		destroyWindow(host)
		return fmt.Sprintf("bounds after host resize = %+v, want %+v (WM_SIZE not routed)", got, want)
	}

	// Dispatch in embed mode rides WM_APP through the same subclass. The
	// closure is already queued before the pump starts, and sentinel
	// messages keep GetMessage from blocking if it is ever NOT picked up
	// (a hand-off through the window's user data would drop it silently).
	ran := false
	w.w.Dispatch(func() { ran = true })
	for range 8 {
		postMessageW(host, wmApp+1, 0, 0)
	}
	var m msgStruct
	for i := 0; i < 9 && !ran && getMessageW(&m, 0, 0, 0) > 0; i++ {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	if !ran {
		w.Close()
		destroyWindow(host)
		return "Dispatch closure never ran in embed mode (WM_APP not routed)"
	}

	w.Close()
	// A valid window still returns its style; a destroyed HWND returns 0.
	alive := getWindowLongPtrW(host, gwlStyle) != 0
	destroyWindow(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	if !alive {
		return "host destroyed (BUG)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resWinEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestCloseViaUI(t *testing.T) {
	got, _ := resWinClose.Load().(string)
	requireGUI(t, got)
	if got != "closed" {
		t.Fatalf("close via UI = %q, want %q", got, "closed")
	}
}

func winBridgeScenario() string {
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
	time.AfterFunc(40*time.Second, func() { w.Close() }) // watchdog

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

// winErrorUnbindScenario covers a rejected-promise binding and, crucially, that
// an Unbound name does NOT reappear when a new document loads - the regression
// test for the doc-start onBind script leak (a buggy Unbind that left the
// document-start bind script installed would make window.temp a function again
// at load, so "temp=undefined" would fail).
func winErrorUnbindScenario() string {
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
	time.AfterFunc(40*time.Second, func() { w.Close() })

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

// xy mirrors the linux/darwin test's point struct (named differently here to
// avoid clashing with the Windows backend's own point type).
type xy struct{ X, Y int }

func winRichTypesScenario() string {
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
	_ = w.w.Bind("echoPoint", func(p xy) xy { return xy{p.X + 1, p.Y + 1} })
	_ = w.w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(40*time.Second, func() { w.Close() })

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

func TestBridge(t *testing.T) {
	got, _ := resWinBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resWinErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resWinRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

// serveDummy is a non-nil resolver for tests that only exercise URL
// rewriting/canonicalization (its content is never consulted).
func serveDummy() serveFunc { return func(*request) *response { return nil } }

// rewriteSchemeURL maps a registered scheme's URL to its https vhost and leaves
// everything else alone. Pure string logic, so it runs headless.
func TestRewriteSchemeURL(t *testing.T) {
	w := &webview{viewCore: viewCore{serve: serveDummy()}}

	cases := []struct {
		name, in, want string
	}{
		{"registered scheme -> vhost", "app://home/index.html", "https://app.localhost/index.html"},
		{"keeps query", "app://home/x?y=1", "https://app.localhost/x?y=1"},
		{"keeps fragment", "app://home/index.html#/route", "https://app.localhost/index.html#/route"},
		{"keeps query and fragment", "app://home/x?y=1#/r", "https://app.localhost/x?y=1#/r"},
		{"root path", "app://home/", "https://app.localhost/"},
		{"unregistered scheme passes through", "other://z/a", "other://z/a"},
		{"https passes through", "https://example.com/a", "https://example.com/a"},
	}
	for _, c := range cases {
		got := w.rewriteSchemeURL(c.in)
		if got != c.want {
			t.Errorf("%s: rewriteSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// With no content resolver, URLs are never rewritten.
func TestRewriteSchemeURLNoContent(t *testing.T) {
	w := &webview{}
	in := "app://home/index.html"
	got := w.rewriteSchemeURL(in)
	if got != in {
		t.Errorf("rewriteSchemeURL(%q) with no resolver = %q, want unchanged", in, got)
	}
}

// canonicalSchemeURL turns the internal vhost URL back into the scheme:// form,
// restoring the authority the app navigated with so a resolver sees the same
// URL as it does on macOS/Linux.
func TestCanonicalSchemeURL(t *testing.T) {
	w := &webview{
		viewCore:        viewCore{serve: serveDummy()},
		schemeAuthority: "home",
	}
	cases := []struct {
		name, in, want string
	}{
		{"restores authority", "https://app.localhost/index.html", "app://home/index.html"},
		{"restores authority + query", "https://app.localhost/x?y=1", "app://home/x?y=1"},
		{"root", "https://app.localhost/", "app://home/"},
	}
	for _, c := range cases {
		got := w.canonicalSchemeURL(c.in)
		if got != c.want {
			t.Errorf("%s: canonicalSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// Without a recorded authority, canonicalSchemeURL falls back to the scheme
// name so the URL is still well-formed scheme:// (not the internal vhost).
func TestCanonicalSchemeURLFallback(t *testing.T) {
	w := &webview{viewCore: viewCore{serve: serveDummy()}}
	got := w.canonicalSchemeURL("https://app.localhost/index.html")
	want := "app://app/index.html"
	if got != want {
		t.Errorf("canonicalSchemeURL fallback = %q, want %q", got, want)
	}
}

// Navigate rewrite followed by the request-time reconstruction round-trips the
// authority the app used, so the resolver URL matches the original scheme:// URL.
func TestSchemeURLRoundTrip(t *testing.T) {
	w := &webview{viewCore: viewCore{serve: serveDummy()}}
	// The app navigates here; rewriteSchemeURL records the "home" authority.
	w.rewriteSchemeURL("app://home/index.html")
	// A sub-resource request arrives on the vhost origin and is reconstructed.
	got := w.canonicalSchemeURL("https://app.localhost/assets/app.js")
	want := "app://home/assets/app.js"
	if got != want {
		t.Errorf("round-trip = %q, want %q", got, want)
	}
}

// repeatDataURL runs dataURLScenario n times in this process, printing each
// result, and returns the exit status: 1 when any run failed. It is a
// diagnostic for TKT-01M3MWY0QQQ6J07DHDY0CCBN2V (Stop dropping a data: page's
// binding call on Windows now and then), set by TUOHI_REPEAT_DATAURL=n. It
// runs after the other scenarios, and the ordinary tests still run and
// assert: the process fails when either they or a repeat fail.
func repeatDataURL(n int) int {
	failed := 0
	for i := 1; i <= n; i++ {
		got := dataURLScenario()
		fmt.Fprintf(os.Stderr, "dataURL run %d/%d: %s\n", i, n, got)
		if got != "called data" {
			failed++
		}
	}
	fmt.Fprintf(os.Stderr, "dataURL: %d of %d failed\n", failed, n)
	if failed > 0 {
		return 1
	}
	return 0
}

// windowTitle reads the native window's title, on the UI thread.
func windowTitle(e engine) string {
	buf := make([]uint16, 512)
	n := getWindowTextW(e.(*webview).window, &buf[0], int32(len(buf)))
	return string(utf16Decode(buf[:n]))
}

// pageURL reads the URL of the document that last committed in the view, on
// the UI thread.
func pageURL(e engine) string {
	return e.(*webview).committedURI
}

// enableFakeCapture does nothing on Windows: tuohi creates the WebView2
// environment through the runtime's internal export, which does not read
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, so there is no way to give it fake
// devices (see TestPermissions).
func enableFakeCapture(engine) {}

// realClick makes no click on Windows, where a clipboard read needs none (see
// TestPermissions).
func realClick(*View) bool { return false }
