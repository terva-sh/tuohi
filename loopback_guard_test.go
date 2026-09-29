package tuohi

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resLoopbackLate atomic.Value // string

// loopbackLateScenario serves App.FS over the loopback server (App.HTTP, which
// macOS always uses) and has the page fetch from it well after its load, as a
// lazy import or a route change would. It reports what the page's location
// shows and what each fetch returned: one by a relative URL, one by an
// absolute path.
func loopbackLateScenario() string {
	app := &App{HTTP: true, FS: fsys(map[string]string{
		"index.html": `<!DOCTYPE html><html><body><p>late</p></body></html>`,
		"data.txt":   "late data",
	})}
	ready := make(chan struct{}, 1)
	got := make(chan string, 4)
	w := &View{
		URL:  "app://app/index.html",
		Bind: map[string]any{"report": func(s string) { got <- s }},
		Ready: func() {
			select {
			case ready <- struct{}{}:
			default:
			}
		},
	}
	if err := app.Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		select {
		case <-ready:
		case <-time.After(15 * time.Second):
			result <- "no ready"
			return
		}
		// Longer than the idle timeout the server used to have.
		time.Sleep(4500 * time.Millisecond)
		w.Eval(`(async function(){
  var out = ['path=' + location.pathname + location.search];
  for (var u of ['data.txt', '/data.txt']) {
    try { var r = await fetch(u); out.push(u + '=' + r.status + ':' + (await r.text())); }
    catch (e) { out.push(u + '=error'); }
  }
  window.report(out.join(' '));
})();`)
		select {
		case s := <-got:
			result <- s
		case <-time.After(10 * time.Second):
			result <- "no report"
		}
	}()

	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no result"
	}
}

// TestLoopbackLateFetch checks that a page served from the loopback server can
// still fetch from App.FS long after it loaded, by a relative URL and by an
// absolute path, and that its location shows no token.
func TestLoopbackLateFetch(t *testing.T) {
	got, _ := resLoopbackLate.Load().(string)
	requireGUI(t, got)
	want := "path=/index.html data.txt=200:late data /data.txt=200:late data"
	if got != want {
		t.Fatalf("late fetch:\n got %s\nwant %s", got, want)
	}
	if strings.Contains(got, ".tuohi") {
		t.Fatalf("the page's location shows the token prefix: %s", got)
	}
}
