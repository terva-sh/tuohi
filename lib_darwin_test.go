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

	"github.com/terva-sh/tuohi/pure/objc"

	"github.com/terva-sh/tuohi/dialog"
)

// AppKit runs on one OS thread, so the GUI scenarios run in TestMain (the main
// goroutine) and stash results; the TestXxx functions only assert. The macOS CI
// runner has a window server, so no virtual display is needed.

var (
	resBridge       atomic.Value // string
	resErrorUnbind  atomic.Value // string
	resRichTypes    atomic.Value // string
	resMultiWindow  atomic.Value // string
	resEmbed        atomic.Value // string
	resOpenPanel    atomic.Value // string
	resDialogCfg    atomic.Value // string
	resFirstMouse   atomic.Value // string
	resHitTest      atomic.Value // string
	resRaise        atomic.Value // string
	resExternalLoop atomic.Value // string
	resWindowState  atomic.Value // string
	resBadMessages  atomic.Value // string
)

func TestMain(m *testing.M) {
	// Honor -short so `go test -short ./...` is a fast, headless run: each GUI
	// scenario drives a real NSApplication run loop and can take a few seconds, so
	// running all of them unconditionally makes a plain `go test` slow and fragile
	// under a tight timeout. The assertions skip when their scenario didn't run.
	flag.Parse()
	if !testing.Short() {
		runtime.LockOSThread()
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resMultiWindow.Store(multiWindowScenario())
		resEmbed.Store(embedScenario())
		resOpenPanel.Store(openPanelCompletionScenario())
		resDialogCfg.Store(dialogConfigScenario())
		resFirstMouse.Store(firstMouseScenario())
		resHitTest.Store(hitTestFirstMouseScenario())
		resRaise.Store(raiseScenario())
		resWindowState.Store(windowStateScenario())
		resOriginGate.Store(originGateScenario())
		resFrameGate.Store(frameGateScenario())
		resBadMessages.Store(badMessagesScenario())
		// Last: this scenario runs its own [NSApp run] as the "external" host.
		resExternalLoop.Store(externalLoopScenario())
	}
	os.Exit(m.Run())
}

// requireGUI skips a GUI assertion when its scenario did not run (e.g. -short).
func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("GUI scenarios skipped (-short)")
	}
}

// openPanelCompletionScenario exercises the WKUIDelegate file-chooser
// completion path (invokeOpenPanelCompletion / NSInvocation "v@?@") without
// presenting the modal panel, by invoking it with a Go block and a cancelled
// (nil) selection. The full panel UI is exercised manually via
// demos/filepicker.
func openPanelCompletionScenario() string {
	done := make(chan objc.ID, 1)
	block := objc.NewBlock(func(_ objc.Block, urls objc.ID) {
		select {
		case done <- urls:
		default:
		}
	})
	invokeOpenPanelCompletion(objc.ID(uintptr(block)), 0)
	select {
	case urls := <-done:
		if urls != 0 {
			return "urls=nonnil (want nil for cancel)"
		}
		return "panel-ok"
	case <-time.After(2 * time.Second):
		return "completion handler not invoked"
	}
}

// multiWindowScenario verifies window ref-count bookkeeping across two engines
// and that full Destroy returns the count to its baseline (no run loop needed).
func multiWindowScenario() string {
	start := atomic.LoadInt32(&windowCount)
	w1 := &View{}
	if err := testApp().Show(w1); err != nil {
		return "w1 error: " + err.Error()
	}
	w2 := &View{window: nil}
	if err := testApp().Show(w2); err != nil {
		return "w2 error: " + err.Error()
	}
	peak := atomic.LoadInt32(&windowCount)
	w1.Close()
	w2.Close()
	end := atomic.LoadInt32(&windowCount)
	return strconv.Itoa(int(start)) + "->" + strconv.Itoa(int(peak)) + "->" + strconv.Itoa(int(end))
}

// embedScenario embeds a web view into a caller-provided NSWindow and verifies
// the engine does not take ownership and Destroy leaves the host window intact.
func embedScenario() string {
	host := class("NSWindow").Send(sel("alloc"))
	host = host.Send(sel("initWithContentRect:styleMask:backing:defer:"),
		cgRect{cgPoint{0, 0}, cgSize{400, 300}},
		uint(nsWindowStyleMaskTitled), nsBackingStoreBuffered, false)
	host = host.Send(sel("retain"))

	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host)) // objc.ID -> unsafe.Pointer
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := native(w).ownsWindow // concrete type (same package)
	w.Close()

	// Host must still be alive after Destroy (this would crash on a released
	// object), then tear it down ourselves.
	host.Send(sel("setTitle:"), nsstr("still alive"))
	host.Send(sel("close"))
	host.Send(sel("release"))

	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestMultiWindowRefCount(t *testing.T) {
	const want = "0->2->0"
	got, _ := resMultiWindow.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("window ref-count = %q, want %q", got, want)
	}
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestOpenPanelCompletion(t *testing.T) {
	got, _ := resOpenPanel.Load().(string)
	requireGUI(t, got)
	if got != "panel-ok" {
		t.Fatalf("open-panel completion = %q, want %q", got, "panel-ok")
	}
}

// dialogConfigScenario verifies that configureOpenPanel maps dialog.Options
// onto the NSOpenPanel correctly, without presenting the modal (runModal is the
// only part that needs UI; the configuration is the part worth asserting). The
// full dialog is exercised manually via demos/dialog.
func dialogConfigScenario() string {
	res := "dialog-config-ok"
	autorelease(func() {
		// File mode: choose files, multiple selection, a two-extension filter
		// (one of them carrying a leading dot, which must be stripped).
		p := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(p, true, false, true, dialog.Options{
			Title:   "Pick a file",
			Filters: []dialog.FileFilter{{Name: "Images", Extensions: []string{"png", ".jpg"}}},
		})
		switch {
		case p.Send(sel("canChooseFiles")) == 0:
			res = "file: canChooseFiles=false"
		case p.Send(sel("canChooseDirectories")) != 0:
			res = "file: canChooseDirectories=true (want false)"
		case p.Send(sel("allowsMultipleSelection")) == 0:
			res = "file: allowsMultipleSelection=false"
		case int(p.Send(sel("allowedFileTypes")).Send(sel("count"))) != 2:
			res = "file: allowedFileTypes count != 2"
		}
		if res != "dialog-config-ok" {
			return
		}
		// Directory mode: files off, dirs on, and a wildcard filter must leave
		// the type restriction unset (allowedFileTypes nil).
		d := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(d, false, true, false, dialog.Options{
			Filters: []dialog.FileFilter{{Extensions: []string{"*"}}},
		})
		switch {
		case d.Send(sel("canChooseFiles")) != 0:
			res = "dir: canChooseFiles=true (want false)"
		case d.Send(sel("canChooseDirectories")) == 0:
			res = "dir: canChooseDirectories=false"
		case d.Send(sel("allowedFileTypes")) != 0:
			res = "dir: allowedFileTypes set (want nil for wildcard)"
		}
	})
	return res
}

func TestDialogConfig(t *testing.T) {
	got, _ := resDialogCfg.Load().(string)
	requireGUI(t, got)
	if got != "dialog-config-ok" {
		t.Fatalf("dialog config = %q, want %q", got, "dialog-config-ok")
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

// firstMouseScenario checks the opt-in end to end against the Objective-C
// runtime: the view a first-mouse web view is built from must ANSWER YES to
// acceptsFirstMouse:, and a default one must keep AppKit's NO. Asking the
// object itself is the point - a test that only compared class names would
// pass while the method was never installed.
func firstMouseScenario() string {
	ask := func(v *View) (string, bool) {
		if err := testApp().Show(v); err != nil {
			return "new error: " + err.Error(), false
		}
		defer v.Close()
		view := native(v).webView
		if view == 0 {
			return "no web view was created", false
		}
		if view.Send(sel("respondsToSelector:"), sel("acceptsFirstMouse:")) == 0 {
			return "the view does not respond to acceptsFirstMouse:", false
		}
		// A nil NSEvent is what AppKit passes when it asks about a view that is
		// not in a window yet, and neither implementation reads it.
		return "", bool(view.Send(sel("acceptsFirstMouse:"), objc.ID(0)) != 0)
	}

	msg, on := ask(&View{FirstMouse: true})
	if msg != "" {
		return msg
	}
	if !on {
		return "opted in, but the view still refuses the first mouse"
	}
	msg, off := ask(&View{})
	if msg != "" {
		return msg
	}
	if off {
		return "not opted in, but the view accepts the first mouse (the default must stay AppKit's)"
	}
	return "first-mouse-ok"
}

func TestFirstMouseIsOptIn(t *testing.T) {
	const want = "first-mouse-ok"
	got, _ := resFirstMouse.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("acceptsFirstMouse: got %q, want %q", got, want)
	}
}

// hitTestFirstMouseScenario checks the property that actually decides whether
// the opt-in works: AppKit asks the view its HIT TEST lands on, not the one we
// happen to hold a pointer to. If WebKit ever puts an internal subview in front
// of ours, the override would still answer YES to us and NO to the user's
// click - a silent, untestable-by-name regression.
func hitTestFirstMouseScenario() string {
	w := &View{FirstMouse: true}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	wv := native(w).webView
	win := native(w).window
	content := win.Send(sel("contentView"))
	hit := content.Send(sel("hitTest:"), cgPoint{200, 200})

	name := func(id objc.ID) string {
		if id == 0 {
			return "<nil>"
		}
		return cstr(id.Send(sel("className")).Send(sel("UTF8String")))
	}
	accepts := func(id objc.ID) string {
		if id == 0 {
			return "-"
		}
		if id.Send(sel("respondsToSelector:"), sel("acceptsFirstMouse:")) == 0 {
			return "no-selector"
		}
		if id.Send(sel("acceptsFirstMouse:"), objc.ID(0)) != 0 {
			return "YES"
		}
		return "NO"
	}
	if accepts(hit) != "YES" {
		return "the view under the cursor refuses the first mouse: hit=" +
			name(hit) + "/" + accepts(hit) + " webView=" + name(wv) + "/" + accepts(wv)
	}
	_ = content
	return "hit-test-ok"
}

func TestTheViewUnderTheCursorAcceptsTheFirstMouse(t *testing.T) {
	const want = "hit-test-ok"
	got, _ := resHitTest.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("hit-test first mouse: got %q, want %q", got, want)
	}
}

// raiseScenario checks that Raise leaves the window KEY. A window that rose but
// is not key is exactly the state Raise exists to escape: the next click on it
// is spent activating instead of pressing what it landed on.
func raiseScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	win := native(w).window

	// Order it out first, so "already key" cannot pass for a working Raise.
	win.Send(sel("orderOut:"), objc.ID(0))
	if win.Send(sel("isKeyWindow")) != 0 {
		return "the window is still key after orderOut: the fixture proves nothing"
	}

	w.Focus(true)
	if win.Send(sel("isKeyWindow")) == 0 {
		return "Raise left the window not key"
	}
	return "raise-ok"
}

func TestRaiseMakesTheWindowKey(t *testing.T) {
	const want = "raise-ok"
	got, _ := resRaise.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("Raise: got %q, want %q", got, want)
	}
}

// windowStateScenario verifies the frameless manual window-state controls.
// A borderless NSWindow cannot use AppKit's performZoom:/miniaturize: (those
// are titled-window features), so the darwin engine implements them manually:
// Maximize must grow the window to its screen's visible frame and Unmaximize
// must restore the original frame; Minimize hides (orderOut) and Unminimize
// brings it back. Frame geometry is read back from the real window.
// windowStateScenario verifies the frameless manual window-state controls. A
// borderless NSWindow can't use AppKit's built-in performZoom:/miniaturize:
// (those are wired only for titled windows), so the darwin engine implements
// them manually: Maximize must grow the window to its screen's visible frame
// and Unmaximize must restore the original frame; Minimize hides (orderOut) and
// Unminimize brings it back. Frame geometry is read back from the live window.
func windowStateScenario() string {
	w := &View{} // frameless, resizable default
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	wv := native(w)
	win := wv.window
	// Bring the window on screen and let display settle before we read frames.
	wv.runEventLoopWhile(func() bool { return win.Send(sel("isVisible")) == 0 })
	fr := func() cgRect { return objc.Send[cgRect](win, sel("frame")) }

	before := fr()
	wv.maximized = false
	w.Maximize()
	wv.runEventLoopWhile(func() bool { return !wv.maximized })
	mid := fr()
	w.Unmaximize()
	wv.runEventLoopWhile(func() bool { return wv.maximized })
	after := fr()

	if !(mid.Size.Width > before.Size.Width && mid.Size.Height > before.Size.Height) {
		return "maximize did not enlarge the frameless window (" + ws(before) + " -> " + ws(mid) + ")"
	}
	if !(rEq(after.Origin.X, before.Origin.X) && rEq(after.Origin.Y, before.Origin.Y) &&
		rEq(after.Size.Width, before.Size.Width) && rEq(after.Size.Height, before.Size.Height)) {
		return "unmaximize did not restore the frameless frame (" + ws(before) + " -> " + ws(after) + ")"
	}

	wv.minimized = false
	w.Minimize()
	wv.runEventLoopWhile(func() bool { return !wv.minimized })
	hidden := win.Send(sel("isVisible")) == 0
	w.Unminimize()
	wv.runEventLoopWhile(func() bool { return wv.minimized })
	shown := win.Send(sel("isVisible")) != 0
	if !hidden {
		return "frameless minimize did not hide the window"
	}
	if !shown {
		return "frameless unminimize did not restore the window"
	}
	return "window-state-ok"
}

// ws formats a frame for a diagnostic message.
func ws(r cgRect) string {
	return strconv.FormatFloat(r.Size.Width, 'f', 0, 64) + "x" +
		strconv.FormatFloat(r.Size.Height, 'f', 0, 64) + "@" +
		strconv.FormatFloat(r.Origin.X, 'f', 0, 64) + "," +
		strconv.FormatFloat(r.Origin.Y, 'f', 0, 64)
}

// rEq compares two floats for the purposes of frame equality (1px tolerance).
func rEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1
}

func TestFramelessWindowState(t *testing.T) {
	got, _ := resWindowState.Load().(string)
	requireGUI(t, got)
	const want = "window-state-ok"
	if got != want {
		t.Fatalf("frameless window state: got %q, want %q", got, want)
	}
}

// externalLoopScenario covers a host application that already drives
// [NSApp run] on the main thread (native UI or a tray) while the webview is
// created from a plain goroutine. New must not start a second [NSApp run]
// on another thread - that hangs waiting for an applicationDidFinishLaunching
// that has already fired. The scenario drives the full lifecycle - create,
// configure, Run, Terminate, Destroy - and checks the host loop survives
// the window.
func externalLoopScenario() string {
	app := class("NSApplication").Send(sel("sharedApplication"))
	res := make(chan string, 1)

	go func() {
		verdict := func() string {
			for i := 0; app.Send(sel("isRunning")) == 0; i++ {
				if i > 500 {
					return "host loop never started"
				}
				time.Sleep(10 * time.Millisecond)
			}
			done := make(chan string, 1)
			go func() {
				w := &View{}
				if err := testApp().Show(w); err != nil {
					done <- "new error: " + err.Error()
					return
				}
				defer w.Close()
				native(w).loadHTML("<html><body>external loop</body></html>")
				go func() {
					time.Sleep(500 * time.Millisecond)
					w.Close()
				}()
				w.w.Run()
				done <- "external-loop-ok"
			}()
			select {
			case s := <-done:
				if s != "external-loop-ok" {
					return s
				}
			case <-time.After(15 * time.Second):
				return "timeout: New or Run blocked under a running loop (deadlock)"
			}

			// Second shape: the whole lifecycle issued ON the UI thread from
			// inside a run-loop callout - what a tray OnClick does when it
			// calls appkit synchronously. Run must pump events instead of
			// block-waiting, or it deadlocks the very loop that would deliver
			// the close.
			syncRes := make(chan string, 1)
			dispatchMain(func() {
				w := &View{}
				if err := testApp().Show(w); err != nil {
					syncRes <- "sync new error: " + err.Error()
					return
				}
				defer w.Close()
				native(w).loadHTML("<html><body>external loop, sync shape</body></html>")
				go func() {
					time.Sleep(300 * time.Millisecond)
					w.Close()
				}()
				w.w.Run()
				syncRes <- "external-loop-ok"
			})
			select {
			case s := <-syncRes:
				return s
			case <-time.After(15 * time.Second):
				return "timeout: sync (OnClick-shaped) lifecycle hung"
			}
		}()
		if app.Send(sel("isRunning")) == 0 {
			// The webview must not have stopped the host's loop on its way out.
			verdict += " (webview close stopped the host loop)"
		}
		res <- verdict
		// Stop the host loop; the scenario owns it, appkit must not.
		dispatchMain(func() {
			autorelease(func() {
				app.Send(sel("stop:"), objc.ID(0))
				postWakeEvent(app)
			})
		})
	}()

	app.Send(sel("run")) // the "tray": owns the run loop on the main thread
	return <-res
}

func TestNewUnderAnExternalRunLoop(t *testing.T) {
	const want = "external-loop-ok"
	got, _ := resExternalLoop.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("external run loop: got %q, want %q", got, want)
	}
}

// badMessagesScenario posts what a hostile or careless page can post: bodies
// that are not strings, and a well-formed bridge message from a frame. The
// process must survive the first, the gate must drop the second, and the
// page's own call must still arrive.
func badMessagesScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	var hits atomic.Int32
	done := make(chan string, 1)
	_ = w.w.Bind("hit", func() { hits.Add(1) })
	_ = w.w.Bind("done", func() {
		select {
		case done <- "":
		default:
		}
		w.Close()
	})
	time.AfterFunc(15*time.Second, func() { w.Close() })

	native(w).loadHTML(`<!DOCTYPE html><html><body>
<iframe srcdoc="<script>window.webkit.messageHandlers.__webview__.postMessage(JSON.stringify({id:'f1',method:'hit',params:[]}));</script>"></iframe>
<script>
window.addEventListener('load', function(){
  var h = window.webkit.messageHandlers.__webview__;
  h.postMessage({});
  h.postMessage(42);
  h.postMessage(null);
  h.postMessage([1, 2]);
  setTimeout(function(){ window.done(); }, 500);
});
</script></body></html>`)
	w.w.Run()

	select {
	case <-done:
		return fmt.Sprintf("alive frameHits=%d", hits.Load())
	default:
		return "no report"
	}
}

// TestBadMessagesAreDropped checks that non-string bodies cannot crash the
// process and that a frame cannot call a binding.
func TestBadMessagesAreDropped(t *testing.T) {
	got, _ := resBadMessages.Load().(string)
	requireGUI(t, got)
	if got != "alive frameHits=0" {
		t.Fatalf("bad messages = %q, want %q", got, "alive frameHits=0")
	}
}
