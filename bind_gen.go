//go:build !js

package tuohi

// The generated JS: the document-start bridge and the bind/unbind scripts
// (and the marshalling helpers they embed). These strings are the page-side
// half of the binding machinery in bind.go. The file is excluded from
// GOOS=js (js/wasm): it embeds JavaScript as Go strings and is meaningless
// there, like the rest of the engine layer.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// callAndMarshal executes a bound function and marshals the result to JSON.
// Returns the status code (0 for success, -1 for error) and the JSON string.
//
// A panic in a user-supplied binding is recovered and turned into a rejected
// Promise (status -1) instead of crashing the host process and leaving the JS
// caller's Promise pending forever.
func callAndMarshal(fn func(id, req string) (any, error), id, req string) (status int, result string) {
	defer func() {
		r := recover()
		if r != nil {
			status = -1
			result = marshalJSON(fmt.Sprintf("binding panicked: %v", r))
		}
	}()

	resultValue, err := fn(id, req)
	if err != nil {
		return -1, marshalJSON(err.Error())
	}

	data, e := json.Marshal(resultValue)
	if e != nil {
		return -1, marshalJSON(e.Error())
	}
	return 0, string(data)
}

// marshalJSON JSON-encodes a string message for returning to JavaScript.
func marshalJSON(msg string) string {
	data, _ := json.Marshal(msg) // json.Marshal on string never fails
	return string(data)
}

// bridgePostFn (the transport the injected bridge uses to reach Go) is
// platform-specific: WebKit message handlers on macOS/Linux, chrome.webview
// on Windows. It is defined per backend in the lib files (lib_darwin.go /
// lib_unix.go and the Windows backend).

// initBridgeGate, initBridgeHead and initBridgeTail are the parts of the
// document-start bridge template. createInitScript puts the view's token and
// trusted origins in front of initBridgeGate, and splices the caller's post()
// hook between initBridgeHead and initBridgeTail.
//
// The gate runs before anything else in the document, so the page cannot have
// changed location, String.prototype or Object.prototype yet. It computes the
// document's origin key the way originOf does in Go: scheme://host[:port] for
// a URL with a host, which the engine has already canonicalized; for any other
// URL, such as data:, the URL without its fragment in opaqueKey's canonical
// form (percent-escapes decoded, tabs and newlines dropped, spaces, controls,
// '%' and non-ASCII bytes encoded again); and nothing for about:. A document
// that is not the top frame, or whose key is not trusted, gets no bridge at
// all, and never holds the token.
const initBridgeGate = `
  function canonicalOpaque(s) {
    var bytes = [], i, j, c, u;
    for (i = 0; i < s.length; i++) {
      c = s.charCodeAt(i);
      if (c === 37 && /^[0-9A-Fa-f]{2}$/.test(s.substr(i + 1, 2))) {
        bytes.push(parseInt(s.substr(i + 1, 2), 16));
        i += 2;
      } else if (c < 128) {
        bytes.push(c);
      } else {
        u = s.charAt(i);
        if (c >= 0xD800 && c < 0xDC00) { u += s.charAt(++i); }
        try { u = unescape(encodeURIComponent(u)); } catch (e) { u = ''; }
        for (j = 0; j < u.length; j++) { bytes.push(u.charCodeAt(j)); }
      }
    }
    var out = '';
    for (i = 0; i < bytes.length; i++) {
      c = bytes[i];
      if (c === 9 || c === 10 || c === 13) { continue; }
      if (c <= 32 || c >= 127 || c === 37) {
        out += '%' + (c < 16 ? '0' : '') + c.toString(16).toUpperCase();
      } else {
        out += String.fromCharCode(c);
      }
    }
    return out;
  }
  if (window.top !== window) { return; }
  var loc = window.location;
  var key = '';
  if (loc.protocol !== 'about:') {
    key = loc.host ? loc.protocol + '//' + loc.host : canonicalOpaque(loc.href.split('#')[0]);
  }
  if (!key || !Object.prototype.hasOwnProperty.call(trusted, key)) { return; }
`

const initBridgeHead = `
  function generateId() {
    var crypto = window.crypto || window.msCrypto;
    var bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.prototype.slice.call(bytes).map(function(n) {
      var s = n.toString(16);
      return ((s.length % 2) == 1 ? '0' : '') + s;
    }).join('');
  }
  var Webview = (function() {
    var _promises = {};
    // Every object the binding process creates - the namespace containers
    // for dotted names, the callable wrappers and the parsed constant
    // objects - is tracked here, and freezeBinds freezes them all once the
    // batch of a document is complete (the doc-start bind script ends with
    // freezeBinds; a live single-name bind freezes right after its own
    // install). Binding is therefore read-once per page: a bound namespace
    // is sealed the moment its batch finishes.
    var _created = [];
    function _track(o) {
      // Never track the global object itself: the batch only ever freezes
      // objects IT created (namespaces, wrappers, constant trees), so
      // globalThis/window can never end up frozen.
      if (o === window || o === globalThis) { return; }
      if (o !== null && (typeof o === 'object' || typeof o === 'function')) {
        _created.push(o);
      }
    }
    function _trackTree(o) {
      if (o === null || typeof o !== 'object') { return; }
      _track(o);
      for (var k in o) {
        if (Object.prototype.hasOwnProperty.call(o, k)) { _trackTree(o[k]); }
      }
    }
    function Webview_() {}
    Webview_.prototype.post = function(message) {
      return (`

const initBridgeTail = `)(token + message);
    };
    Webview_.prototype.call = function(method) {
      var id = generateId();
      var params = Array.prototype.slice.call(arguments, 1);
      var promise = new Promise(function(resolve, reject) {
        _promises[id] = { resolve: resolve, reject: reject };
      });
      this.post(JSON.stringify({
        id: id,
        method: method,
        params: params
      }));
      return promise;
    };
    Webview_.prototype.onReply = function(id, status, result) {
      var promise = _promises[id];
  // Settle-once: drop the entry so completed calls do not accumulate for
  // the life of the page, and ignore unknown or duplicate replies.
      delete _promises[id];
      if (!promise) {
        return;
      }
      if (result !== undefined) {
        try {
          result = JSON.parse(result);
        } catch (e) {
          promise.reject(new Error("Failed to parse binding result as JSON"));
          return;
        }
      }
      if (status === 0) {
        promise.resolve(result);
      } else {
        promise.reject(result);
      }
    };
    // _holderAt returns the object that owns the LEAF of a dotted name,
    // walking (and creating, and tracking) the intermediate namespace
    // objects as needed.
    function _holderAt(name) {
      var parts = name.split('.');
      var holder = window;
      for (var i = 0; i < parts.length - 1; i++) {
        var seg = parts[i];
        if (typeof holder[seg] !== 'object' || holder[seg] === null) {
          holder[seg] = {};
          _track(holder[seg]);
        }
        holder = holder[seg];
      }
      return { holder: holder, leaf: parts[parts.length - 1] };
    }
    function _settle(promise) {
  // Assignment syntax and fire-and-forget calls drop the promise a Go call
  // returns, so a failing write/call would otherwise surface as an UNHANDLED
  // promise rejection nobody sees. Attach a handler that re-throws on a
  // later macrotask: code that awaits the same promise still sees the
  // rejection (the handler is on the original chain and does not swallow
  // it), and genuine errors hit the console / window.onerror instead of
  // vanishing (RE1).
      if (promise && typeof promise.catch === 'function') {
        promise.catch(function(e) {
          setTimeout(function() { throw e; }, 0);
        });
      }
      return promise;
    }
    // _bindFunction installs the shared callable wrapper for a Go function
    // at a possibly dotted path: every dot walks one level deeper under
    // window, creating intermediate objects as needed, so a name like
    // "app.someAPI.call" is installed as window.app.someAPI.call. A
    // pre-existing window property at the LEAF (e.g. a browser global like
    // window.close) is overwritten - the caller explicitly asked to bind
    // this name, and failing loudly here would abort the whole bind batch
    // and leave the page's other bindings and scripts dead. (Intermediate
    // path segments are reserved for nesting: binding under a path whose
    // parent is itself a bound function replaces it with a namespace
    // object.)
    function _bindFunction(self, name) {
      var at = _holderAt(name);
      var fn = function() {
        var params = [name].concat(Array.prototype.slice.call(arguments));
        return _settle(Webview_.prototype.call.apply(self, params));
      };
      _track(fn);
      at.holder[at.leaf] = fn;
      return fn;
    }
    Webview_.prototype.onBind = function(name) {
  // Bind a ZERO-argument Go function - a callable GETTER: the page calls it
  // (window.name()), and because it needs no arguments it ALSO works as a
  // value: await window.name calls it with no arguments and resolves to
  // its result. The wrapper carries a .then so the read-by-value form is
  // meaningful. Only zero-argument functions get a .then: attaching one to
  // every bound function would make each of them a thenable, so an
  // accidental await window.fn or Promise.resolve(window.fn) would fire a
  // no-argument Go call that any function requiring arguments would reject
  // (E2).
      var self = this;
      var fn = _bindFunction(this, name);
      fn.then = function(onFulfilled, onRejected) {
        return Webview_.prototype.call.call(self, name).then(onFulfilled, onRejected);
      };
    };
    Webview_.prototype.onBindFn = function(name) {
  // Bind any OTHER Go function (two or more arguments, or variadic): it is
  // callable only - no .then and no assignment semantics, so awaiting the
  // name or wrapping it in Promise.resolve() cannot fire a call the function
  // would reject with an arguments mismatch.
      _bindFunction(this, name);
    };
    Webview_.prototype.onBindSetter = function(name) {
  // Bind a one-argument Go function as a CALLABLE SETTER at a possibly
  // dotted path: READING the property yields the callable itself, so
  // window.name(v) calls the Go function with v - and ASSIGNING to it
  // (window.name = v) also calls the function with the assigned value. One
  // name is therefore both a function and a writable variable. Assignment
  // drops the returned promise, so _settle rethrows genuine setter errors
  // on a later task (RE1); awaiting the call form (await window.name(v))
  // still surfaces them normally.
      var at = _holderAt(name);
      var self = this;
      var fn = function() {
        var params = [name].concat(Array.prototype.slice.call(arguments));
        return _settle(Webview_.prototype.call.apply(self, params));
      };
      _track(fn);
      Object.defineProperty(at.holder, at.leaf, {
        configurable: true,
        enumerable: true,
        get: function() { return fn; },
        set: function(value) {
          return _settle(Webview_.prototype.call.call(self, name, value));
        }
      });
    };
    Webview_.prototype.onBindAccessor = function(name, getKey, setKey) {
  // Bind a Go variable at a possibly dotted path as an accessor property:
  // READING the property (window.name / await window.name) calls the Go
  // getter over the bridge and resolves with its result, ASSIGNING to it
  // (window.name = v) calls the Go setter with the assigned value. Either
  // side may be missing - an empty key means the side is not exposed, so a
  // getter-only accessor installs get without set (assignment throws in
  // strict mode) and a setter-only one set without get (reads yield
  // undefined). The getter and setter dispatch through their own registry
  // keys, so the page sees a plain value that stays wired to the Go side.
      var at = _holderAt(name);
      var descriptor = { configurable: true, enumerable: true };
      if (getKey !== '') {
        descriptor.get = (function() {
          var params = [getKey].concat(Array.prototype.slice.call(arguments));
          return _settle(Webview_.prototype.call.apply(this, params));
        }).bind(this);
      }
      if (setKey !== '') {
        descriptor.set = (function() {
          var params = [setKey].concat(Array.prototype.slice.call(arguments));
          return _settle(Webview_.prototype.call.apply(this, params));
        }).bind(this);
      }
      Object.defineProperty(at.holder, at.leaf, descriptor);
    };
    Webview_.prototype.onBindValue = function(name, value) {
  // Bind a Go constant at a possibly dotted path (same namespace rules as
  // onBind): value is the raw JSON of the constant. The parsed value is
  // assigned as ONE thing under its name and every object inside it is
  // tracked, so the whole constant tree freezes with the batch - the page
  // sees a plain, immutable constant, never a way to mutate the Go-side
  // value.
      var at = _holderAt(name);
      var parsed = JSON.parse(value);
      at.holder[at.leaf] = parsed;
      _trackTree(parsed);
    };
    Webview_.prototype.freezeBinds = function() {
  // The binding process of this document is done: freeze every object it
  // created (namespace containers, callable wrappers, constant objects), so
  // the bound namespace is immutable from here on. Defensive: the global
  // object is never frozen even if it somehow ended up tracked.
      for (var i = 0; i < _created.length; i++) {
        if (_created[i] === window || _created[i] === globalThis) { continue; }
        if (!Object.isFrozen(_created[i])) { Object.freeze(_created[i]); }
      }
      _created = [];
    };
    Webview_.prototype.onUnbind = function(name) {
      var parts = name.split('.');
      var holder = window;
      for (var i = 0; i < parts.length - 1; i++) {
        holder = holder[parts[i]];
        if (!holder) {
          return;
        }
      }
      var leaf = parts[parts.length - 1];
      if (!holder.hasOwnProperty(leaf)) {
        throw new Error('Property "' + name + '" does not exist');
      }
      delete holder[leaf];
    };
    return Webview_;
  })();
  window.__webview__ = new Webview();
})()`

// createInitScript returns the document-start bridge. It exposes
// window.__webview__ with Promise-based call()/onReply(), the binding
// installers onBind() (Go functions, awaitable as getters), onBindSetter()
// (one-argument functions, assignable as callable setters), onBindValue()
// (constants) and the accessor installer onBindAccessor(), the batch
// terminator freezeBinds(), and onUnbind() - matching the transport
// the lib backends post into. Every object a bind creates is tracked and
// frozen once the binding batch of the document is complete.
//
// The bridge is installed only in a top-level document whose origin is one of
// origins (see initBridgeGate). post() prefixes token to every message, and
// the Go side drops any message without it (see webview.onMessage).
func createInitScript(postFn, token string, origins []string) string {
	trusted := make(map[string]bool, len(origins))
	for _, o := range origins {
		trusted[o] = true
	}
	trustedJSON, _ := json.Marshal(trusted) // a map of strings to bools always marshals
	return "(function() {\n  'use strict';\n" +
		"  var token = " + marshalJSON(token) + ";\n" +
		"  var trusted = " + string(trustedJSON) + ";\n" +
		initBridgeGate + initBridgeHead + postFn + initBridgeTail
}

// createBindScript returns the document-start script that installs every
// currently-registered binding - constants through onBindValue, getter+setter
// pairs through onBindAccessor, settable functions through onBindSetter,
// zero-argument functions through onBind and any other function through
// onBindFn - and then calls freezeBinds: with every name installed, the
// binding process of this document is complete, so all the objects it created
// (namespace containers, callable wrappers, constant trees) are frozen and
// the bound namespace is immutable from there on. The script runs at the
// start of every navigation, so each fresh document binds and freezes anew.
//
// Entries are emitted in alphabetical name order regardless of how the
// registry map iterates, so the generated script is deterministic and
// golden-testable (P2). The installers themselves guard against a missing
// bridge (a document-start script can run before window.__webview__ exists
// on some backends), which keeps the ordering of the three script families -
// bridge, events/init, bind - from ever breaking a binding (R4).
func createBindScript(entries []binding) string {
	sorted := append([]binding(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	var b strings.Builder
	b.WriteString(`(function() {
  'use strict';
  var w = window.__webview__;
  if (!w) { return; }
`)
	for _, e := range sorted {
		b.WriteString("\tw." + bindInstallCall(e) + ";\n")
	}
	b.WriteString("\tw.freezeBinds();\n})()")
	return b.String()
}

// accessorKeyFor returns the synthetic dispatch key of an accessor side, or
// "" when the side is absent (a getter-only accessor has no setter key, a
// setter-only one no getter key). The JS accessor installer treats an empty
// key as "this side is not exposed".
func accessorKeyFor(side func(id, req string) (any, error), name string, isGet bool) string {
	if side == nil {
		return ""
	}
	if isGet {
		return accessorGetKey(name)
	}
	return accessorSetKey(name)
}

// bindInstallCall renders the installer call for one binding, WITHOUT the
// receiving object - createBindScript prefixes "w.", the live script
// "window.__webview__." - so the doc-start install and the live install can
// never drift apart. The installer is chosen by the binding's kind and
// arity flags: settable functions install through onBindSetter, zero-argument
// functions through onBind (awaitable getters), any other function through
// onBindFn, constants through onBindValue and accessors through
// onBindAccessor (see those installers in createInitScript).
func bindInstallCall(e binding) string {
	switch e.kind {
	case bindingConst:
		return "onBindValue(" + marshalJSON(e.name) + "," + marshalJSON(e.value) + ")"
	case bindingAccessor:
		// A side the binding does not expose (a getter-only accessor has no
		// setter, a setter-only one no getter) installs as an empty key -
		// the JS side omits it.
		return "onBindAccessor(" + marshalJSON(e.name) + "," +
			marshalJSON(accessorKeyFor(e.fn, e.name, true)) + "," +
			marshalJSON(accessorKeyFor(e.set, e.name, false)) + ")"
	default:
		switch {
		case e.settable:
			return "onBindSetter(" + marshalJSON(e.name) + ")"
		case e.gettable:
			return "onBind(" + marshalJSON(e.name) + ")"
		default:
			return "onBindFn(" + marshalJSON(e.name) + ")"
		}
	}
}

// installErrorJS renders the fire-and-forget report a failed live install
// posts to Go (see internalBindError and handleInternalBindError): binding
// installs that throw - a live bind under a namespace an earlier batch
// already sealed, a malformed name, ... - would otherwise be swallowed by
// the engine's fire-and-forget Eval and silently do nothing (R5). err is
// the catch variable name the caller's try/catch bound.
func installErrorJS(err, name string) string {
	return "w.post(JSON.stringify({method:" + marshalJSON(internalBindError) +
		",params:[{name:" + marshalJSON(name) +
		",error:(" + err + "&&" + err + ".message)?" + err + ".message:String(" + err + ")}]}));"
}

// liveBindScript returns the JS that installs a batch of just-registered
// bindings on the CURRENT document (the engines Eval it once per bind batch):
// each install runs in its own try/catch that reports failures to Go
// (installErrorJS), and freezeBinds runs once at the end - a live bind batch
// is its own complete batch, so the objects it creates freeze immediately.
// When the page is not loaded yet (the declarative case: every bind happens
// at window creation, before the first navigation) the __webview__ guard
// makes the whole script a no-op and the document-start bind script performs
// the install plus the batch freeze on the next navigation.
func liveBindScript(entries []binding) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("(function(){var w=window.__webview__;if(!w){return;}")
	for _, e := range entries {
		b.WriteString("try{w." + bindInstallCall(e) + ";}catch(err){")
		b.WriteString(installErrorJS("err", e.name))
		b.WriteString("}")
	}
	b.WriteString("w.freezeBinds();})()")
	return b.String()
}

// liveUnbindJS returns the JS that removes one name from the CURRENT
// document. Unbinding after the namespace froze throws (the delete hits a
// frozen holder); the try/catch reports the failure to Go like a failed
// install, so a live unbind is never a silent no-op (R3/R6).
func liveUnbindJS(name string) string {
	return "(function(){var w=window.__webview__;if(!w){return;}try{w.onUnbind(" +
		marshalJSON(name) + ");}catch(err){" + installErrorJS("err", name) + "}})()"
}
