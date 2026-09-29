package tuohi

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var resOutsideLinks atomic.Value // string

// outsideLinksScenario checks that a navigation leaving the view's trusted
// origins reaches the system without the view requesting it, and that one to
// a host that does not resolve reaches the system too. A trusted page clicks
// a link to a second loopback server, assigns location.href to it, clicks a
// link to an unresolvable host, and loads a trusted URL that redirects to
// that host. The second server records every request it gets; it must get
// none.
func outsideLinksScenario() string {
	var reqMu sync.Mutex
	var requests []string
	outside, closeOutside, err := serveRecording(func(target, purpose string) {
		reqMu.Lock()
		defer reqMu.Unlock()
		if purpose != "" {
			target += "(" + purpose + ")"
		}
		requests = append(requests, target)
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeOutside()
	// .invalid never resolves (RFC 6761).
	const unresolvable = "http://tuohi-outside-link.invalid/x"
	trusted, closeTrusted, err := servePlain(func(target string) (string, string) {
		if target == "/dead-redirect" {
			return unresolvable + "?redirected", ""
		}
		return "", `<!DOCTYPE html><html><body>
<script>window.addEventListener('load', function(){ window.loaded(location.href); });</script>
</body></html>`
	})
	if err != nil {
		return "listen error: " + err.Error()
	}
	defer closeTrusted()

	external := make(chan string, 8)
	prevOpen := openExternal
	openExternal = func(rawurl string) { external <- rawurl }
	defer func() { openExternal = prevOpen }()

	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	loaded := make(chan string, 8)
	_ = w.w.Bind("loaded", func(href string) { loaded <- href })

	steps := []struct{ name, js, url string }{
		{"link", `var a = document.createElement('a'); a.href = '` + outside + `link'; document.body.appendChild(a); a.click();`, outside + "link"},
		{"assign", `location.href = '` + outside + `assign';`, outside + "assign"},
		// A link inside a shadow root: the click reaches window retargeted
		// to the host.
		{"shadowlink", `var h = document.createElement('div'); document.body.appendChild(h); var r = h.attachShadow({mode: 'open'}); var s = document.createElement('a'); s.href = '` + outside + `shadow'; s.textContent = 'x'; r.appendChild(s); s.click();`, outside + "shadow"},
		{"unresolvable", `var b = document.createElement('a'); b.href = '` + unresolvable + `'; document.body.appendChild(b); b.click();`, unresolvable},
		// A trusted URL that redirects to the unresolvable host: the page
		// sees only the trusted URL, so only the engine can hand it over.
		{"deadredirect", `location.href = '` + trusted + `dead-redirect';`, unresolvable + "?redirected"},
	}

	var mu sync.Mutex
	var report []string
	result := make(chan string, 1)
	time.AfterFunc(60*time.Second, func() { w.Close() })
	go func() {
		defer w.Close()
		select {
		case <-loaded:
		case <-time.After(10 * time.Second):
			result <- "trusted page never loaded"
			return
		}
		for _, st := range steps {
			w.Eval(st.js)
			got := "none"
			select {
			case u := <-external:
				if u == st.url {
					got = "external"
				} else {
					got = "external " + u
				}
			case <-time.After(15 * time.Second):
			}
			mu.Lock()
			report = append(report, st.name+"="+got)
			mu.Unlock()
		}
		// Give a request that raced the hand-off time to arrive.
		time.Sleep(300 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		reqMu.Lock()
		defer reqMu.Unlock()
		got := fmt.Sprintf("%s requested=%d", strings.Join(report, " "), len(requests))
		if len(requests) > 0 {
			got += " " + strings.Join(requests, ",")
		}
		result <- got
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

// TestOutsideLinksNotRequested checks, on every engine, that links and
// scripted navigations to an untrusted origin reach the system without the
// view requesting them, and that a link to a host that does not resolve
// reaches the system too (TKT-01M3MV9PNVWQ1N2ZCAGCM4AJEK).
func TestOutsideLinksNotRequested(t *testing.T) {
	got, _ := resOutsideLinks.Load().(string)
	requireGUI(t, got)
	want := "link=external assign=external shadowlink=external unresolvable=external deadredirect=external requested=0"
	if got != want {
		t.Fatalf("outside links:\n got %s\nwant %s", got, want)
	}
}

// serveRecording answers every request on a fresh loopback port with a small
// page, and calls seen with each request's target and its Sec-Purpose or
// Purpose header, which a speculative request (a prefetch or preconnect)
// carries. It returns the server's base URL, ending in a slash, and a
// function that stops it.
func serveRecording(seen func(target, purpose string)) (string, func(), error) {
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
				target, purpose := "", ""
				if f := strings.Fields(line); len(f) == 3 {
					target = f[1]
				}
				for {
					h, err := br.ReadString('\n')
					if err != nil {
						return
					}
					h = strings.TrimRight(h, "\r\n")
					if h == "" {
						break
					}
					name, value, _ := strings.Cut(h, ":")
					if strings.EqualFold(name, "Sec-Purpose") || strings.EqualFold(name, "Purpose") {
						purpose = strings.TrimSpace(value)
					}
				}
				seen(target, purpose)
				const body = `<!DOCTYPE html><p>outside</p>`
				_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}()
		}
	}()
	return "http://" + ln.Addr().String() + "/", func() { _ = ln.Close() }, nil
}
