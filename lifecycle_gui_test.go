package tuohi

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

var resLifecycle atomic.Value

// TestMain invokes this on the UI thread. Native close helpers exercise each
// toolkit's user/keyboard close path, rather than the unconditional Destroy.
func lifecycleScenario() string {
	app := testApp()
	ready := make(chan struct{})
	seen := make(chan string, 2)
	requests := make(chan *CloseRequest, 4)
	var count atomic.Int32
	var hidden, visible, terminal atomic.Int32
	v := &View{Frame: true, Width: 400, Height: 300, URL: "data:text/html,<p>Retained view</p>", Ready: func() { close(ready) }, Bind: map[string]any{"seen": func(s string) { seen <- s }}}
	v.CloseRequested = func(r *CloseRequest) {
		if !onUIThread() {
			panic("close callback off UI")
		}
		if count.Add(1) == 1 {
			r.Cancel()
			v.Hide()
		} else {
			r.Defer()
			requests <- r
		}
	}
	v.WindowChanged = func(s WindowState) {
		if !onUIThread() {
			panic("state callback off UI")
		}
		if s.Closed {
			terminal.Add(1)
		} else if s.Visible {
			visible.Add(1)
		} else {
			hidden.Add(1)
		}
	}
	if err := app.Show(v); err != nil {
		return err.Error()
	}
	e := v.live()
	defer v.Close()
	peerRequests := make(chan *CloseRequest, 2)
	peer := &View{Frame: true, Width: 200, Height: 100, CloseRequested: func(r *CloseRequest) { r.Defer(); peerRequests <- r }}
	if err := app.Show(peer); err != nil {
		return err.Error()
	}
	defer peer.Close()
	ordinary := &View{Frame: true, Width: 200, Height: 100, CloseRequested: func(r *CloseRequest) { r.Allow() }}
	if err := app.Show(ordinary); err != nil {
		return err.Error()
	}
	defer ordinary.Close()
	result := make(chan string, 1)
	watchdog := time.AfterFunc(30*time.Second, func() { v.Close() })
	defer watchdog.Stop()
	go func() {
		fail := func(s string) { result <- s; v.Close() }
		wait := func(predicate func() bool) bool {
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				if predicate() {
					return true
				}
				time.Sleep(10 * time.Millisecond)
			}
			return false
		}
		select {
		case <-ready:
		case <-time.After(15 * time.Second):
			fail("page never ready")
			return
		}
		var original uintptr
		if err := ui.call(func() { original = uintptr(e.Window()); e.Eval(`window.retained = 'kept'; window.seen('set')`) }); err != nil {
			fail(err.Error())
			return
		}
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			fail("initial JS missing")
			return
		}
		if err := ui.call(func() { nativeRequestClose(ordinary.live().(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return ordinary.WindowState().Closed }) {
			fail("allowed native close did not publish terminal state")
			return
		}
		if err := ui.call(func() { nativeActivateForTest(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return v.WindowState().Active }) {
			fail("native activation event missing")
			return
		}
		if err := ui.call(func() { nativeActivateForTest(peer.live().(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return !v.WindowState().Active }) {
			fail("native deactivation event missing")
			return
		}
		if err := ui.call(func() { nativeRequestClose(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return count.Load() == 1 && !v.WindowState().Visible && hidden.Load() > 0 }) || v.live() != e {
			fail("cancel/hide lost the window or visibility event")
			return
		}
		var same bool
		if err := ui.call(func() { v.Show(); same = uintptr(e.Window()) == original; e.Eval(`window.seen(window.retained)`) }); err != nil {
			fail(err.Error())
			return
		}
		select {
		case s := <-seen:
			if s != "kept" {
				fail("renderer state lost: " + s)
				return
			}
		case <-time.After(5 * time.Second):
			fail("retained JS missing")
			return
		}
		if !same || !wait(func() bool { return v.WindowState().Visible && visible.Load() >= 2 }) {
			fail("show did not restore same native window")
			return
		}
		if err := ui.call(func() { nativeRequestClose(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		var r *CloseRequest
		select {
		case r = <-requests:
		case <-time.After(5 * time.Second):
			fail("deferred request missing")
			return
		}
		if err := ui.call(v.RequestClose); err != nil {
			fail(err.Error())
			return
		}
		if count.Load() != 2 || !r.Cancel() {
			fail("deferred cancel/coalescing failed")
			return
		}
		if err := ui.call(func() { nativeRequestClose(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		select {
		case r = <-requests:
		case <-time.After(5 * time.Second):
			fail("second deferred request missing")
			return
		}
		if !r.Allow() {
			fail("deferred allow refused")
			return
		}
		result <- "ok"
	}()
	e.Run()
	select {
	case r := <-result:
		if r != "ok" {
			return r
		}
	case <-time.After(time.Second):
		return "loop ended without report"
	}
	if terminal.Load() != 1 || count.Load() != 3 || !v.WindowState().Closed || v.live() != nil {
		return fmt.Sprintf("terminal=%d requests=%d state=%+v", terminal.Load(), count.Load(), v.WindowState())
	}
	peer.RequestClose()
	var pending *CloseRequest
	select {
	case pending = <-peerRequests:
	default:
		return "quit test has no pending request"
	}
	quit := make(chan struct{})
	go func() { app.Quit(); close(quit) }()
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		return "App.Quit waited for deferred close"
	}
	select {
	case <-pending.Done():
	default:
		return "App.Quit failed to invalidate deferred close"
	}
	if pending.Allow() {
		return "quit request accepted late decision"
	}
	peer.Close()
	if len(peerRequests) != 0 {
		return "programmatic teardown after quit was intercepted"
	}
	return "lifecycle-ok"
}

func TestNativeWindowLifecycle(t *testing.T) {
	got, _ := resLifecycle.Load().(string)
	requireGUI(t, got)
	if got != "lifecycle-ok" {
		t.Fatal(got)
	}
}
