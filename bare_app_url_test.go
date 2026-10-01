package tuohi

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resBareAppURL atomic.Value // string

// bareAppURLScenario shows an App.FS view at a bare "app://#bare", with no
// host, once served natively and once under App.HTTP (which macOS always
// uses). The page reports its fragment through a binding when it loads, so a
// report means the page loaded from App.FS with its bridge and kept the
// fragment (TKT-01M3R23M9PWBF3VB2QD76SRSHH).
//
// It runs after other App.FS views, with other filesystems, have closed. On
// WebKitGTK the native view therefore also checks that its requests are
// answered from its own filesystem, not by the first view that registered
// the scheme (TKT-01M3TZ0AWR4PHN56MT2H9Z39RE).
func bareAppURLScenario() string {
	var out []string
	for _, served := range []struct {
		name string
		http bool
	}{{"native", false}, {"http", true}} {
		out = append(out, served.name+"="+bareAppURLView(served.http))
	}
	return strings.Join(out, " ")
}

func bareAppURLView(http bool) string {
	app := &App{HTTP: http, FS: fsys(map[string]string{
		"index.html": `<!DOCTYPE html><html><body><p>bare</p><script>
window.addEventListener('load', function(){ window.report('root' + location.hash); });
</script></body></html>`,
	})}
	got := make(chan string, 1)
	w := &View{
		URL: "app://#bare",
		Bind: map[string]any{"report": func(s string) {
			select {
			case got <- s:
			default:
			}
		}},
	}
	if err := app.Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	result := make(chan string, 1)
	time.AfterFunc(30*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		select {
		case s := <-got:
			result <- s
		case <-time.After(15 * time.Second):
			result <- "no call"
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

// TestBareAppURLHasBridge checks that a view navigated to a bare app:// loads
// the App.FS root with its bindings, and keeps the URL's fragment, whether
// the content is served natively or over the loopback server.
func TestBareAppURLHasBridge(t *testing.T) {
	got, _ := resBareAppURL.Load().(string)
	requireGUI(t, got)
	if want := "native=root#bare http=root#bare"; got != want {
		t.Fatalf("bare app:// = %q, want %q", got, want)
	}
}
