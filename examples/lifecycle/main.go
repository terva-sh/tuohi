// Command lifecycle preserves a window across close/hide/show and illustrates
// asynchronous close decisions. Run from examples: go run ./lifecycle.
package main

import (
	"embed"
	"log"
	"sync/atomic"
	"time"

	"github.com/terva-sh/tuohi"
)

//go:embed page.html
var page embed.FS

func main() {
	app := &tuohi.App{Name: "tuohi lifecycle", FS: page, Exit: true}
	var keep atomic.Bool
	keep.Store(true)
	view := &tuohi.View{Frame: true, Width: 640, Height: 400, URL: "app://page.html"}
	view.Bind = map[string]any{
		"demo.close": view.RequestClose,
		"demo.quit":  app.Quit,
		"demo.keep":  func(value bool) { keep.Store(value) },
	}
	view.CloseRequested = func(r *tuohi.CloseRequest) {
		if keep.Load() {
			r.Cancel()
			view.Hide()
			// This demo has no tray: restore automatically so its only window
			// stays reachable. Real adapters own their background/tray policy.
			time.AfterFunc(2*time.Second, view.Show)
			return
		}
		r.Defer()
		go func() {
			// Stand-in for saving changes or another asynchronous decision.
			// Never block the native callback while doing that work.
			select {
			case <-time.After(250 * time.Millisecond):
				r.Allow()
			case <-r.Done(): // teardown/quit invalidated the pending request
			}
		}()
	}
	view.WindowChanged = func(state tuohi.WindowState) {
		log.Printf("visible=%v active=%v closed=%v", state.Visible, state.Active, state.Closed)
	}
	if err := app.Show(view); err != nil {
		log.Fatal(err)
	}
	defer view.Close()
	if err := app.Wait(); err != nil {
		log.Fatal(err)
	}
}
