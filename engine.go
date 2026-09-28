package tuohi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"net/url"
	"sort"
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

	// trust adds the origins of urls to the view's trusted origins (see
	// viewCore.trustURL). When that adds one, it rebuilds the document-start
	// scripts, so the next document on that origin receives the bridge. Each
	// engine's Navigate calls it before starting the load.
	trust(urls ...string)

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

	// token is the view's bridge secret, bridgeTokenLen hex characters, made
	// by the first bridgeScriptLocked. The bridge script keeps it only in a
	// document on a trusted origin, and prefixes it to every message, so a
	// message from any other document or frame lacks it. Guarded by mu.
	token string
}

func (c *viewCore) core() *viewCore { return c }

// trustURL adds the origin of a URL the application chose to load to the
// view's trusted origins. Every engine calls it from Navigate with the URL
// it is about to load, after any app:// rewrite, so the trusted origin is
// the one the page really has: the loopback port on macOS and under
// App.HTTP, the app scheme or its https vhost otherwise.
//
// It reports whether the origin is new, in which case the caller rebuilds the
// document-start scripts (see engine.trust).
func (c *viewCore) trustURL(raw string) bool {
	o := originOf(raw)
	if o == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.origins[o] {
		return false
	}
	if c.origins == nil {
		c.origins = map[string]bool{}
	}
	c.origins[o] = true
	return true
}

// trustURLs trusts every URL in urls and reports whether any origin was new.
func (c *viewCore) trustURLs(urls []string) bool {
	added := false
	for _, u := range urls {
		if c.trustURL(u) {
			added = true
		}
	}
	return added
}

// bridgeTokenLen is the length of a view's bridge token: 32 random bytes in
// hex.
const bridgeTokenLen = 64

// bridgeScriptLocked returns the document-start bridge for this view, with
// its token and its trusted origins (see createInitScript), making the token
// on first use. Each engine's script rebuild puts it first. Assumes mu is
// held.
func (c *viewCore) bridgeScriptLocked(postFn string) string {
	if c.token == "" {
		var b [bridgeTokenLen / 2]byte
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand does not fail on the supported platforms; a view
			// with no token accepts no message.
			panic("tuohi: no randomness for the bridge token: " + err.Error())
		}
		c.token = hex.EncodeToString(b[:])
	}
	origins := make([]string, 0, len(c.origins))
	for o := range c.origins {
		origins = append(origins, o)
	}
	sort.Strings(origins)
	return createInitScript(postFn, c.token, origins)
}

// checkToken strips the view's bridge token from the front of body and
// reports whether it was there. The comparison takes the same time whatever
// the body holds.
func (c *viewCore) checkToken(body string) (string, bool) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token == "" || len(body) < len(token) {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(body[:len(token)]), []byte(token)) != 1 {
		return "", false
	}
	return body[len(token):], true
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
//   - Any other URL with an opaque path, such as data:, is keyed by the whole
//     URL without its fragment, in a canonical form every engine's spelling
//     reduces to (see opaqueKey). A data: URL is its own content, so only
//     the page Go loaded matches, however Go or the engine spelled it.
//
// Like a browser, it first strips leading and trailing C0 controls and
// spaces, and removes every tab and newline: a data: URL built from
// multi-line HTML loads without its newlines.
func originOf(rawurl string) string {
	rawurl = strings.TrimFunc(rawurl, func(r rune) bool { return r <= ' ' })
	rawurl = urlWhitespace.Replace(rawurl)
	if scheme, rest, ok := splitOpaque(rawurl); ok {
		if scheme == "about" {
			return ""
		}
		return opaqueKey(scheme, rest)
	}
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

// urlWhitespace removes the ASCII tabs and newlines a browser drops from
// anywhere in a URL.
var urlWhitespace = strings.NewReplacer("\t", "", "\n", "", "\r", "")

// specialSchemes are the WHATWG URL standard's special schemes, whose URLs
// always have a host and a hierarchical path.
var specialSchemes = map[string]bool{
	"http": true, "https": true, "ws": true, "wss": true, "ftp": true, "file": true,
}

// splitOpaque reports whether rawurl has an opaque path in the WHATWG URL
// standard's sense: a valid scheme that is not special, followed by
// something other than "/". It returns the scheme in lower case and what
// follows its colon.
func splitOpaque(rawurl string) (scheme, rest string, ok bool) {
	i := strings.IndexByte(rawurl, ':')
	if i < 1 {
		return "", "", false
	}
	for j := 0; j < i; j++ {
		c := rawurl[j]
		alpha := ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
		other := ('0' <= c && c <= '9') || c == '+' || c == '-' || c == '.'
		if !alpha && (j == 0 || !other) {
			return "", "", false
		}
	}
	scheme, rest = strings.ToLower(rawurl[:i]), rawurl[i+1:]
	if specialSchemes[scheme] || strings.HasPrefix(rest, "/") {
		return "", "", false
	}
	return scheme, rest, true
}

// opaqueKey returns the key of an opaque-path URL: the scheme, a colon, and
// what follows, without its fragment, in one canonical form (see
// canonicalOpaque). The engines do not spell such a URL alike: WebKitGTK
// percent-encodes the query and keeps the path raw, WebView2 also encodes
// quotes, and NSURL may encode more still. The canonical form makes those
// spellings of one document agree, without making two documents agree. The
// bridge script computes the same key in the page (see initBridgeGate), and
// TestBridgeGate holds the two together.
func opaqueKey(scheme, rest string) string {
	rest, _, _ = strings.Cut(rest, "#")
	return scheme + ":" + canonicalOpaque(scheme, rest)
}

// canonicalOpaque is the canonical form of rest, what follows the colon of an
// opaque-path URL with the given scheme, without its fragment.
//
// Only a data: URL's body is percent-decoded by the fetch standard's data:
// URL processor, so only there are an escape and the byte it names the same
// document. The body, after the first raw comma, is decoded and encoded again
// one way: every byte that is a space, a control, '%', '#', or not ASCII.
// '#' stays escaped, so decoding never starts a fragment. An escaped tab or
// newline is content and stays; only a raw one, which a browser strips before
// parsing, is gone by now (see originOf), and Navigate never passes one on
// (see canonicalNavigateURL).
//
// Everything else is kept as written, escapes included: a data: URL's
// metadata, where %2C is not the comma that ends it, and every other scheme.
// Only its raw spaces, controls and non-ASCII bytes are encoded, which is how
// an engine that encodes them spells it.
func canonicalOpaque(scheme, rest string) string {
	head, body, hasBody := rest, "", false
	if scheme == "data" {
		head, body, hasBody = strings.Cut(rest, ",")
	}
	out := encodeOpaque(head, false)
	if hasBody {
		out += "," + encodeOpaque(body, true)
	}
	return out
}

// encodeOpaque percent-encodes, in upper-case hex, every byte of s that is a
// space, a control, or not ASCII. With decode, it first decodes every valid
// escape, and also encodes '%' and '#': a '%' that starts no valid escape is
// a literal percent sign in a data: body, so it is written %25.
func encodeOpaque(s string, decode bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if decode && c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			c = unhex(s[i+1])<<4 | unhex(s[i+2])
			i += 2
		} else if c == '%' && !decode {
			b.WriteByte(c) // kept as written
			continue
		}
		if c <= ' ' || c >= 0x7F || (decode && (c == '%' || c == '#')) {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// canonicalNavigateURL is the URL every engine's Navigate loads and trusts in
// place of rawurl. For an opaque-path URL, such as a data: URL built from
// multi-line HTML, it percent-encodes the raw tabs and newlines instead of
// leaving them to the engine: WebKitGTK strips them, as the WHATWG URL
// standard says, while WebView2 keeps them as %0A, so the same string would
// load two different documents, and a newline can end a // comment in an
// inline script. The rest is put in canonicalOpaque's form, with the
// fragment kept. Any other URL is returned unchanged.
func canonicalNavigateURL(rawurl string) string {
	trimmed := strings.TrimFunc(rawurl, func(r rune) bool { return r <= ' ' })
	scheme, rest, ok := splitOpaque(trimmed)
	if !ok || scheme == "about" {
		return rawurl
	}
	rest, fragment, hasFragment := strings.Cut(rest, "#")
	out := scheme + ":" + canonicalOpaque(scheme, rest)
	if hasFragment {
		out += "#" + fragment
	}
	return out
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	default:
		return c - 'a' + 10
	}
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

// navAction is what the navigation policy does with a top-level navigation
// the page started.
type navAction int

const (
	// navProceed lets the navigation go ahead in the view.
	navProceed navAction = iota
	// navExternal cancels it in the view and hands the URL to the system,
	// as App.Open does.
	navExternal
	// navCancel cancels it and nothing opens.
	navCancel
)

// navigationPolicy decides a top-level navigation to rawurl, the rule every
// engine's navigation hook applies:
//   - a trusted origin proceeds, which covers every URL Go navigated to;
//   - about:blank proceeds, because it is empty and gets no bridge, and
//     Navigate("") loads it;
//   - a URL App.Open accepts (http, https, mailto) opens in the system;
//   - anything else, such as data:, blob:, file:, or a custom scheme, is
//     cancelled.
//
// A redirect is decided the same way, by the URL it leads to. Frames are
// never passed here: the bridge token keeps them from Go, and cancelling
// them would break embedded content.
func (c *viewCore) navigationPolicy(rawurl string) navAction {
	if c.trusts(rawurl, true) {
		return navProceed
	}
	if u, err := url.Parse(rawurl); err == nil &&
		strings.EqualFold(u.Scheme, "about") && strings.EqualFold(u.Opaque, "blank") {
		return navProceed
	}
	if validateScheme(rawurl) == nil {
		return navExternal
	}
	return navCancel
}

// refuseNavigation carries out a navigation the policy did not let proceed,
// after the engine has cancelled it in the view: it opens the URL in the
// system when action is navExternal, and logs it otherwise, so a developer
// can see why a click did nothing.
func refuseNavigation(rawurl string, action navAction) {
	if action == navExternal {
		openExternal(rawurl)
		return
	}
	log.Printf("tuohi: navigation to %q refused by the navigation policy", rawurl)
}

// handleNewWindow applies the navigation policy to a page's request for a
// new window (target=_blank, window.open), which the engine has already
// refused to open: tuohi never opens a second window. A trusted origin loads
// in this view instead, so the page keeps the bridge and stays under the
// policy. An about:blank window is dropped, because loading it here would
// replace the application's page. Anything else is refused as a navigation.
func (w *webview) handleNewWindow(rawurl string) {
	switch action := w.navigationPolicy(rawurl); {
	case action == navProceed && w.trusts(rawurl, true):
		w.Dispatch(func() { w.Navigate(rawurl) })
	case action == navProceed:
		log.Printf("tuohi: new window for %q dropped: it would replace the view's page", rawurl)
	default:
		refuseNavigation(rawurl, action)
	}
}

// openExternal hands a refused navigation's URL to the system browser or mail
// client, without blocking the UI thread it is called on. Tests replace it,
// on the UI thread before a view exists, so that no browser starts.
var openExternal = func(rawurl string) {
	go func() {
		if err := openURL(rawurl); err != nil {
			log.Printf("tuohi: open %q: %v", rawurl, err)
		}
	}()
}

// bridgeMessage is the envelope window.__webview__ posts for every call.
type bridgeMessage struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// onMessage is where every engine hands over a message the page posted,
// with the URL of the page that sent it when the engine knows it. A message
// that does not start with the view's bridge token, or that comes from an
// origin the view does not trust, is dropped before anything reads it, so
// bindings, events, and the internal window messages share one gate. The
// token covers what the sender URL cannot: WebKitGTK does not say which frame
// posted, and reads the URI when the message arrives, which after a
// navigation names the next page.
// Otherwise it handles the bridge's internal messages, and runs any other
// method as a call of the binding with that name on the view's serial call
// queue, off the UI thread.
func (w *webview) onMessage(body, senderURL string, senderKnown bool) {
	body, ok := w.checkToken(body)
	if !ok || !w.trusts(senderURL, senderKnown) {
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
