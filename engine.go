package tuohi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/terva-sh/tuohi/dialog"
	"golang.org/x/net/idna"
)

// engine is the platform boundary: everything the shared code needs from a
// native window and its web view. Each platform's webview (lib_unix.go,
// lib_darwin.go, lib_windows.go) satisfies it, and View holds one only
// through this interface, so a method the shared code calls cannot be missing
// or drift on one platform without failing the build there.
//
// Methods declared in the shared files on *webview (Close, Dialog, On, ...)
// are part of the interface too: they are written once, but each platform's
// webview is its own type, so they are listed here like the rest.
type engine interface {
	// core returns the per-view state the shared code owns.
	core() *viewCore

	Navigate(url string)
	Eval(js string)
	Init(js string)
	Dispatch(f func())
	Window() unsafe.Pointer
	Run()
	Terminate()
	Destroy()
	Close()

	Focus()
	Raise()
	Show()
	Hide()
	Maximize()
	Minimize()
	Unminimize()
	Unmaximize()

	Bind(name string, vals ...any) error
	BindBatch(batch []bindRequest) error
	Unbind(name string) error

	installEvents() error
	On(name string, handler func(args ...json.RawMessage)) (cancel func())
	Off(name string)
	Emit(name string, data ...any) error

	Dialog(opts dialog.Options) ([]string, error)

	// handleInternal handles one of the bridge's internal window messages
	// (drag, resize, cursor, app regions, toggle maximize) and reports
	// whether it did. A message it does not handle falls through to the
	// bindings, where no binding has an internal name.
	handleInternal(method string, params json.RawMessage) bool

	// updateBindings applies mutate to the binding table and rebuilds the
	// document-start scripts, on whichever thread and under whichever lock
	// the platform's script rebuild needs. When mutate returns an error, the
	// table is left as mutate left it and no rebuild happens.
	updateBindings(mutate func(bindings map[string]binding) error) error
}

var _ engine = (*webview)(nil)

// viewCore is the per-view state the shared code reads and writes. Each
// platform's webview embeds it, so the fields are declared once.
type viewCore struct {
	mu             sync.Mutex
	bindings       map[string]binding
	userScriptSrcs []string
	events         *events // the view's events bridge (View.On/Off/Emit), installed by App.Show
	calls          serialQueue

	// eventsGlobal names the page-side JS global the events API is installed
	// at (window.<name> with on/off/emit); "events" by default, App.Events
	// overrides it.
	eventsGlobal string

	// onReady fires exactly once, on the UI thread, when the first page load
	// finishes (see View.Ready / fireReady).
	onReady      func()
	onReadyFired bool

	// serve resolves the app's content (App.FS) for requests on the app
	// scheme, or on the loopback server; nil when the app serves no
	// filesystem.
	serve serveFunc

	// contentBase is the loopback-server origin this view's app:// URLs
	// resolve to, or "" when they are served through the platform's native
	// scheme. It is the base of the window's temporary per-view loopback
	// server (App.HTTP, and always on macOS).
	contentBase string
	// transient is that temporary loopback server, nil when the window is
	// scheme-served. releaseLoopback stops it when the window is destroyed,
	// and it also stops itself after loopbackIdleTimeout without a request.
	transient *loopbackServer

	// origins is the set of origins whose pages may use the bridge: every
	// origin the application itself navigated the view to, plus View.Origins.
	// Guarded by mu.
	origins map[string]bool
}

func (c *viewCore) core() *viewCore { return c }

// trustURL adds the origin of a URL the application chose to load to the
// view's trusted origins. Every engine calls it from Navigate with the URL
// it is about to load, after any app:// rewrite, so the trusted origin is
// the one the page really has: the loopback port on macOS and under
// App.HTTP, the app scheme or its https vhost otherwise.
func (c *viewCore) trustURL(raw string) {
	o := originOf(raw)
	if o == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.origins == nil {
		c.origins = map[string]bool{}
	}
	c.origins[o] = true
}

// trusts reports whether a message from a page at senderURL may use the
// bridge. known is false when the engine cannot say where a message came
// from; such a message is allowed, which keeps an engine without a sender
// check working as before until it has one.
func (c *viewCore) trusts(senderURL string, known bool) bool {
	if !known {
		return true
	}
	o := originOf(senderURL)
	if o == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.origins[o]
}

// originOf returns the origin of rawurl in the form the bridge compares, and
// in the form a browser reports it, so a URL given to Navigate matches the
// URI the engine later names as the sender: scheme://host[:port] with the
// scheme in lower case, the host canonicalized as the WHATWG URL standard
// does (see canonicalHost), and the port as a number with the scheme's
// default dropped. An unparsable URL, or one with no scheme, has no origin
// and is never trusted.
//
// A URL with no host has an opaque origin that the URL cannot name:
//   - about: URLs, about:blank above all, have no origin of their own. The
//     document inherits the origin of whoever created it, so any page can
//     make one. They are never trusted.
//   - Any other hostless URL, such as data:, is keyed by the whole URL without
//     its fragment. A data: URL is its own content, so only the exact page Go
//     loaded matches.
func originOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		// Go's parser refuses a percent-escaped host, which a browser
		// decodes before loading: %65xample.com is example.com.
		fixed, ok := unescapeHost(rawurl)
		if !ok {
			return ""
		}
		if u, err = url.Parse(fixed); err != nil {
			return ""
		}
	}
	if u.Scheme == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if u.Host == "" {
		if scheme == "about" {
			return ""
		}
		u.Fragment, u.RawFragment = "", ""
		u.Scheme = scheme
		return u.String()
	}
	host, ok := canonicalHost(u.Hostname())
	if !ok {
		return ""
	}
	port := ""
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return ""
		}
		isDefault := (scheme == "http" && n == 80) || (scheme == "https" && n == 443)
		if !isDefault {
			port = strconv.FormatUint(n, 10)
		}
	}
	if port != "" {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return scheme + "://[" + host + "]"
	}
	return scheme + "://" + host
}

// unescapeHost percent-decodes the host of a scheme://host[:port]/... URL and
// reports whether it changed anything, the way a browser's host parser
// decodes the host before IDNA. The userinfo, port, and path are untouched.
func unescapeHost(rawurl string) (string, bool) {
	i := strings.Index(rawurl, "://")
	if i < 0 {
		return "", false
	}
	rest := rawurl[i+3:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	hostStart := strings.LastIndex(authority, "@") + 1
	hostport := authority[hostStart:]
	host, port := hostport, ""
	if !strings.HasPrefix(hostport, "[") {
		if c := strings.LastIndex(hostport, ":"); c >= 0 {
			host, port = hostport[:c], hostport[c:]
		}
	}
	decoded, err := url.PathUnescape(host)
	if err != nil || decoded == host || strings.ContainsAny(decoded, "/?#@:[]%") {
		return "", false
	}
	return rawurl[:i+3] + authority[:hostStart] + decoded + port + rest[end:], true
}

// canonicalHost returns host as a browser serializes it, following the
// WHATWG URL standard's host parser: an IPv6 address in the standard's
// compressed hex form (see ipv6String), and otherwise the host's IDNA ASCII
// form, lower case. Only after IDNA does it test for IPv4, as browsers do,
// so full-width digits count: a host whose last label is then a number must
// be an IPv4 address in one of the forms a browser accepts (127.1,
// 0x7f.0.0.1, 2130706433) and is written in dotted decimal.
func canonicalHost(host string) (string, bool) {
	if strings.Contains(host, ":") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", false
		}
		return ipv6String(ip), true
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" {
		return "", false
	}
	ascii = strings.ToLower(ascii)
	if endsInNumber(ascii) {
		// A host whose last label is a number must be an IPv4 address; a
		// browser rejects the URL otherwise (256.0.0.1, 1.2.3.4.5).
		return parseWHATWGIPv4(ascii)
	}
	return ascii, true
}

// ipv6String serializes an IPv6 address the way the WHATWG URL standard
// does: eight lower-case hex pieces without leading zeros, the first longest
// run of two or more zero pieces written as "::", and never an embedded
// dotted IPv4 part. That last rule keeps [::ffff:127.0.0.1], which the
// standard writes as [::ffff:7f00:1], a different origin from 127.0.0.1,
// where Go's own String would print it as dotted IPv4.
func ipv6String(ip netip.Addr) string {
	b := ip.As16()
	var pieces [8]uint16
	for i := range pieces {
		pieces[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	start, length := -1, 1
	for i := 0; i < 8; {
		if pieces[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && pieces[j] == 0 {
			j++
		}
		if j-i > length {
			start, length = i, j-i
		}
		i = j
	}
	var sb strings.Builder
	for i := 0; i < 8; i++ {
		if i == start {
			sb.WriteString("::")
			i += length - 1
			continue
		}
		if i > 0 && i != start+length {
			sb.WriteByte(':')
		}
		sb.WriteString(strconv.FormatUint(uint64(pieces[i]), 16))
	}
	return sb.String()
}

// endsInNumber reports whether host's last label, ignoring one trailing dot,
// is all decimal digits or a 0x hex number: the WHATWG test for a host that
// must parse as IPv4.
func endsInNumber(host string) bool {
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	last := labels[len(labels)-1]
	if last == "" {
		return false
	}
	if strings.Trim(last, "0123456789") == "" {
		return true
	}
	if len(last) >= 2 && (last[:2] == "0x" || last[:2] == "0X") {
		return strings.Trim(last[2:], "0123456789abcdefABCDEF") == ""
	}
	return false
}

// parseWHATWGIPv4 parses host by the WHATWG URL standard's IPv4 rules: one to
// four dot-separated parts, each decimal, octal with a leading 0, or hex with
// 0x; the last part fills the remaining bytes. It reports false for a host
// that is not a valid IPv4 address.
func parseWHATWGIPv4(host string) (string, bool) {
	parts := strings.Split(strings.TrimSuffix(host, "."), ".")
	if len(parts) == 0 || len(parts) > 4 {
		return "", false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		base := 10
		switch {
		case len(p) > 1 && (strings.HasPrefix(p, "0x") || strings.HasPrefix(p, "0X")):
			p, base = p[2:], 16
		case len(p) > 1 && p[0] == '0':
			p, base = p[1:], 8
		}
		if p == "" {
			if base == 16 {
				nums[i] = 0 // "0x" alone is zero
				continue
			}
			return "", false
		}
		n, err := strconv.ParseUint(p, base, 64)
		if err != nil {
			return "", false
		}
		nums[i] = n
	}
	var v uint64
	for i, n := range nums[:len(nums)-1] {
		if n > 255 {
			return "", false
		}
		v |= n << (8 * uint(3-i))
	}
	last := nums[len(nums)-1]
	if last >= 1<<(8*uint(5-len(nums))) {
		return "", false
	}
	v |= last
	return fmt.Sprintf("%d.%d.%d.%d", v>>24, (v>>16)&0xff, (v>>8)&0xff, v&0xff), true
}

// bridgeMessage is the envelope window.__webview__ posts for every call.
type bridgeMessage struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// onMessage is where every engine hands over a message the page posted,
// with the URL of the page that sent it when the engine knows it. A message
// from an origin the view does not trust is dropped before anything reads
// it, so bindings, events, and the internal window messages share one gate.
// Otherwise it handles the bridge's internal messages, and runs any other
// method as a call of the binding with that name on the view's serial call
// queue, off the UI thread.
func (w *webview) onMessage(body, senderURL string, senderKnown bool) {
	if !w.trusts(senderURL, senderKnown) {
		return
	}
	var m bridgeMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return
	}
	if m.Method == internalBindError {
		// A live bind or unbind failed on the page. The install Eval is
		// fire-and-forget, so log the failure rather than lose it.
		handleInternalBindError(m.Params)
		return
	}
	if w.handleInternal(m.Method, m.Params) {
		return
	}
	w.mu.Lock()
	b, ok := w.bindings[m.Method]
	w.mu.Unlock()
	if !ok || b.kind != bindingFunc {
		return
	}
	w.calls.do(func() {
		status, result := callAndMarshal(b.fn, m.ID, string(m.Params))
		w.resolve(m.ID, status, result)
	})
}

// resolve delivers a binding call's result to the page, on the UI thread.
func (w *webview) resolve(id string, status int, resultJSON string) {
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)",
		marshalJSON(id), status, marshalJSON(resultJSON))
	w.Dispatch(func() { w.Eval(js) })
}

// BindBatch registers every request of a declarative bind batch and installs
// it. All entries are prepared first, so one bad entry fails the whole batch
// before anything is stored. They then replace any earlier binding of the
// same page name (bindingsReplace: a view entry overrides an app-wide one),
// with one script rebuild and one live install for the whole batch.
func (w *webview) BindBatch(batch []bindRequest) error {
	prepared, live, err := prepareBindBatch(batch)
	if err != nil {
		return err
	}
	_ = w.updateBindings(func(bindings map[string]binding) error {
		for _, p := range prepared {
			bindingsReplace(bindings, p.entries)
		}
		return nil
	})
	w.Eval(liveBindScript(live))
	return nil
}

// errNotBound is Unbind's error for a name with no binding.
var errNotBound = errors.New("name not bound")

// Unbind removes a binding from the view and from the current page.
func (w *webview) Unbind(name string) error {
	err := w.updateBindings(func(bindings map[string]binding) error {
		if _, exists := bindings[name]; !exists {
			return errNotBound
		}
		// An accessor binding lives under three keys: the page name plus its
		// two synthetic dispatch keys. Remove them all.
		for _, n := range []string{name, accessorGetKey(name), accessorSetKey(name)} {
			delete(bindings, n)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Unbinding a name on a frozen namespace throws. liveUnbindJS reports
	// that failure to Go instead of doing nothing.
	w.Eval(liveUnbindJS(name))
	return nil
}
