//go:build linux || freebsd || netbsd || darwin

package tuohi

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The origin gate scenario runs on the engines that name a message's sender.
// Windows joins when its sender check lands.

var resOriginGate atomic.Value // string

// originGateScenario checks that the bridge answers only origins the
// application navigated to. A loopback server serves one page, which calls
// the "hit" binding and then fetches /done. The view first reaches the page
// through a navigation the page itself starts, so its origin is untrusted and
// the call must not arrive. Go then navigates to the same page, which trusts
// its origin, and the call must arrive, although Go names the origin in a
// spelling WebKit canonicalizes.
func originGateScenario() string {
	done := make(chan struct{}, 4)
	serve := func(r *request) *response {
		if strings.HasSuffix(r.URL, "/done") {
			done <- struct{}{}
			return &response{Body: []byte("ok"), MIME: "text/plain"}
		}
		return &response{MIME: "text/html", Body: []byte(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', function(){
  try { window.hit(location.origin); } catch (e) {}
  setTimeout(function(){ fetch('/done'); }, 200);
});
</script></body></html>`)}
	}
	srv, base, err := listenLoopbackHTTP(serve)
	if err != nil {
		return "loopback error: " + err.Error()
	}
	defer func() { _ = srv.Close() }()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	var hits atomic.Int32
	_ = w.w.Bind("hit", func(string) { hits.Add(1) })

	result := make(chan string, 1)
	go func() {
		wait := func() bool {
			select {
			case <-done:
				// The binding call was posted before the fetch. Give it
				// time to arrive, if the gate lets it through.
				time.Sleep(500 * time.Millisecond)
				return true
			case <-time.After(10 * time.Second):
				return false
			}
		}
		if !wait() {
			result <- "untrusted page never loaded"
			w.Close()
			return
		}
		untrusted := hits.Load()
		// Navigate with a spelling the engine canonicalizes (mixed-case
		// host, zero-padded port): the trusted origin must still match the
		// URI WebKit reports for the page.
		loose := strings.Replace(base, "http://localhost:", "http://LocalHost:0", 1)
		w.w.Dispatch(func() { w.w.Navigate(loose + "/page") })
		if !wait() {
			result <- "trusted page never loaded"
			w.Close()
			return
		}
		result <- fmt.Sprintf("untrusted=%d trusted=%d", untrusted, hits.Load()-untrusted)
		w.Close()
	}()

	native(w).loadHTML(`<!DOCTYPE html><script>location.href = "` + base + `/page";</script>`)
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

func TestOriginGate(t *testing.T) {
	got, _ := resOriginGate.Load().(string)
	requireGUI(t, got)
	if got != "untrusted=0 trusted=1" {
		t.Fatalf("origin gate = %q, want %q", got, "untrusted=0 trusted=1")
	}
}
