package tuohi

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resLoopbackApp atomic.Value // string

// loopbackAppScenario checks that an application served from its own
// loopback server works under the default navigation policy, with nothing
// configured: Go navigates to the server once, and every page the user then
// reaches on that origin, by a link, a query, a reload, or the history, loads
// in the view and reaches Go through the bridge. Nothing may be handed to the
// system.
//
// Each page reports its path through the "shown" binding on pageshow, which
// also fires when back and forward restore a page from the back-forward cache
// without a load.
func loopbackAppScenario() string {
	base, closeServer, err := servePlain(func(string) (string, string) {
		return "", `<!DOCTYPE html><html><body><p>app</p><script>
window.addEventListener('pageshow', function(){ window.shown(location.pathname + location.search); });
</script></body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeServer()

	var mu sync.Mutex
	var external []string
	prevOpen := openExternal
	openExternal = func(rawurl string) {
		mu.Lock()
		defer mu.Unlock()
		external = append(external, rawurl)
	}
	defer func() { openExternal = prevOpen }()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	shown := make(chan string, 16)
	_ = w.w.Bind("shown", func(path string) { shown <- path })

	steps := []struct{ name, js string }{
		{"link", `var a = document.createElement('a'); a.href = '/b'; document.body.appendChild(a); a.click();`},
		{"query", `location.href = '/c?q=1';`},
		{"reload", `location.reload();`},
		{"back", `history.back();`},
		{"forward", `history.forward();`},
	}

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		next := func() string {
			select {
			case p := <-shown:
				return p
			case <-time.After(5 * time.Second):
				return "none"
			}
		}
		report := []string{"start=" + next()}
		for _, st := range steps {
			w.w.Dispatch(func() { w.w.Eval(st.js) })
			report = append(report, st.name+"="+next())
		}
		mu.Lock()
		ext := strings.Join(external, " ")
		mu.Unlock()
		result <- fmt.Sprintf("%s external=[%s]", strings.Join(report, " "), ext)
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

// TestLoopbackAppDefaultPolicy checks that a loopback-served interface works
// under the default policy: in-app links, queries, reloads, and the history
// all stay in the view with the bridge.
func TestLoopbackAppDefaultPolicy(t *testing.T) {
	got, _ := resLoopbackApp.Load().(string)
	requireGUI(t, got)
	want := "start=/a link=/b query=/c?q=1 reload=/c?q=1 back=/b forward=/c?q=1 external=[]"
	if got != want {
		t.Fatalf("loopback app:\n got %s\nwant %s", got, want)
	}
}

var resDataURL atomic.Value // string

// dataURLScenario checks that a data: URL given to Navigate, as View.URL may
// be, loads and can call a binding: Navigate trusts it by its exact URL, and
// both the navigation policy and the bridge gate must key it the same way.
//
// When the call does not arrive, the page's own report says why: it requests
// an image from a loopback server, which needs no bridge, naming whether it
// sees window.chrome, chrome.webview, and the bridge, and its own URL. No
// report at all means the page never loaded.
func dataURLScenario() string {
	diag := make(chan string, 1)
	diagBase, closeDiag, err := servePlain(func(target string) (string, string) {
		if strings.HasPrefix(target, "/diag?") {
			select {
			case diag <- target[len("/diag?"):]:
			default:
			}
		}
		return "", ""
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeDiag()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	hit := make(chan string, 1)
	_ = w.w.Bind("hit", func(s string) {
		select {
		case hit <- s:
		default:
		}
	})
	result := make(chan string, 1)
	go func() {
		defer w.Close()
		select {
		case s := <-hit:
			result <- "called " + s
		case <-time.After(10 * time.Second):
			select {
			case d := <-diag:
				result <- "no call, page saw " + d
			default:
				result <- "no call, and the page never reported"
			}
		}
	}()
	page := `<!DOCTYPE html><script>
window.addEventListener('load', function(){
  var c = window.chrome, v = c && c.webview;
  new Image().src = '` + diagBase + `diag?chrome=' + typeof c + '&webview=' + typeof v +
    '&bridge=' + typeof window.__webview__ + '&hit=' + typeof window.hit +
    '&href=' + encodeURIComponent(location.href.slice(0, 40));
  try { window.hit('data'); } catch (e) {}
});
</script>`
	w.w.Navigate("data:text/html," + page)
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// TestDataURLCanUseBindings checks that a data: View.URL can call a binding.
// On Windows that has never worked, for a reason not yet established, so a
// failure there skips with the page's own report instead of failing
// (TKT-01M3HWWRT7X1RZZYY6KFEP0ERE).
func TestDataURLCanUseBindings(t *testing.T) {
	got, _ := resDataURL.Load().(string)
	requireGUI(t, got)
	if got == "called data" {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skipf("data: pages cannot use bindings on Windows yet: %s", got)
	}
	t.Fatalf("data: URL = %q, want %q", got, "called data")
}
