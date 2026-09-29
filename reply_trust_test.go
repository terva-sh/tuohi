package tuohi

import (
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resReplyTrust atomic.Value // string

// replyTrustScenario checks that a binding's result and an event reach only a
// trusted document. A trusted page starts a binding call that Go holds open,
// and the view then leaves for about:blank, which is shown but never
// trusted. There the test defines a fake window.__webview__ and listens for
// events, then lets the call return and emits an event, and delivers one
// reply without the guard as a control. The blank page carries what it
// received back to a trusted page in the URL fragment, where calls and
// events must work again.
func replyTrustScenario() string {
	trusted, closeTrusted, err := servePlain(func(target string) (string, string) {
		if strings.HasPrefix(target, "/report") {
			return "", `<!DOCTYPE html><html><body><script>
window.addEventListener('load', function() {
  window.events.on('ping', function(v) { window.report('event=' + v); });
  window.report('blank=' + decodeURIComponent(location.hash.slice(1)));
  window.echo(5).then(function(v) { window.report('call=' + v); });
});
</script></body></html>`
		}
		return "", `<!DOCTYPE html><html><body>
<script>window.addEventListener('load', function(){ window.loaded(location.href); });</script>
</body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeTrusted()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	loaded := make(chan string, 4)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	reports := make(chan string, 8)
	_ = w.w.Bind("loaded", func(href string) { loaded <- href })
	_ = w.w.Bind("slow", func() string {
		entered <- struct{}{}
		<-release
		return "secret-result"
	})
	_ = w.w.Bind("echo", func(n int) int { return n })
	_ = w.w.Bind("report", func(s string) { reports <- s })

	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		fail := func(s string) { result <- s }
		select {
		case <-loaded:
		case <-time.After(10 * time.Second):
			fail("trusted page never loaded")
			return
		}
		w.Eval(`window.slow()`)
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			fail("slow call never arrived")
			return
		}
		w.Eval(`location.href = 'about:blank'`)
		if at := leftForBlank(w, loaded); at != "" {
			close(release)
			fail("blank " + at)
			return
		}
		// The blank page's own bridge and listener. The events API may or
		// may not be installed in about:blank, depending on the engine; the
		// page listens through it if it is and fakes it if not.
		w.Eval(`window.hits = [];
window.__webview__ = { onReply: function(id, status, result) { hits.push(id === 'control' ? 'control' : 'reply ' + result); } };
Object.defineProperty(window.__webview__, '__key', { get: function() { return 'guess'; } });
if (window.events && window.events.on) {
  window.events.on('ping', function(v) { hits.push('event ' + v); });
} else {
  window.events = { _dispatch: function(n, a) { hits.push('event ' + a[0]); } };
}`)
		close(release)
		_ = w.w.Emit("ping", "secret-event")
		// Without the guard, as resolve evaluated it before: the harness sees
		// a delivery when one happens.
		w.Eval(`window.__webview__.onReply("control", 0, "1")`)
		time.Sleep(500 * time.Millisecond)
		w.Eval(`location.href = '` + trusted + `report#' + encodeURIComponent(hits.join(','))`)

		var got []string
		timeout := time.After(15 * time.Second)
		for len(got) < 3 {
			select {
			case r := <-reports:
				got = append(got, r)
				if strings.HasPrefix(r, "blank=") {
					// The trusted page is back; its events must arrive.
					_ = w.w.Emit("ping", "after")
				}
			case <-timeout:
				fail("reports " + strings.Join(got, " "))
				return
			}
		}
		sort.Strings(got)
		result <- strings.Join(got, " ")
	}()

	w.w.Navigate(trusted + "page")
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// TestRepliesOnlyToTrusted checks, on every engine, that a binding's result
// and an event reach only a trusted document, and that calls and events work
// again once the view is back on a trusted page
// (TKT-01M3JYQWZB01CPZ938Y8C24PXX).
func TestRepliesOnlyToTrusted(t *testing.T) {
	got, _ := resReplyTrust.Load().(string)
	requireGUI(t, got)
	want := "blank=control call=5 event=after"
	if got != want {
		t.Fatalf("reply trust:\n got %s\nwant %s", got, want)
	}
}
