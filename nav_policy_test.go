package tuohi

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resNavPolicy atomic.Value // string

// navPolicyScenario drives a trusted page through each kind of navigation the
// policy judges, and reports where the view ended up after each one and what
// was handed to the system. The trusted page also holds a frame from the
// untrusted origin, which must still load: the policy judges top-level
// navigations only.
//
// After each step Go asks the page to report its URL through a binding. A
// view still on the trusted page answers; a view that left it does not,
// because no other page gets the bridge.
func navPolicyScenario() string {
	var frameRan atomic.Bool
	untrusted, closeUntrusted, err := servePlain(func(target string) (string, string) {
		switch target {
		case "/frame":
			return "", `<!DOCTYPE html><script>fetch('/ran');</script>`
		case "/ran":
			frameRan.Store(true)
		}
		return "", `<!DOCTYPE html><p>untrusted</p>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeUntrusted()
	trusted, closeTrusted, err := servePlain(func(target string) (string, string) {
		if target == "/redirect" {
			return untrusted + "redirected", ""
		}
		return "", `<!DOCTYPE html><html><body>
<iframe src="` + untrusted + `frame"></iframe>
<script>window.addEventListener('load', function(){ window.loaded(location.href); });</script>
</body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeTrusted()

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

	loaded := make(chan string, 16)
	_ = w.w.Bind("loaded", func(href string) { loaded <- href })

	page := trusted + "page"
	steps := []struct{ name, js string }{
		{"link", `var a = document.createElement('a'); a.href = '` + untrusted + `link'; document.body.appendChild(a); a.click();`},
		{"redirect", `location.href = '` + trusted + `redirect';`},
		// A new window: WebKit's popup blocker drops a window.open that no
		// user gesture started before any policy sees it, so this clicks a
		// target=_blank link, which takes the same new-window path as a
		// window.open the user started.
		{"popup", `var b = document.createElement('a'); b.href = '` + untrusted + `popup'; b.target = '_blank'; document.body.appendChild(b); b.click();`},
		{"data", `location.href = 'data:text/html,<p>data</p>';`},
		{"mailto", `location.href = 'mailto:someone@example.invalid';`},
		// A new window to a trusted origin loads in the view instead.
		{"trustedpopup", `var c = document.createElement('a'); c.href = '` + trusted + `page?popup'; c.target = '_blank'; document.body.appendChild(c); c.click();`},
		{"blank", `location.href = 'about:blank';`},
	}

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		select {
		case href := <-loaded:
			if href != page {
				result <- "first load at " + href
				return
			}
		case <-time.After(10 * time.Second):
			result <- "trusted page never loaded"
			return
		}
		var report []string
		for _, st := range steps {
			w.Eval(st.js)
			time.Sleep(1500 * time.Millisecond)
			for len(loaded) > 0 {
				<-loaded
			}
			w.Eval(`window.loaded(location.href)`)
			where := "left"
			select {
			case href := <-loaded:
				if href == page {
					where = "stay"
				} else {
					where = "at " + href
				}
			case <-time.After(3 * time.Second):
			}
			report = append(report, st.name+"="+where)
		}
		mu.Lock()
		ext := strings.Join(external, " ")
		mu.Unlock()
		out := fmt.Sprintf("%s frameRan=%v external=[%s]", strings.Join(report, " "), frameRan.Load(), ext)
		result <- strings.NewReplacer(untrusted, "U/", trusted, "T/").Replace(out)
	}()

	w.w.Navigate(page)
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// TestNavigationPolicy checks that only trusted origins and about:blank are
// shown in the view, that http(s) and mailto: go to the system, that data:
// is dropped, that a new window to a trusted origin loads in the view, and
// that a cross-origin frame is left alone.
func TestNavigationPolicy(t *testing.T) {
	got, _ := resNavPolicy.Load().(string)
	requireGUI(t, got)
	want := "link=stay redirect=stay popup=stay data=stay mailto=stay trustedpopup=at T/page?popup blank=left frameRan=true " +
		"external=[U/link U/redirected U/popup mailto:someone@example.invalid]"
	if got != want {
		t.Fatalf("navigation policy:\n got %s\nwant %s", got, want)
	}
}
