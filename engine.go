package tuohi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"unsafe"

	"github.com/terva-sh/tuohi/dialog"
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

// originOf returns the origin of rawurl in the form the bridge compares:
// scheme://host[:port] in lower case, with the scheme's default port
// dropped. A URL with no host, such as about:blank or data:, is its own
// origin: the whole URL without its fragment. An unparsable URL, or one with
// no scheme, has no origin and is never trusted.
func originOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil || u.Scheme == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if u.Host == "" {
		u.Fragment, u.RawFragment = "", ""
		u.Scheme = scheme
		return u.String()
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
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
