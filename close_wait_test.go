package tuohi

import (
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
	watchdog := time.AfterFunc(30*time.Second, func() {
		hung.Store(true)
		app.Quit()
	})
	err := app.Wait()
	watchdog.Stop()
	switch {
	case noReady.Load():
		return "the page never loaded, so the close was not tested after Ready"
	case hung.Load():
		return "Wait did not return after View.Close from a goroutine"
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
