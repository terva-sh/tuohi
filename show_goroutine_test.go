package tuohi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resShowGoroutine atomic.Value // string

// showGoroutineScenario shows a second View from goroutines while the first
// view's loop runs on the UI thread (TKT-01M3N0PTQ7ZY1X78PX70SZ56AN). Two
// goroutines call App.Show on the same View at once. Each must return nil,
// or errShowInProgress when the first creation ran it (WebView2 pumps
// messages while it creates), and at least one must return nil. Exactly one
// window may be created, and its page must load and call a binding, which
// needs a window built on the UI thread. A Show off the UI thread with no
// loop running must refuse and create nothing.
func showGoroutineScenario() string {
	app := testApp()
	first := &View{}
	if err := app.Show(first); err != nil {
		return "new error: " + err.Error()
	}
	defer first.Close()

	hits := make(chan string, 4)
	var readies atomic.Int32
	second := &View{
		URL:   "data:text/html,<script>addEventListener('load',function(){window.hit('two')})</script>",
		Bind:  map[string]any{"hit": func(s string) { hits <- s }},
		Ready: func() { readies.Add(1) },
	}

	result := make(chan string, 1)
	time.AfterFunc(45*time.Second, func() { first.Close() })
	loopUp := make(chan struct{})
	go func() {
		defer first.Close()
		// Queued now, run once the first view's loop drains the queue.
		ui.run(func() { close(loopUp) })
		select {
		case <-loopUp:
		case <-time.After(15 * time.Second):
			result <- "the first view's loop never ran"
			return
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = app.Show(second)
			}()
		}
		wg.Wait()
		live := second.live() != nil
		hit := "none"
		select {
		case hit = <-hits:
		case <-time.After(15 * time.Second):
		}
		// A second window would load the page and report too.
		time.Sleep(time.Second)
		second.Close()
		shows := "ok"
		for _, err := range errs {
			if err != nil && !errors.Is(err, errShowInProgress) {
				shows = err.Error()
			}
		}
		if errs[0] != nil && errs[1] != nil {
			shows = "neither Show returned nil"
		}
		result <- fmt.Sprintf("show=%s live=%v hit=%s hits=%d ready=%d",
			shows, live, hit, 1+len(hits), readies.Load())
	}()

	native(first).loadHTML(`<!DOCTYPE html><html><body>first</body></html>`)
	first.w.Run()
	var r string
	select {
	case r = <-result:
	default:
		return "no report"
	}

	// With the loop stopped, a Show from another goroutine has no UI thread
	// to run on.
	idle := &View{}
	refused := make(chan error, 1)
	go func() { refused <- app.Show(idle) }()
	select {
	case err := <-refused:
		r += fmt.Sprintf(" idle=%v,%v", err != nil, idle.live() != nil)
	case <-time.After(10 * time.Second):
		r += " idle=blocked"
	}
	return r
}

// TestShowFromGoroutine checks that App.Show off the UI thread creates the
// window on the UI thread, once, and refuses when no loop runs there.
func TestShowFromGoroutine(t *testing.T) {
	got, _ := resShowGoroutine.Load().(string)
	requireGUI(t, got)
	want := "show=ok live=true hit=two hits=1 ready=1 idle=true,false"
	if got != want {
		t.Fatalf("Show from a goroutine:\n got %s\nwant %s", got, want)
	}
}
