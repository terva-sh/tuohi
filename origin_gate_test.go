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
// the call must not arrive, and the page must have no bridge at all. An
// engine with the navigation policy refuses that navigation instead and hands
// the URL to the system, which is reported as "untrusted=refused". Go then
// navigates to the same page, which trusts its origin, and the call must
// arrive, although Go names the origin in a spelling WebKit canonicalizes.
func originGateScenario() string {
	refused := make(chan string, 4)
	prevOpen := openExternal
	openExternal = func(rawurl string) { refused <- rawurl }
	defer func() { openExternal = prevOpen }()

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
		first := ""
		select {
		case bridge = <-done:
			// The binding call was posted before the fetch. Give it time to
			// arrive, if the gate lets it through.
			time.Sleep(500 * time.Millisecond)
			first = fmt.Sprintf("untrusted=%d bridge=%s", hits.Load(), bridge)
		case u := <-refused:
			if u != base+"/page" {
				first = "refused " + u
			} else {
				first = "untrusted=refused"
			}
		case <-time.After(10 * time.Second):
			result <- "untrusted page never loaded or refused"
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
		result <- fmt.Sprintf("%s trusted=%d bridge=%s", first, hits.Load()-untrusted, bridge)
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
	// An engine without the navigation policy shows the untrusted page, which
	// must have no bridge; one with it refuses the navigation. Both are safe.
	t.Logf("origin gate: %s", got)
	shown := "untrusted=0 bridge=no trusted=1 bridge=yes"
	refused := "untrusted=refused trusted=1 bridge=yes"
	if got != shown && got != refused {
		t.Fatalf("origin gate = %q, want %q or %q", got, shown, refused)
	}
}

var resFrameGate atomic.Value // string

// frameGateScenario checks that a frame from another origin cannot reach the
// bridge from inside a trusted page. The frame posts a well-formed bridge
// message straight to the engine's message channel, as a hostile frame
// would. WebKitGTK does not say which frame posted and names the top-level
// page as the sender, so only the bridge token stops it there. The frame then
// fetches /ran from its own server, so a frame that never loaded or ran fails
// the scenario instead of passing it.
func frameGateScenario() string {
	var ran atomic.Bool
	// Plain servers, not listenLoopbackHTTP: its COEP and CORP headers would
	// keep the page from loading a frame from another origin at all.
	frameURL, closeFrame, err := servePlainHTML(`<!DOCTYPE html><script>
var m = JSON.stringify({id: 'f1', method: 'hit', params: []});
if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.__webview__) {
  window.webkit.messageHandlers.__webview__.postMessage(m);
}
if (window.chrome && window.chrome.webview) { window.chrome.webview.postMessage(m); }
try { window.top.__webview__.post(m); } catch (e) {}
fetch('/ran');
</script>`, func(target string) {
		if target == "/ran" {
			ran.Store(true)
		}
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeFrame()
	pageURL, closePage, err := servePlainHTML(`<!DOCTYPE html><html><body>
<iframe src="`+frameURL+`frame"></iframe>
<script>
window.addEventListener('load', function(){
  setTimeout(function(){ window.done(); }, 500);
});
</script></body></html>`, nil)
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
		return fmt.Sprintf("frameRan=%v frameHits=%d", ran.Load(), hits.Load())
	default:
		return "no report"
	}
}

func TestFrameGate(t *testing.T) {
	got, _ := resFrameGate.Load().(string)
	requireGUI(t, got)
	if want := "frameRan=true frameHits=0"; got != want {
		t.Fatalf("frame gate = %q, want %q", got, want)
	}
}

// servePlainHTML answers every request on a fresh loopback port with html and
// no headers beyond the content type and length, and returns the server's
// base URL, ending in a slash, and a function that stops it. seen, when not
// nil, is called with each request's target.
func servePlainHTML(html string, seen func(target string)) (string, func(), error) {
	return servePlain(func(target string) (string, string) {
		if seen != nil {
			seen(target)
		}
		return "", html
	})
}

// servePlain is servePlainHTML with a handler per request: it returns a
// Location to redirect to with 302 Found, or "" to answer 200 OK with body.
func servePlain(handle func(target string) (location, body string)) (string, func(), error) {
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
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				target := ""
				if f := strings.Fields(line); len(f) == 3 {
					target = f[1]
				}
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimRight(line, "\r\n") == "" {
						break
					}
				}
				location, body := handle(target)
				if location != "" {
					_, _ = fmt.Fprintf(conn, "HTTP/1.1 302 Found\r\nLocation: %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", location)
					return
				}
				_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}()
		}
	}()
	return "http://" + ln.Addr().String() + "/", func() { _ = ln.Close() }, nil
}
