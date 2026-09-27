package tuohi

// The per-view events bridge (Go <-> JS publish/subscribe): the page side is
// window.<App.Events> with on/off/emit (eventsInitScript), the Go side is
// View.On/Off/Emit over a per-view events host, and a JS-side emit is routed
// to Go through the internal eventsBindName binding.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// --- Events: Go<->JS publish/subscribe on this view ---
//
// Every shown View carries its own events bridge: the page reaches it
// through window.events (on/off/emit by default; App.Events renames
// the global) and Go reaches it through
// On / Off / Emit below. Show installs the bridge on every view at
// creation, so the three methods always work - there is no separate setup
// or handle.

// On subscribes handler to the named event and returns a function that
// cancels just this subscription. The handler receives the event's
// arguments, each as the raw JSON the emitter sent, to unmarshal into
// whatever type it expects. Handlers for a JS-originated event run on the
// binding goroutine; handlers for a Go-originated event run on the
// goroutine that called Emit. Re-enter the UI thread with Dispatch if a
// handler touches the window. Before Show the returned cancel is a no-op.
func (v *View) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	if v.w == nil {
		return func() {}
	}
	return v.w.On(name, handler)
}

// Off removes every Go handler subscribed to the named event. It does not
// affect the page's own JS listeners.
func (v *View) Off(name string) {
	if v.w == nil {
		return
	}
	v.w.Off(name)
}

// Emit publishes an event to every listener on both sides. Each value in
// data becomes one argument delivered to the handlers (Go handlers
// receive it as raw JSON, JS handlers as a decoded value). It is safe to
// call from any goroutine; the JS-side listeners are notified on the UI
// thread. Emit returns an error only if a value in data cannot be
// JSON-encoded, in which case nothing is published.
func (v *View) Emit(name string, data ...any) error {
	if v.w == nil {
		return notShown()
	}
	return v.w.Emit(name, data...)
}

// events is the per-view publish/subscribe state of the events bridge. Every
// shown View carries one: App.Show installs the bridge (installEvents) on
// each view at creation, and the View methods On/Off/Emit delegate to this
// state. It is deliberately unexported - the bridge is part of the View, not
// a standalone facility, so there is no public handle to create one
// independently.
//
// eventsHost is the minimal engine surface the bridge needs: inject the
// document-start script (Init), install the Go binding JS-side emits call
// (Bind), and reach the page's JS listeners on the UI thread (Dispatch +
// Eval). The *webview backends implement it; tests use a recording stub.
type eventsHost interface {
	Init(js string)
	Bind(name string, vals ...any) error
	Dispatch(f func())
	Eval(js string)
}

type events struct {
	w      eventsHost
	global string // page-side JS global the events API is installed at (window.<global>)

	mu     sync.RWMutex
	subs   map[string][]eventSub
	nextID uint64
}

type eventSub struct {
	id      uint64
	handler func(args ...json.RawMessage)
}

// eventsBindName is the Go function the injected JS calls to forward a
// JS-side emit into Go. It must match the name referenced in eventsInitScript.
const eventsBindName = "__appkit_event__"

// installEvents wires the events bridge onto w and returns its per-view
// state: the document-start script (eventsInitScript(global)) and the
// internal Go binding that JS-side emits call. global is the page-side JS
// global the events API is installed at - window.<global> with on/off/emit
// ("events" by default; App.Events overrides it). App.Show calls it
// on every view at creation (via the webview's installEvents method); the
// error is non-nil only if the underlying Bind fails.
func installEvents(w eventsHost, global string) (*events, error) {
	if global == "" {
		global = "events"
	}
	e := &events{
		w:      w,
		global: global,
		subs:   make(map[string][]eventSub),
	}
	w.Init(eventsInitScript(global))
	if err := w.Bind(eventsBindName, e.receiveFromJS); err != nil {
		return nil, fmt.Errorf("appkit: install events bridge: %w", err)
	}
	return e, nil
}

// installEvents stores the view's events bridge on the concrete webview. See
// installEvents.
func (w *webview) installEvents() error {
	if w.events != nil {
		return nil // already installed
	}
	e, err := installEvents(w, w.eventsGlobal)
	if err != nil {
		return err
	}
	w.events = e
	return nil
}

// On subscribes handler to the named event. See the View.On doc.
func (w *webview) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	if w.events == nil {
		return func() {}
	}
	return w.events.On(name, handler)
}

// Off removes every Go handler subscribed to the named event. See View.Off.
func (w *webview) Off(name string) {
	if w.events == nil {
		return
	}
	w.events.Off(name)
}

// Emit publishes an event to every listener on both sides. See View.Emit.
func (w *webview) Emit(name string, data ...any) error {
	if w.events == nil {
		return errors.New("appkit: events bridge is not installed on this view")
	}
	return w.events.Emit(name, data...)
}

// On subscribes handler to the named event and returns a function that cancels
// just this subscription. Handlers for a JS-originated event run on the binding
// goroutine; handlers for a Go-originated event run on the goroutine that called
// Emit. Re-enter the UI thread with Dispatch if a handler touches the window.
func (e *events) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	e.subs[name] = append(e.subs[name], eventSub{id: id, handler: handler})
	e.mu.Unlock()
	return func() { e.remove(name, id) }
}

// Off removes every handler subscribed to the named event.
func (e *events) Off(name string) {
	e.mu.Lock()
	delete(e.subs, name)
	e.mu.Unlock()
}

// Emit publishes an event to every listener on both sides. Each value in data
// becomes one argument delivered to the handlers (Go handlers receive it as raw
// JSON, JS handlers as a decoded value). It is safe to call from any goroutine;
// the JS-side listeners are notified on the UI thread. Emit returns an error
// only if a value in data cannot be JSON-encoded, in which case nothing is
// published.
func (e *events) Emit(name string, data ...any) error {
	raw := make([]json.RawMessage, len(data))
	parts := make([]string, len(data))
	for i := range data {
		b, err := json.Marshal(data[i])
		if err != nil {
			return fmt.Errorf("appkit: encode event %q argument %d: %w", name, i, err)
		}
		raw[i] = b
		parts[i] = string(b)
	}

	// Go-side listeners, synchronously on the caller's goroutine.
	e.dispatch(name, raw)

	// JS-side listeners, on the UI thread. _dispatch only fires local JS
	// listeners, so this does not bounce back to Go.
	payload := "[" + strings.Join(parts, ",") + "]"
	js := "(function(){var g=window." + e.global + ";if(g&&g._dispatch){g._dispatch(" + marshalJSON(name) + "," + payload + ");}})()"
	e.w.Dispatch(func() { e.w.Eval(js) })
	return nil
}

// receiveFromJS is the bound function the page calls when JS emits. It fans the
// event out to the Go handlers only (the JS side already notified its own
// listeners), so there is no echo.
func (e *events) receiveFromJS(name string, args []json.RawMessage) {
	e.dispatch(name, args)
}

// dispatch runs every Go handler for name. Handlers are copied out under the
// lock and called without it, so a handler may subscribe, cancel, or emit
// without deadlocking.
func (e *events) dispatch(name string, args []json.RawMessage) {
	e.mu.RLock()
	subs := e.subs[name]
	handlers := make([]func(args ...json.RawMessage), len(subs))
	for i := range subs {
		handlers[i] = subs[i].handler
	}
	e.mu.RUnlock()

	for _, h := range handlers {
		h(args...)
	}
}

func (e *events) remove(name string, id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	subs := e.subs[name]
	for i := range subs {
		if subs[i].id == id {
			e.subs[name] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(e.subs[name]) == 0 {
		delete(e.subs, name)
	}
}

// eventsInitScript returns the document-start script that installs the
// events bridge on the page: window.<global> with on/off/emit, keeping a
// local listener table and routing a JS-side emit to Go through
// eventsBindName. global is the events-API global name - "events" by
// default, overridable via App.Events. _dispatch is the inbound path
// Go uses to reach JS listeners; it deliberately does not forward back to
// Go.
func eventsInitScript(global string) string {
	const tpl = `(function() {
  'use strict';
  if (window.__API__ && window.__API__._dispatch) { return; }
  var listeners = {};
  function on(name, fn) {
    (listeners[name] = listeners[name] || []).push(fn);
    return function() { off(name, fn); };
  }
  function off(name, fn) {
    if (!listeners[name]) { return; }
    if (!fn) { delete listeners[name]; return; }
    listeners[name] = listeners[name].filter(function(f) { return f !== fn; });
    if (listeners[name].length === 0) { delete listeners[name]; }
  }
  function fire(name, args) {
    var fns = listeners[name];
    if (!fns) { return; }
    fns.slice().forEach(function(fn) {
      try {
        fn.apply(null, args);
      } catch (e) {
        console.error('appkit: event handler for "' + name + '" threw:', e);
      }
    });
  }
  function emit(name) {
    var args = Array.prototype.slice.call(arguments, 1);
    fire(name, args);
    if (typeof window.__appkit_event__ === 'function') {
      var promise = window.__appkit_event__(name, args);
      if (promise && typeof promise.catch === 'function') { promise.catch(function() {}); }
    }
  }
  function _dispatch(name, args) { fire(name, args); }
  window.__API__ = { on: on, off: off, emit: emit, _dispatch: _dispatch };
})()`
	return strings.ReplaceAll(tpl, "__API__", global)
}

// eventsGlobalProvider is the OPTIONAL surface that tells the bind-name
// validation which page global the events API occupies (window.<App.Events>,
// "events" by default), so a top-level binding that would clobber the events
// bridge can be rejected loudly. The *webview backends implement it; test
// stubs fall back to the default name.
type eventsGlobalProvider interface {
	eventsGlobalName() string
}
