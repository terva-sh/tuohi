package tuohi

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var resLifecycle atomic.Value

// TestMain invokes this on the UI thread. Native close helpers exercise each
// toolkit's user/keyboard close path, rather than the unconditional Destroy.
func lifecycleScenario() string {
	app := testApp()
	var stage atomic.Value
	stage.Store("creating lifecycle windows")
	// Scenarios run before m.Run, so Go's -test.timeout is not active yet.
	// Keep this armed through deferred cleanup, too: native teardown can stall.
	hardWatchdog := time.AfterFunc(35*time.Second, func() {
		stack := make([]byte, 1<<20)
		n := runtime.Stack(stack, true)
		panic(fmt.Sprintf("native lifecycle scenario stalled: %s\n%s", stage.Load(), stack[:n]))
	})
	defer hardWatchdog.Stop()
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
	stage.Store("waiting for application loop")
	var timedOut atomic.Bool
	watchdog := time.AfterFunc(30*time.Second, func() {
		timedOut.Store(true)
		app.Quit()
	})
	defer watchdog.Stop()
	started := make(chan struct{})
	v.Window(func(_ unsafe.Pointer) { close(started) })
	var pending *CloseRequest
	workerDone := make(chan struct{})
	go func() {
		defer func() {
			app.Quit()
			close(workerDone)
		}()
		fail := func(s string) { result <- stage.Load().(string) + ": " + s }
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
		case <-started:
		case <-time.After(5 * time.Second):
			fail("application loop did not start")
			return
		}
		stage.Store("waiting for page ready")
		select {
		case <-ready:
		case <-time.After(15 * time.Second):
			fail("page never ready")
			return
		}
		stage.Store("checking initial renderer state")
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
		stage.Store("allowing ordinary native close")
		if err := ui.call(func() { nativeRequestClose(ordinary.live().(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return ordinary.WindowState().Closed }) {
			fail("allowed native close did not publish terminal state")
			return
		}
		stage.Store("checking native activation")
		if err := ui.call(func() { nativeActivateForTest(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return v.WindowState().Active }) {
			fail("native activation event missing")
			return
		}
		stage.Store("checking native deactivation")
		if err := ui.call(func() { nativeActivateForTest(peer.live().(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return !v.WindowState().Active }) {
			fail("native deactivation event missing")
			return
		}
		stage.Store("cancelling and hiding retained window")
		if err := ui.call(func() { nativeRequestClose(e.(*webview)) }); err != nil {
			fail(err.Error())
			return
		}
		if !wait(func() bool { return count.Load() == 1 && !v.WindowState().Visible && hidden.Load() > 0 }) || v.live() != e {
			fail("cancel/hide lost the window or visibility event")
			return
		}
		stage.Store("restoring retained renderer")
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
		stage.Store("deferring and coalescing close requests")
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
		stage.Store("allowing deferred close")
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
		if !wait(func() bool { return v.WindowState().Closed && terminal.Load() == 1 }) {
			fail("deferred allow did not publish terminal state")
			return
		}
		if count.Load() != 3 || v.live() != nil {
			fail(fmt.Sprintf("requests=%d state=%+v", count.Load(), v.WindowState()))
			return
		}
		stage.Store("deferring peer close before app quit")
		if err := ui.call(peer.RequestClose); err != nil {
			fail(err.Error())
			return
		}
		select {
		case pending = <-peerRequests:
		default:
			fail("quit test has no pending request")
			return
		}
		stage.Store("quitting app with deferred peer close")
		result <- "ok"
	}()
	waitErr := app.Wait()
	// Wait can return after Quit sets the exit flag but before Quit returns.
	// Join the worker on every path while the hard watchdog is still armed.
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		return "App.Quit waited for deferred close: " + stage.Load().(string)
	}
	if waitErr != nil {
		return waitErr.Error()
	}
	if timedOut.Load() {
		return "lifecycle timeout: " + stage.Load().(string)
	}
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
