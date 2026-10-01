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
	go func() {
		defer first.Close()
		// Wait for the first view's Run, the loop the Shows are handed to.
		// A queued function is no sign of it: on Windows loadHTML pumps
		// messages while it rebuilds the page's scripts, before Run starts,
		// and GitHub run 36905378959 ran the Shows there, with no loop
		// counted. ui.call succeeds only while a counted loop runs.
		deadline := time.Now().Add(15 * time.Second)
		for ui.call(func() {}) != nil {
			if time.Now().After(deadline) {
				result <- "the first view's loop never ran"
				return
			}
			time.Sleep(50 * time.Millisecond)
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
				shows = "failed"
			}
		}
		if errs[0] != nil && errs[1] != nil {
			shows = "failed"
		}
		r := fmt.Sprintf("show=%s live=%v hit=%s hits=%d ready=%d",
			shows, live, hit, 1+len(hits), readies.Load())
		if shows != "ok" {
			// Each Show's own error: GitHub run 36822614924 reported only
			// that both failed, which left the cause to guess.
			r += fmt.Sprintf(" [errs: %v | %v]", errs[0], errs[1])
		}
		result <- r
	}()

	native(first).loadHTML(`<!DOCTYPE html><html><body>first</body></html>`)
	first.w.Run()
	// Run can end before the goroutine reports: closing the last counted
	// window stops the loop on its own.
	var r string
	select {
	case r = <-result:
	case <-time.After(20 * time.Second):
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
