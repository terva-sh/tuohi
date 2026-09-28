package tuohi

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resOriginGate atomic.Value // string

// originGateScenario checks that the bridge answers only origins the
// application navigated to. A loopback server serves one page, which calls
// the "hit" binding and then fetches /done. The view first reaches the page
// through a navigation the page itself starts, so its origin is untrusted and
// the call must not arrive, and the page must have no bridge at all. Go then
// navigates to the same page, which trusts its origin, and the call must
// arrive, although Go names the origin in a spelling WebKit canonicalizes.
func originGateScenario() string {
	done := make(chan string, 4)
	serve := func(r *request) *response {
		if i := strings.Index(r.URL, "/done?bridge="); i >= 0 {
			done <- r.URL[i+len("/done?bridge="):]
			return &response{Body: []byte("ok"), MIME: "text/plain"}
		}
		return &response{MIME: "text/html", Body: []byte(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', function(){
  try { window.hit(location.origin); } catch (e) {}
  var bridge = typeof window.__webview__ === 'object' ? 'yes' : 'no';
  setTimeout(function(){ fetch('/done?bridge=' + bridge); }, 200);
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
		var bridge string
		wait := func() bool {
			select {
			case bridge = <-done:
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
		untrusted, untrustedBridge := hits.Load(), bridge
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
		result <- fmt.Sprintf("untrusted=%d bridge=%s trusted=%d bridge=%s",
			untrusted, untrustedBridge, hits.Load()-untrusted, bridge)
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
	want := "untrusted=0 bridge=no trusted=1 bridge=yes"
	if got != want {
		t.Fatalf("origin gate = %q, want %q", got, want)
	}
}

var resFrameGate atomic.Value // string

// frameGateScenario checks that a frame from another origin cannot reach the
// bridge from inside a trusted page. The frame posts a well-formed bridge
// message straight to the engine's message channel, as a hostile frame
// would. WebKitGTK does not say which frame posted and names the top-level
// page as the sender, so only the bridge token stops it there.
func frameGateScenario() string {
	// Plain servers, not listenLoopbackHTTP: its COEP and CORP headers would
	// keep the page from loading a frame from another origin at all.
	frameURL, closeFrame, err := servePlainHTML(`<!DOCTYPE html><script>
var m = JSON.stringify({id: 'f1', method: 'hit', params: []});
if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.__webview__) {
  window.webkit.messageHandlers.__webview__.postMessage(m);
}
if (window.chrome && window.chrome.webview) { window.chrome.webview.postMessage(m); }
try { window.top.__webview__.post(m); } catch (e) {}
</script>`)
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeFrame()
	pageURL, closePage, err := servePlainHTML(`<!DOCTYPE html><html><body>
<iframe src="` + frameURL + `frame"></iframe>
<script>
window.addEventListener('load', function(){
  setTimeout(function(){ window.done(); }, 500);
});
</script></body></html>`)
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closePage()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	var hits atomic.Int32
	done := make(chan struct{}, 1)
	_ = w.w.Bind("hit", func() { hits.Add(1) })
	_ = w.w.Bind("done", func() {
		select {
		case done <- struct{}{}:
		default:
		}
		// A frame message that got through may still be on its way.
		time.Sleep(300 * time.Millisecond)
		w.Close()
	})
	time.AfterFunc(15*time.Second, func() { w.Close() })

	w.w.Navigate(pageURL + "page")
	w.w.Run()
	select {
	case <-done:
		return fmt.Sprintf("frameHits=%d", hits.Load())
	default:
		return "no report"
	}
}

func TestFrameGate(t *testing.T) {
	got, _ := resFrameGate.Load().(string)
	requireGUI(t, got)
	if got != "frameHits=0" {
		t.Fatalf("frame gate = %q, want %q", got, "frameHits=0")
	}
}

// servePlainHTML answers every request on a fresh loopback port with html and
// no headers beyond the content type and length, and returns the server's
// base URL, ending in a slash, and a function that stops it.
func servePlainHTML(html string) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				br := bufio.NewReader(conn)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(line, "\r\n") == "" {
						break
					}
				}
				_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(html), html)
			}()
		}
	}()
	return "http://" + ln.Addr().String() + "/", func() { _ = ln.Close() }, nil
}
