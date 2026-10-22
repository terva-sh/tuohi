package tuohi

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resGoroutineCalls atomic.Value // string

// goroutineCallsScenario checks that View's methods work when called from a
// binding, which runs on its own goroutine, off the UI thread. The page at /a
// calls the "step" binding, and the bindings call Eval, Focus, and Navigate
// on the View directly, with no Dispatch. Eval makes the page report through
// the "shown" binding, which then calls Navigate to load /b and report its
// path. Waiting for Eval's report matters: evaluation is asynchronous, so an
// immediate navigation can replace the page before the script executes.
func goroutineCallsScenario() string {
	base, closeServer, err := servePlain(func(target string) (string, string) {
		return "", `<!DOCTYPE html><html><body><script>
window.addEventListener('pageshow', function(){
  window.shown(location.pathname);
  if (location.pathname === '/a') { window.step(); }
});
</script></body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeServer()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	shown := make(chan string, 16)
	var offUI atomic.Bool
	_ = w.w.Bind("shown", func(s string) {
		shown <- s
		if s == "eval:/a" {
			offUI.Store(offUI.Load() && !onUIThread())
			w.Navigate(base + "b")
		}
	})
	_ = w.w.Bind("step", func() {
		offUI.Store(!onUIThread())
		w.Eval(`window.shown('eval:' + location.pathname)`)
		w.Focus(true)
	})

	result := make(chan string, 1)
	watchdog := time.AfterFunc(30*time.Second, func() { w.Close() })
	defer watchdog.Stop()
	go func() {
		defer w.Close()
		// One deadline for all reports expires before the emergency close, so
		// failures retain their partial observations instead of "no report".
		deadline := time.After(20 * time.Second)
		expired := false
		next := func() string {
			if expired {
				return "none"
			}
			select {
			case s := <-shown:
				return s
			case <-deadline:
				expired = true
				return "none"
			}
		}
		got := []string{"start=" + next(), "eval=" + next(), "navigate=" + next()}
		result <- fmt.Sprintf("%s offUI=%v", strings.Join(got, " "), offUI.Load())
	}()

	w.w.Navigate(base + "a")
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// TestViewMethodsFromBinding checks that Eval, Focus, and Navigate called
// from a binding goroutine reach the view, on every engine.
func TestViewMethodsFromBinding(t *testing.T) {
	got, _ := resGoroutineCalls.Load().(string)
	requireGUI(t, got)
	want := "start=/a eval=eval:/a navigate=/b offUI=true"
	if got != want {
		t.Fatalf("View methods from a binding:\n got %s\nwant %s", got, want)
	}
}
