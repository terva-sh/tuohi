package tuohi

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resCloseFromGo atomic.Value // string

// closeFromGoScenario shows one window under App{Exit: true}, closes it with
// View.Close from a goroutine once its page has loaded, and checks that
// App.Wait returns. A watchdog quits the app if it does not, so a hang is
// reported rather than stalling the run (TKT-01M3N0PTS10TG629DVNEFSNVSZ).
//
// Wait runs a loop of its own, so each engine's TestMain runs this scenario
// after the ones that drive a view's loop directly.
func closeFromGoScenario() string {
	app := &App{Exit: true}
	ready := make(chan struct{})
	var once sync.Once
	w := &View{
		Width: 400, Height: 300,
		URL:   "data:text/html,<p>close from Go</p>",
		Ready: func() { once.Do(func() { close(ready) }) },
	}
	if err := app.Show(w); err != nil {
		return "view error: " + err.Error()
	}
	scope := scopePtr.Load()
	var noReady atomic.Bool
	go func() {
		select {
		case <-ready:
		case <-time.After(15 * time.Second):
			noReady.Store(true)
		}
		w.Close()
	}()
	var hung atomic.Bool
	var atHang atomic.Value // string
	watchdog := time.AfterFunc(30*time.Second, func() {
		// What the scope saw, before Quit changes it: whether the close was
		// counted, and whether an exit was already requested.
		atHang.Store(fmt.Sprintf(" (windows=%d exit=%d)", atomic.LoadInt32(&scope.windows), atomic.LoadInt32(&scope.exitFlag)))
		hung.Store(true)
		app.Quit()
	})
	err := app.Wait()
	// The App scope counts a window as closed only when the engine reports
	// its close, so Wait returning with the count at zero means the close
	// reached it, rather than Wait returning for some other reason.
	open := atomic.LoadInt32(&scope.windows)
	watchdog.Stop()
	switch {
	case noReady.Load():
		return "the page never loaded, so the close was not tested after Ready"
	case hung.Load():
		s, _ := atHang.Load().(string)
		return "Wait did not return after View.Close from a goroutine" + s
	case open != 0:
		return fmt.Sprintf("Wait returned with %d window(s) still counted open", open)
	case err != nil:
		return "wait error: " + err.Error()
	}
	return "wait-ok"
}

// TestWaitReturnsAfterCloseFromGo checks that a window closed with View.Close
// from a goroutine ends App.Wait when App.Exit is set, as a window the user
// closes does.
func TestWaitReturnsAfterCloseFromGo(t *testing.T) {
	got, _ := resCloseFromGo.Load().(string)
	requireGUI(t, got)
	if got != "wait-ok" {
		t.Fatalf("close from Go = %q, want %q", got, "wait-ok")
	}
}
