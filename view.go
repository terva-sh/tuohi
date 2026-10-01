package tuohi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/terva-sh/tuohi/dialog"
)

// Custom CSS attribute implemented by tuohi for frameless windows:
//
//	.titlebar { -app-region: drag; }
//	.titlebar-button { -app-region: no-drag; }
//
// The legacy "-webkit-app-region" (Electron) and "-webview-app-region"
// spellings are accepted as aliases so existing markup keeps working
// unchanged. The attribute is NOT a standard CSS property, so browsers drop
// it from the parsed CSSOM; the injected tracker therefore reads the raw
// stylesheet text (inline style attributes, <style> blocks and fetched
// same-origin <link> stylesheets) and derives the drag/no-drag boxes from it.
//
// The tracked boxes flow to the native side as
// {"drag":[{x,y,w,h}...],"noDrag":[...]} in device pixels:
// - Windows : WM_NCHITTEST hit-testing (drag => HTCAPTION; edges of a
// resizable window keep their native resize hit codes and cursors).
// - macOS : mouse-down in a drag box => -[NSWindow performWindowDragWithEvent:].
// - Linux : mouse-down in a drag box => gtk_window_begin_move_drag; edges of
// a resizable window get the proper resize cursor and
// gtk_window_begin_resize_drag.

// Internal script-message methods. They never collide with user Bind names:
// the bind-name validation (validateTopLevel) rejects top-level binding
// names that start with "__tuohi" or equal "__webview__", so these message
// methods stay reachable no matter what a page binds.
const (
	internalAppRegions           = "__tuohiAppRegions"           // page -> native: drag/no-drag boxes (device px)
	internalWindowDrag           = "__tuohiWindowDrag"           // page -> native: start a window move (macOS/Linux)
	internalWindowResize         = "__tuohiWindowResize"         // page -> native: start an edge resize (Linux)
	internalWindowCursor         = "__tuohiWindowCursor"         // page -> native: force the edge/corner resize cursor (macOS)
	internalWindowToggleMaximize = "__tuohiWindowToggleMaximize" // page -> native: double-click on a drag box toggles maximize (all platforms)
	internalBindError            = "__tuohiBindError"            // page -> native: a live bind/unbind install failed ({name, error})
	internalOpenExternal         = "__tuohiOpenExternal"         // page -> native: hand a navigation leaving the trusted origins to the system (Linux, Windows)
	internalPageTitle            = "__tuohiPageTitle"            // page -> native: the trusted page's document.title (all platforms)
)

// appRegion is one draggable (or explicitly non-draggable) box in device
// pixels, in client-area coordinates.
type appRegion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

func (r appRegion) contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// appRegionSet is the full drag-region state of the current page.
type appRegionSet struct {
	Drag   []appRegion `json:"drag"`
	NoDrag []appRegion `json:"noDrag"`
}

func (rs appRegionSet) empty() bool { return len(rs.Drag) == 0 && len(rs.NoDrag) == 0 }

// isDrag answers whether a client-area point (device px) starts a window move.
// "no-drag" always wins, so a button marked no-drag inside a draggable bar
// stays interactive - the semantics of the titlebar example.
func (rs appRegionSet) isDrag(x, y float64) bool {
	for _, r := range rs.NoDrag {
		if r.contains(x, y) {
			return false
		}
	}
	for _, r := range rs.Drag {
		if r.contains(x, y) {
			return true
		}
	}
	return false
}

// decodeParam unmarshals the first element of a script-message params array
// into dst - the page's bridge always sends a call's arguments as [arg] - and
// reports whether dst was filled. An empty or malformed params array leaves
// dst at its zero value.
func decodeParam(params json.RawMessage, dst any) bool {
	var arr []json.RawMessage
	if err := json.Unmarshal(params, &arr); err != nil || len(arr) == 0 {
		return false
	}
	return json.Unmarshal(arr[0], dst) == nil
}

// parseAppRegionSet decodes a params array like
// [{"drag":[...],"noDrag":[...]}] into the region set.
func parseAppRegionSet(params json.RawMessage) appRegionSet {
	var rs appRegionSet
	decodeParam(params, &rs)
	return rs
}

// parseDragRequest decodes the mouse-down details a page sends to start a
// native window move or edge resize.
type dragRequestParams struct {
	Direction string  `json:"direction"` // resize edge: nw/n/ne/e/se/s/sw/w; empty for moves
	Button    int32   `json:"button"`
	ScreenX   int32   `json:"screenX"`
	ScreenY   int32   `json:"screenY"`
	ClientX   float64 `json:"clientX"`
	ClientY   float64 `json:"clientY"`
	Time      uint32  `json:"time"`
}

func parseDragRequest(params json.RawMessage) dragRequestParams {
	var p dragRequestParams
	decodeParam(params, &p)
	return p
}

// GdkWindowEdge (GTK3) / GdkSurfaceEdge (GTK4) - identical values, used by
// gtk_window_begin_resize_drag (GTK3) and gdk_toplevel_begin_resize (GTK4) on
// the Linux backend.
const (
	gdkEdgeNorthWest = 0
	gdkEdgeNorth     = 1
	gdkEdgeNorthEast = 2
	gdkEdgeWest      = 3
	gdkEdgeEast      = 4
	gdkEdgeSouthWest = 5
	gdkEdgeSouth     = 6
	gdkEdgeSouthEast = 7
)

// gdkEdgeFor maps the tracker's direction names onto GdkWindowEdge /
// GdkSurfaceEdge, or -1 for unknown input.
func gdkEdgeFor(direction string) int32 {
	switch direction {
	case "nw":
		return gdkEdgeNorthWest
	case "n":
		return gdkEdgeNorth
	case "ne":
		return gdkEdgeNorthEast
	case "w":
		return gdkEdgeWest
	case "e":
		return gdkEdgeEast
	case "sw":
		return gdkEdgeSouthWest
	case "s":
		return gdkEdgeSouth
	case "se":
		return gdkEdgeSouthEast
	}
	return -1
}

// appRegionScriptTmpl is the document-start tracker template used by
// createAppRegionScript: a JS IIFE whose format verbs are, in order, the
// platform name (as a JSON string), resizable, postRegions and the internal
// message-method names.
const appRegionScriptTmpl = `(function() {
  'use strict';

  var PLATFORM = %s;
  var RESIZABLE = %v;
  var POST_REGIONS = %v;
  var EDGE = 6; // css px band at window edges treated as a resize handle
  var PROP = /(?:-app-region|-webview-app-region|-webkit-app-region)\s*:\s*(drag|no-drag)/gi;

  var elMap = new Map();    // element -> 'drag' | 'no-drag' (last rule wins)
  var dragRects = [];
  var noDragRects = [];
  var lastSig = '';
  var dirty = true;         // styles may have changed -> rescan selectors (initial scan too)
  var fetchedTexts = {};    // stylesheet href -> raw css text (fetched once)
  var fetching = {};        // stylesheet href -> true while a fetch is in flight
  var lastEdge = '';
  var cursorStyle = null;
  var dpr = window.devicePixelRatio || 1;
  var started = false;      // the rAF loop is armed once the bridge is present
  var lastClick = null;     // {x, y, t} of the previous left mouse-down in a drag box

  // The injected bridge (window.__webview__) may not be installed yet at
  // document-start on every backend/WebView2 runtime. Unlike an object shape
  // with a post() but no bridge, or no bridge at all, we must not bail out
  // permanently: instead wait until the bridge appears and then start tracking.
  function bridge() {
    return (window.__webview__ && typeof window.__webview__.post === 'function') ? window.__webview__ : null;
  }

  function propValue(decls) {
    var value = '';
    PROP.lastIndex = 0;
    var m;
    while ((m = PROP.exec(decls)) !== null) { value = m[1]; }
    return value;
  }

  // Split raw css text into {selector, decls} pairs, skipping at-rule
  // wrappers (@media / @supports: the nested rule's selector is what we
  // keep).
  function parseRules(text) {
    var out = [];
    var depth = 0, start = 0, selStart = 0, selEnd = 0;
    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i);
      if (ch === '{') {
        selStart = start; selEnd = i;
        start = i + 1;
        depth++;
      } else if (ch === '}') {
        depth--;
        if (depth === 0) {
          out.push({ selector: text.slice(selStart, selEnd).trim(), decls: text.slice(start, i) });
          start = i + 1;
        }
      }
    }
    return out;
  }

  function applyRule(selectorList, value) {
    var sels = selectorList.split(',');
    for (var i = 0; i < sels.length; i++) {
      var sel = sels[i].trim();
      if (!sel || sel.charAt(0) === '@') { continue; }
      var matches;
      try { matches = document.querySelectorAll(sel); } catch (e) { continue; }
      for (var j = 0; j < matches.length; j++) { elMap.set(matches[j], value); }
    }
  }

  function addStyleSource(text) {
    // Strip comments first: parseRules splits on raw braces and would
    // otherwise merge a trailing /* comment */ into the next rule's selector
    // (making "#titlebar" unqueryable and silently dropping the drag box).
    text = String(text).replace(/\/\*[\s\S]*?\*\//g, '');
    var parsed = parseRules(text);
    for (var i = 0; i < parsed.length; i++) {
      var value = propValue(parsed[i].decls);
      if (value) { applyRule(parsed[i].selector, value); }
    }
  }

  function scanStyles() {
    // elMap is rebuilt from scratch on every scan, so every source must be
    // re-applied SYNCHRONOUSLY within the same scan: fetched stylesheets are
    // cached as raw text and replayed here, never applied from an async
    // callback that a later scan could wipe before collectRects runs.
    elMap = new Map();

    var styleEls = document.querySelectorAll('style');
    for (var i = 0; i < styleEls.length; i++) { addStyleSource(styleEls[i].textContent); }

    // External stylesheets are fetched as raw text - the CSSOM drops the
    // unknown -app-region property, so cssRules cannot be used. A
    // fetch that fails or hangs is retried on later scans (fetching[] is
    // cleared on settle), so a transient early-load failure self-heals.
    var linkEls = document.querySelectorAll('link[rel~="stylesheet"]');
    for (var j = 0; j < linkEls.length; j++) {
      var href = linkEls[j].href;
      if (!href || href.indexOf('file:') === 0) { continue; }
      if (fetchedTexts[href]) {
        addStyleSource(fetchedTexts[href]);
        continue;
      }
      if (fetching[href]) { continue; }
      fetching[href] = true;
      (function(href) {
        fetch(href).then(function(r) { return r.text(); }).then(function(text) {
          fetchedTexts[href] = text;
          delete fetching[href];
          dirty = true;
        }).catch(function() { delete fetching[href]; }); // next scan retries
      })(href);
    }

  // Inline style attributes (highest precedence, applied last).
    var styled = document.querySelectorAll('[style]');
    for (var k = 0; k < styled.length; k++) {
      var v = propValue(styled[k].getAttribute('style') || '');
      if (v) { elMap.set(styled[k], v); }
    }
  }

  function collectRects() {
    var drag = [], noDrag = [];
    var changed = false;
    elMap.forEach(function(value, el) {
      if (!document.documentElement || !document.documentElement.contains(el)) {
        elMap.delete(el);
        changed = true;
        return;
      }
      var rect = el.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0 &&
          rect.right > 0 && rect.left < window.innerWidth &&
          rect.bottom > 0 && rect.top < window.innerHeight) {
        var box = {
          x: rect.left * dpr,
          y: rect.top * dpr,
          w: rect.width * dpr,
          h: rect.height * dpr
        };
        if (value === 'drag') { drag.push(box); } else { noDrag.push(box); }
      }
    });
    dragRects = drag;
    noDragRects = noDrag;
    return changed;
  }

  function postRegions() {
    var sig = JSON.stringify({ drag: dragRects, noDrag: noDragRects });
    if (sig === lastSig) { return; }
    lastSig = sig;
    if (POST_REGIONS) {
      var b = bridge();
      if (b) {
        b.post(JSON.stringify({
          method: %q,
          params: [{ drag: dragRects, noDrag: noDragRects }]
        }));
      }
    }
  }

  function update() {
    if (dirty) { dirty = false; scanStyles(); }
    if (collectRects()) { lastSig = ''; }
    postRegions();
  }

  function inRects(x, y, rects) {
    for (var i = 0; i < rects.length; i++) {
      if (x >= rects[i].x && x < rects[i].x + rects[i].w &&
          y >= rects[i].y && y < rects[i].y + rects[i].h) { return true; }
    }
    return false;
  }

  function edgeAt(x, y) {
    var left = x <= EDGE, right = x >= window.innerWidth - EDGE;
    var top = y <= EDGE, bottom = y >= window.innerHeight - EDGE;
    if (top && left) { return 'nw'; }
    if (top && right) { return 'ne'; }
    if (bottom && left) { return 'sw'; }
    if (bottom && right) { return 'se'; }
    if (top) { return 'n'; }
    if (bottom) { return 's'; }
    if (left) { return 'w'; }
    if (right) { return 'e'; }
    return '';
  }

  function edgeCursor(edge) {
    switch (edge) {
      case 'n': case 's': return 'ns-resize';
      case 'e': case 'w': return 'ew-resize';
      case 'nw': case 'se': return 'nwse-resize';
      case 'ne': case 'sw': return 'nesw-resize';
    }
    return '';
  }

  function setCursor(edge) {
    if (edge === lastEdge) { return; }
    lastEdge = edge;
    if (!cursorStyle && document.head) {
      cursorStyle = document.createElement('style');
      cursorStyle.id = '__tuohi_appregion_cursor';
      document.head.appendChild(cursorStyle);
    }
    if (cursorStyle) {
      cursorStyle.textContent = edge ? ('* { cursor: ' + edgeCursor(edge) + ' !important; }') : '';
    }
    // macOS WKWebView manages its own cursor in the web process and, unlike
    // WebKitGTK / WebView2, will not always honour a dynamic page-wide CSS
    // cursor over live web content (it can revert to the arrow even where
    // * { cursor: … } applies). When an edge/corner is hovered we therefore
    // ALSO send the edge to the engine so it can push AppKit's resize cursor
    // on the UI thread as a native override. Only fires when the edge actually
    // changes (guarded above), so it is cheap.
    if (PLATFORM === 'darwin') {
      post({ method: %q, params: [{ edge: edge }] });
    }
  }

  function post(obj) {
    var b = bridge();
    if (b) { b.post(JSON.stringify(obj)); }
  }

  // Native-side resizability is fixed at creation (View.State); the
  // backends push the initial value here once the tracker exists.
  function installBridgeAPI(b) {
    b.onAppRegionState = function(state) {
      if (state && typeof state.resizable === 'boolean') {
        RESIZABLE = state.resizable;
        if (!RESIZABLE) { setCursor(''); }
      }
    };
  }

  function boot() {
    var b = bridge();
    if (!b) { return; }
    installBridgeAPI(b);
    if (started) { return; }
    started = true;

  // The tracking loop and the initial scan are the critical part and must
  // always come up, so wire them FIRST, before anything that could throw on
  // a document that is still being parsed at document-start (e.g. a null
  // document.documentElement).
    (function loop() {
      update();
      window.requestAnimationFrame(loop);
    })();
  // A slow timer re-scans even if the fast loop's scans ran before the page
  // finished building its DOM/styles (document-start runs early). Forcing
  // dirty here guarantees late-appearing drag boxes are picked up even if
  // the 'load'/'resize' events are missed.
    window.setInterval(function() { dirty = true; }, 300);

    window.addEventListener('resize', function() { dirty = true; }, { passive: true });
    window.addEventListener('load', function() { dirty = true; });

    window.addEventListener('mousedown', function(event) {
      var x = event.clientX, y = event.clientY;

  // Window edge resize. Edges win over drag boxes, like native frames.
  // On Linux and Windows the page reports the mouse-down and native starts
  // the resize (gtk_window_begin_resize_drag / WM_NCLBUTTONDOWN+edge).
      if (RESIZABLE && (PLATFORM === 'linux' || PLATFORM === 'windows')) {
        var edge = edgeAt(x, y);
        if (edge) {
          event.preventDefault();
          event.stopPropagation();
          post({ method: %q, params: [{
            direction: edge,
            button: event.button,
  // GTK3 wants screen/root coordinates in device pixels; GTK4 uses
  // client (surface) coordinates, which are already logical.
            screenX: Math.round(event.screenX * dpr), screenY: Math.round(event.screenY * dpr),
            clientX: x, clientY: y,
            time: event.timeStamp
          }] });
          return;
        }
      }

      if (inRects(x * dpr, y * dpr, noDragRects)) { return; }
      if (!inRects(x * dpr, y * dpr, dragRects)) { return; }

  // Dragging a box swallows the click (title bars do not click through).
      event.preventDefault();
      event.stopPropagation();

  // Double-clicking a drag box toggles maximize, like a native title bar:
  // the SECOND left mouse-down of a pair - same spot within the platform
  // double-click tolerance (<= 400 ms and <= 5 css px) - posts the toggle
  // instead of a drag request. The pending pair is consumed when the toggle
  // fires, so a fast third click starts a fresh pair (a native triple-click
  // toggles once, then the third click drags). A non-left button breaks any
  // pending pair without starting one of its own. Clicks outside drag boxes
  // and on edge/resize or no-drag areas never reach this state (see above).
      if (event.button === 0 && lastClick &&
          Math.abs(x - lastClick.x) <= 5 && Math.abs(y - lastClick.y) <= 5 &&
          event.timeStamp - lastClick.t <= 400) {
        lastClick = null;
        post({ method: %q });
        return;
      }
      if (event.button === 0) {
        lastClick = { x: x, y: y, t: event.timeStamp };
      } else {
        lastClick = null;
      }
      post({ method: %q, params: [{
        button: event.button,
        screenX: Math.round(event.screenX * dpr), screenY: Math.round(event.screenY * dpr),
        clientX: x, clientY: y,
        time: event.timeStamp
      }] });
    }, true);

    if (RESIZABLE && (PLATFORM === 'linux' || PLATFORM === 'windows' || PLATFORM === 'darwin')) {
      window.addEventListener('mousemove', function(event) {
        setCursor(edgeAt(event.clientX, event.clientY));
      }, { passive: true });
      document.addEventListener('mouseout', function(event) {
        if (!event.relatedTarget) { setCursor(''); }
      });
      window.addEventListener('scroll', function() { setCursor(''); }, { passive: true });
    }

  // The MutationObserver is best-effort: at document-start the DOM is not
  // built yet (document.documentElement may be null) and observe() throws,
  // which must not take the tracking loop down with it. The 'load' listener
  // above re-scans once the page exists, so an early throw here only loses
  // incremental style watching until then.
    if (document.documentElement && window.MutationObserver) {
      try {
        new MutationObserver(function() { dirty = true; }).observe(document.documentElement, {
          attributes: true, attributeFilter: ['style', 'class'], childList: true, subtree: true
        });
      } catch (e) {}
    }
  }

  // Document-start scripts can run before the tuohi bridge is in place
  // (this is exactly what happens on the WebView2/Windows backend on the
  // initial document). Retry quickly until the bridge appears so tracking
  // always arms.
  boot();
  var bootTries = 0;
  (function retry() {
    if (!started && bootTries++ < 100) {
      boot();
      setTimeout(retry, 10);
    }
  })();
})()
`

// createAppRegionScript generates the document-start tracker injected into
// borderless windows. resizable is the initial resize state (View.State);
// postRegions is true only where the native side hit-tests with the region
// list (Windows) - macOS/Linux do their hit-testing in JS and only send drag
// requests.
func createAppRegionScript(resizable, postRegions bool, platform string) string {
	return fmt.Sprintf(appRegionScriptTmpl, marshalJSON(platform), resizable, postRegions,
		internalAppRegions, internalWindowCursor, internalWindowResize,
		internalWindowToggleMaximize, internalWindowDrag)
}

// The app's content - the filesystem App.FS - reaches every view through the
// platform's secure-context serving surface: the custom "app" scheme on every
// platform (WebKitGTK and WebView2 treat a registered custom scheme as a
// secure context; macOS serves the scheme through WKWebView's scheme
// handler). The consumer never sees the difference: it navigates to the
// uniform "app://" origin (see App.FS) and tuohi serves the file at that
// path. The types below are the internal request/response contract the
// scheme backends share.

// request carries one incoming request for the app's content. URL is the
// full request URL, e.g. "app://assets/index.html". Method is empty on the
// scheme backends (every scheme request is a GET).
type request struct {
	Method string
	URL    string
}

// response is what a serveFunc returns for a request. A nil response is
// treated as "not found". Body is the response payload (the backend copies or
// streams it before the call returns); MIME defaults to
// "application/octet-stream" when empty.
type response struct {
	Body []byte
	MIME string
}

// serveFunc resolves one request for the app's content to bytes. It runs on
// the UI thread (keep it fast; it reads from an in-memory filesystem).
type serveFunc func(*request) *response

// Show presents a configured View: the first time a View is shown, App.Show
// creates its window and web view (opens/commits the App scope, runs the
// one-time platform initialization) and registers the View with the App,
// which manages it from then on. Calling Show again on the SAME View while
// its window is alive brings it back instead of recreating it: the window is
// un-minimized, shown and focused (Focus(true)). After View.Close the View is
// unregistered and reset, so the same View can be Show'n again later.
//
// The View's exported fields (Debug/FirstMouse, URL, Ready, the
// geometry fields Left/Top/Width/Height/State and the Bind map) are read
// once, exactly at the first Show; keep the *View afterwards - it is the
// handle to the shown window.
//
// The first successful call pins the calling goroutine to its OS thread; keep
// all direct UI calls on that goroutine and re-enter through Window(func)
// from background goroutines. Exception: when the application run loop is
// already running (started by a tray loop or another owner), Show may be
// called from any goroutine - creation and the UI-touching methods marshal
// themselves to the main thread.
//
// A Show of a View whose window another Show is still creating returns an
// error rather than creating a second window.
//
// Every binding is applied while the window is created, deterministically:
// the app-wide App.Bind entries first, then the view's own View.Bind entries,
// each map iterated in alphabetical key order, so the outcome never depends
// on Go's map iteration order. Each entry is ONE name - a function value
// becomes a callable JS function, any other JSON-encodable value a frozen JS
// constant - and once the page's binding batch is installed the whole bound
// namespace is frozen (see makeBinding). A view entry overrides the same app
// name; a nil view entry unbinds it. The events bridge is installed before
// the page loads, so View.On/Off/Emit work immediately.
func (a *App) Show(view *View) error {
	if view == nil {
		return errors.New("tuohi: Show requires a non-nil View")
	}
	if w := view.live(); w != nil {
		// Already shown: reveal the live window (un-minimize, show, focus).
		view.onUIWith(w, func(w engine) {
			w.Unminimize()
			w.Show()
			w.Raise()
			w.Focus()
		})
		return nil
	}
	view.mu.Lock()
	switch {
	case view.w != nil:
		// Another Show finished creating it since the check above.
		w := view.w
		view.mu.Unlock()
		view.onUIWith(w, func(w engine) {
			w.Unminimize()
			w.Show()
			w.Raise()
			w.Focus()
		})
		return nil
	case view.showing:
		view.mu.Unlock()
		return errShowInProgress
	}
	view.showing = true
	view.mu.Unlock()
	defer func() {
		view.mu.Lock()
		view.showing = false
		view.mu.Unlock()
	}()
	return a.showFirst(view)
}

// errShowInProgress is what Show returns for a View whose window another
// Show is still creating, which may yet fail.
var errShowInProgress = errors.New("tuohi: Show: the View's window is still being created by another Show")

// showFirst performs the one-time creation of a View's window and registers
// it with the App (see App.Show).
func (a *App) showFirst(view *View) error {
	// Before the scope opens, so that a refused call does no platform
	// initialization on the wrong thread.
	if err := uiThreadErr(); err != nil {
		return err
	}
	s, err := a.begin()
	if err != nil {
		return err
	}
	cfg := s.cfg
	// Resolve the effective per-window settings: the view's own View.Debug
	// ORs over the app-wide App.Debug (which already includes TUOHI_DEBUG=1
	// when App.AllowEnvDebug is set), so the engine reads one value.
	view.Debug = view.Debug || cfg.Debug
	// The first window creation triggers the one-time application
	// initialization (the icon); later views are no-ops.
	a.start(s)
	// The engine stays private to showFirst until it is fully set up: only
	// then is it published on the View, so a Close from another goroutine
	// cannot tear it down while it is still being configured. A Close that
	// arrives before that finds the View unshown and does nothing.
	nw, err := newView(view, serveAppFS(cfg.FS))
	if err != nil {
		return err
	}
	var w engine = nw
	w.trust(view.Origins...)
	fail := func(err error) error {
		w.Close()
		return err
	}
	// Name the global the events API is installed at: window.<name> with
	// on/off/emit - the app-wide App.Events name, default "events".
	w.core().eventsGlobal = cfg.Events
	// Every view carries the events bridge (View.On/Off/Emit): Show installs
	// it - the document-start script plus the internal Go binding - on the
	// view at creation, so the bridge is live before the page loads. The only
	// possible failure is an internal bind conflict on a freshly created view,
	// which cannot happen in practice.
	if err := w.installEvents(); err != nil {
		return fail(err)
	}
	// Declarative bindings: the app-wide App.Bind map first, then the view's
	// own View.Bind map, each in alphabetical key order; a per-view name
	// overrides the app-wide one, a nil per-view entry unbinds it (see
	// applyBinds). The view's map is snapshotted at this first read:
	// the consumer may keep mutating View.Bind after Show, and binding only
	// ever sees this copy.
	if err := applyBinds(w, cfg.Bind, cloneBindMap(view.Bind)); err != nil {
		return fail(err)
	}
	if view.window == nil {
		// Owned windows are counted so Wait can return when the last one
		// closes (when App.Exit is set). The engines call appWindowClosed
		// when an owned window is destroyed.
		atomic.AddInt32(&s.windows, 1)
	}
	// Wire the Ready callback into the engine: it fires exactly once, on
	// the UI thread, when the first page load after Show finishes (see the
	// View.Ready doc).
	w.core().onReady = view.Ready
	// The title sources: the view's own Title, and, for a window tuohi
	// created, App.Name until the page gives one (see applyTitle).
	w.core().titleGo = view.Title
	w.core().titleDefault = cfg.Name
	w.core().hostWindow = view.window != nil
	w.core().mu.Lock()
	w.core().permissions = permissionSet(view.Permissions)
	w.core().mu.Unlock()
	a.registerView(view, w)
	// Publish the engine, so the View's methods delegate to the window (and
	// can be called right after Show).
	view.mu.Lock()
	view.w, view.app = w, a
	view.mu.Unlock()
	view.onUIWith(w, applyTitle)
	// Load the window's first page: the declarative URL. With an empty URL
	// no navigation happens (a blank window) and Ready stays pending until
	// a later Navigate completes.
	if view.URL != "" {
		w.Navigate(view.URL)
	}
	return nil
}

// View describes one window and its embedded web view. It is tuohi's
// define-first window object, the same pattern App uses for the application:
// the exported fields are the configuration of a window that does not exist
// yet, and App.Show(view) turns it into a live window. Configure a View, hand
// it to App.Show, and keep the pointer - after Show the same View is the
// handle to its window (Navigate, On/Off/Emit, Show/Hide, ...), so there is no
// separate window object to track.
//
// Show reads the fields exactly once, when the window is created; later edits
// have no effect on the running window (matching how App commits its own
// settings). Re-showing the same View while its window is alive brings the
// window back (see App.Show); after View.Close the fields are read again, so
// the View can be reconfigured and shown anew. Before
// Show the imperative methods below have no engine behind them and either
// return a clear error/zero value or panic with "View is not shown".
type View struct {
	// Debug turns the platform web inspector / developer tools on for this
	// window. App.Show ORs it with the app-wide App.Debug - a true on either
	// side opens the tools, and so does the TUOHI_DEBUG=1 environment
	// variable when the App sets AllowEnvDebug.
	//
	// Backend mapping - WebView2 AreDevToolsEnabled (Windows), WebKitGTK
	// enable-developer-extras (Linux), WKPreferences developerExtrasEnabled
	// (macOS).
	Debug bool

	// FirstMouse makes a click on an INACTIVE window reach the page instead
	// of only bringing the window forward.
	//
	// macOS only; ignored elsewhere, where a click on an inactive window
	// already reaches the content. AppKit's default is the opposite of what
	// most web UIs want: the first click is swallowed as activation, so a
	// user who clicks a button in a window that lost focus has to click twice
	// - and the first click looks broken. Turn this on for control panels,
	// dashboards, players and anything else the user clicks in passing.
	//
	// It is OPT-IN because the default protects destructive interfaces: in a
	// drawing tool, an editor, or any window with a delete button, a click
	// that merely raises the window must NOT also press what happens to be
	// under the cursor. Leave it off when a stray first click could destroy
	// something.
	FirstMouse bool

	// URL is the page the window loads first. App.Show navigates the view
	// here once the window exists (Navigate). The URL may be the loopback
	// address of a server the program runs itself, such as
	// http://127.0.0.1:8080/, a uniform "app://" URL served by App.FS, an
	// https:// URL, or a data: URI. Empty (the default) starts a blank window
	// and no navigation happens - and because Ready fires on a completed
	// load, it will not fire until a later Navigate completes.
	URL string

	// Origins lists origins, besides the ones the view is navigated to from
	// Go, whose pages may call the view's bindings and use its events, such
	// as "https://auth.example.com". Every URL given to Navigate, including
	// URL, is trusted already. A page on any other origin, reached through a
	// link, a redirect, or a frame, cannot reach Go. An about:blank page is
	// never trusted, because any page can create one.
	//
	// These are also the only pages the view shows. When a page leads the
	// whole view elsewhere, by a link, a redirect, a script, or a new window,
	// the view stays where it was and an http, https, or mailto URL opens in
	// the system browser or mail client instead; data:, blob:, file:, and
	// other schemes are dropped. about:blank is shown, and frames are left
	// alone. A new window (target=_blank, window.open) never opens: one to a
	// trusted origin loads in this view instead, one to about:blank is
	// dropped, and any other follows the rule above. So a URL that
	// redirects to a sign-in page on another origin
	// needs that origin listed here, or the view stays blank while the
	// browser opens it. A redirect to another scheme or host, such as http
	// to https, or 127.0.0.1 to localhost, counts as another origin.
	Origins []string

	// Permissions lists what a page in this view may use that the web engine
	// would otherwise ask the user for: the camera, the microphone, and
	// script reads of the clipboard (see Permission). Only a page on an
	// origin the view trusts (see Origins) receives one, and only if it is
	// listed here. Everything else a page asks for, such as geolocation or
	// notifications, is denied, and no engine shows a prompt of its own.
	// Empty, the default, denies everything. Pointer lock is not a
	// permission: every engine allows it on a user gesture.
	//
	// A frame on another origin can ask only when the trusted page delegates
	// the feature to it with the iframe's allow attribute. WebView2 and
	// WKWebView then decide by the frame's own origin and deny it. WebKitGTK
	// does not say which frame asks, so on Linux such a frame asks as the
	// trusted page: delegating a listed permission to a frame grants it
	// there. Delegate only to frames you would trust with the permission.
	//
	// On macOS, a program that lists the camera or the microphone also needs
	// NSCameraUsageDescription or NSMicrophoneUsageDescription in its
	// Info.plist.
	Permissions []Permission

	// Title is the window's title. It names the window in its title bar,
	// the taskbar or Dock, the window switcher, and accessibility tools, so
	// it matters for a frameless window too. SetTitle changes it after Show.
	//
	// When Title is empty the window follows the page instead: it takes the
	// document.title of the page it shows, and App.Name while that page has
	// none. Only a page on a trusted origin (see Origins) can set it, so a
	// page the view does not trust, such as about:blank, leaves the title
	// as it was. A window embedded from the host's own takes only a
	// non-empty Title; the page and App.Name leave it alone.
	Title string

	// Ready, when non-nil, is called exactly once, on the UI thread, the
	// first time a page finishes loading after Show (the initial Navigate to URL
	// or the first later navigation). It is the "the window is fully up"
	// callback: bindings and the events bridge are live by then.
	//
	// Pair it with Eval to run JavaScript on init: Ready fires only after the
	// first page load completed, so the DOM and the page's own scripts are in
	// place and a single Eval reaches them reliably. Injecting script before
	// the document exists is not reliable across the three engines, so tuohi
	// has no declarative JS/CSS injection API - call Eval from Ready to run
	// code when the page comes up.
	Ready func()

	// window is the OPTIONAL native host window to embed into, set before
	// Show (a GtkWindow* / NSWindow* / HWND); nil lets tuohi create and own
	// the window. It is unexported: embedding is an internal capability. The
	// live native handle is reachable through View.Window(func) - see that
	// method.
	window unsafe.Pointer

	// Bind holds this view's declarative bindings: every entry is bound onto
	// the web view when App.Show runs. A key is a DOTTED path - dots
	// separate nested variables on the page, so a value bound at
	// "app.someAPI.call" appears as window.app.someAPI.call. What a value
	// becomes is decided by its kind alone:
	//
	//   - a function becomes a JS function the page calls. Its arity decides
	//     whether it ALSO works as a variable: a zero-argument function is a
	//     callable GETTER - call it (`window.name()`), or read it as a value
	//     (`await window.name`, which calls it with no arguments); a
	//     one-argument function is a callable SETTER - call it
	//     (`window.name(v)`), or ASSIGN to it (`window.name = v`, which runs
	//     it with the assigned value; the assignment expression yields that
	//     value, so await the CALL form for the result);
	//   - a length-2 array of two functions ([2]any{getter, setter}) becomes
	//     a readable AND writable property: reading it runs the getter over
	//     the bridge (`const v = await window.name`), assigning to it runs
	//     the setter (`window.name = v`);
	//   - any other value - a bool, a number, a string, or any JSON-encodable
	//     value such as a struct, map or slice - becomes an immutable JS
	//     constant bound wholesale under that name.
	//
	// The page's calls are dispatched to Go in the order it makes them, so a
	// read issued after a write observes the write
	// (`window.count = 1; await window.count`).
	//
	// No part of a Go type is ever bound separately: structs and maps are
	// never walked. The namespace the bindings of a page are installed into
	// is frozen once the batch finishes, so the page cannot mutate the
	// functions, constants or accessor objects it was given.
	//
	// A name set here overrides the same name in the app-wide App.Bind map;
	// a nil entry under a name UNBINDS that name again, removing an app-wide
	// binding this view does not want. Entries are applied in alphabetical
	// key order, after the app-wide entries (see App.Show). A nil entry in
	// App.Bind itself binds nothing.
	Bind map[string]any

	// Frame creates the window with the OS frame - the title bar and system
	// buttons - and an opaque background. The default (false) is a frameless
	// window: NO OS decoration of any kind and a fully transparent background,
	// so the desktop shows through everywhere the page does not paint. The
	// page is then responsible for the window chrome and marks the movable
	// pieces with the "-app-region" CSS attribute ("drag" / "no-drag");
	// see the package documentation. Resizing still works from the window
	// edges unless State is StateFixed.
	//
	// Only meaningful for windows tuohi owns; an embedded window keeps its
	// host's frame and background.
	Frame bool

	// Left/Top optionally place the window on the screen, in pixels. They are
	// best effort, because not every window system lets a client pick its
	// position: supported on Windows and on the GTK3/X11 stack; ignored on
	// GTK4 and on Wayland (the compositor places windows). macOS treats the
	// coordinates as AppKit screen coordinates (origin at the bottom-left).
	// Both zero mean "let the platform decide".
	Left, Top int

	// Width/Height set the initial window size in pixels. Both zero mean the
	// backend default (640x480), matching the behavior of a window that was
	// never sized. With State StateMin/StateMax they are the respective
	// minimum/maximum bounds instead.
	Width, Height int

	// State is the initial resize state (StateNone/StateMin/StateMax/
	// StateFixed): StateFixed makes the window non-resizable (and disables the
	// frameless edge resize), StateMin/StateMax turn Width/Height into bounds.
	//
	// Every geometry field above is applied once, when the window is created -
	// see the note on the View type about window control.
	State State

	// mu guards w, app and showing, which App.Show sets and Close clears
	// while other goroutines call the View's methods.
	mu sync.Mutex

	// showing is true while App.Show creates the View's window, so a second
	// Show of the same View in that time does not create another.
	showing bool

	// w is the live engine handle. App.Show stores it here (nil before the
	// first show and after Close, so the View can be shown again); methods
	// fail with "View is not shown" while it is nil.
	w engine

	// app is the App currently managing this View (set by App.Show, cleared
	// by Close). It is used to unregister the View on Close.
	app *App
}

// live returns the View's engine handle, nil when the View is not shown.
func (v *View) live() engine {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.w
}

// onUI runs f on the UI thread with the View's engine (see uiDispatcher.run).
// A call queued from another goroutine runs later, so when it runs it first
// checks that the View still holds the engine it was queued for: a Close in
// between has destroyed that engine, and f is dropped.
func (v *View) onUI(f func(w engine)) {
	v.onUIWith(v.mustEngine(), f)
}

// onUIWith is onUI for an engine the caller already holds.
func (v *View) onUIWith(w engine, f func(w engine)) {
	ui.run(func() {
		if v.live() == w {
			f(w)
		}
	})
}

// State values configure window sizing and resizing at creation time.
type State int

const (
	// StateNone lets the window size freely; zero Width/Height pick the
	// backend default.
	StateNone State = iota

	// StateMin makes Width and Height the minimum bounds.
	StateMin

	// StateMax makes Width and Height the maximum bounds.
	StateMax

	// StateFixed prevents the user from resizing the window.
	StateFixed
)

// The methods below drive the shown window and its embedded web view; each
// delegates to the live engine handle App.Show stored on the View. Each is
// safe to call from any goroutine, including a binding's: on the UI thread
// (the goroutine that created the first window) it runs in place, and
// elsewhere it is queued to the UI thread and returns without waiting, unless
// its own documentation says it waits. Calls from one goroutine run in the
// order they were made. Calling any of them before Show is a
// caller error: methods that can report failure return an error ("View is not
// shown"), value-returning methods return their zero value, and void methods
// panic with the same message instead of dereferencing a nil engine.
//
// Bind has no method form: bindings are declarative (the View.Bind map, plus
// the app-wide App.Bind map), applied once by App.Show.
//
// Note on window control: geometry (size and position) is deliberately NOT
// part of the runtime surface. It is a defined non-feature of tuohi - the
// platforms this library targets cannot agree on resizing or moving an
// existing window at runtime (Wayland compositors do not allow a client to
// move its own toplevel, and GTK4 has no move API at all), so a unified
// runtime setter would silently not work on part of the target matrix. Size
// and position therefore live in the View's geometry fields (Left/Top/
// Width/Height/State), applied once when the window is created. If you need to
// resize or move the window afterwards, read the
// View.Window handle (filled by Show) and do it with that platform's own
// API.

// mustEngine returns the live engine handle of a shown View, panicking with
// a clear message when the View was never passed to App.Show.
func (v *View) mustEngine() engine {
	w := v.live()
	if w == nil {
		panic("tuohi: View is not shown: pass it to App.Show first")
	}
	return w
}

// notShown is the error value-returning View methods report before App.Show.
func notShown() error {
	return errors.New("tuohi: View is not shown: pass it to App.Show first")
}

// Run is not part of the public View API - the application run loop is owned
// by App.Wait; engines keep an internal Run for tests and embedding hosts.

// Navigate loads the given URL in the view. The URL may be an "app://" URL
// served by App.FS, an https:// URL, a properly encoded data URI, or any
// other URL the platform engine accepts. An "app://" URL with no host loads
// from the "app" host: "app://#route" is "app://app/#route". Examples:
//
//	v.Navigate("https://github.com/terva-sh/tuohi")
//	v.Navigate("app://app/index.html")
//	v.Navigate("data:text/html,%3Ch1%3EHello%3C%2Fh1%3E")
func (v *View) Navigate(url string) {
	v.onUI(func(w engine) { w.Navigate(url) })
}

// Window marshals f to the UI thread and calls it with the view's native
// window handle (a GtkWindow* / NSWindow* / HWND). Use it instead of touching
// the platform from a background goroutine: it is the re-enter-the-UI-thread
// entry point (the former Dispatch) with the handle handed to you. f runs on
// the UI thread; keep it short. Before Show it panics with "View is not
// shown".
func (v *View) Window(f func(wnd unsafe.Pointer)) {
	w := v.mustEngine()
	w.Dispatch(func() { f(w.Window()) })
}

// Close tears the view down for good: it terminates the view's run loop (if
// one is running) and destroys the native window and web view, then
// UNREGISTERS the View from the App that showed it and resets its internal
// state (engine handle, App reference) so the same View can be App.Show'n
// again later. It is idempotent and safe to call from any goroutine,
// concurrently too: exactly one call tears the window down. It is a no-op
// before the View was ever shown - closing an unshown View is not an error.
// Off the UI thread the teardown is queued and Close does not wait for it.
func (v *View) Close() {
	v.mu.Lock()
	w, app := v.w, v.app
	v.w, v.app = nil, nil
	v.mu.Unlock()
	if w == nil {
		return
	}
	w.Close()
	if app != nil {
		app.unregisterView(v, w)
	}
}

// Eval evaluates arbitrary JavaScript asynchronously; the result is
// ignored. It is the supported way to run code on init: call it from the
// View.Ready callback, once the first page load completed, so the DOM and
// the page's own scripts are in place. Injecting script before the document
// exists is not reliable across the three engines, so tuohi exposes no
// declarative JS/CSS injection API.
func (v *View) Eval(js string) {
	v.onUI(func(w engine) { w.Eval(js) })
}

// Focus moves keyboard focus into the web content - so typing, and a screen
// reader's cursor, land inside the page - and, when raise is true, FIRST
// brings the window to the front and gives the application focus (the case a
// program that took focus away from itself needs: it launched a window that
// activates, finished a job that raised something else). Use Focus(true)
// sparingly - stealing focus from someone typing in another application is
// worse than the extra click it saves.
func (v *View) Focus(raise bool) {
	v.onUI(func(w engine) {
		if raise {
			w.Raise()
		}
		w.Focus()
	})
}

// Show makes the window visible again and brings it to the front, putting
// it back into the taskbar / window list after Hide, or restoring it after
// Minimize. Safe to call from any goroutine (the backends marshal to the
// UI thread).
func (v *View) Show() {
	v.onUI(engine.Show)
}

// Hide removes the window from the screen AND from the taskbar / window
// list - the classic "hide to tray" behavior: the process keeps running
// and the window stays alive until Show brings it back. Safe to call from
// any goroutine.
func (v *View) Hide() {
	v.onUI(engine.Hide)
}

// Maximize enlarges the window to fill the available screen area. On macOS
// it performs the native zoom, which is a TOGGLE: calling Maximize on an
// already-zoomed window restores its previous size. Safe to call from any
// goroutine.
func (v *View) Maximize() {
	v.onUI(engine.Maximize)
}

// Minimize shrinks the window to the taskbar / Dock (on macOS it is
// miniaturized into the Dock). Show restores it. Safe to call from any
// goroutine.
func (v *View) Minimize() {
	v.onUI(engine.Minimize)
}

// Unminimize restores a minimized window to its normal on-screen state
// (the inverse of Minimize). It is a no-op when the window is not
// minimized. Safe to call from any goroutine.
func (v *View) Unminimize() {
	v.onUI(engine.Unminimize)
}

// SetTitle sets the window's title, as View.Title does at Show. An empty
// title hands the window back to the page: it follows the page's
// document.title again, or App.Name while the page has none. Safe to call
// from any goroutine.
func (v *View) SetTitle(title string) {
	v.onUI(func(w engine) {
		w.core().titleGo = title
		applyTitle(w)
	})
}

// Unmaximize restores a maximized window to its previous normal size (the
// inverse of Maximize). It is a no-op when the window is not maximized.
// Safe to call from any goroutine.
func (v *View) Unmaximize() {
	v.onUI(engine.Unmaximize)
}

// Unbind is not part of the public View API - bindings are fixed at Show time
// by the App.Bind / View.Bind maps. Engines keep an internal Unbind used by
// tests to verify binding teardown.

// --- Native file dialogs (a tuohi extension) ---

// Dialog presents a native, application-modal file panel chosen by
// opts.Type (open, multi-open, save or directory) and built on the
// github.com/terva-sh/tuohi/dialog package. Unlike the other View
// methods it BLOCKS the calling goroutine until the user dismisses the
// dialog. Call it from a Bind callback - which runs on a background
// goroutine - or any other goroutine; on the UI thread it runs the panel in
// place. Off the UI thread it needs a running loop (App.Wait): when none is
// running, or the loop stops before the panel opens, it returns an error and
// no panel is shown.
//
// A cancelled dialog - and a dialog that could not be presented (no
// backend, no display) - returns an empty result and a nil error, per the
// dialog package contract.
func (v *View) Dialog(opts dialog.Options) ([]string, error) {
	w := v.live()
	if w == nil {
		return nil, notShown()
	}
	return w.Dialog(opts)
}

// Dialog presents the native panel selected by opts.Type and returns the
// chosen path(s), or nil if the user cancelled.
func (w *webview) Dialog(opts dialog.Options) ([]string, error) {
	var paths []string
	var err error
	if callErr := ui.call(func() { paths, err = dialog.Open(opts) }); callErr != nil {
		return nil, fmt.Errorf("tuohi: dialog: %w", callErr)
	}
	return paths, err
}

// schemeMIME returns a response's MIME type or the octet-stream default.
func schemeMIME(r *response) string {
	if r.MIME != "" {
		return r.MIME
	}
	return "application/octet-stream"
}

// appSchemeName is the custom scheme tuohi registers to serve the app's
// filesystem - the app-scope content origin consumers navigate to. The page
// is loaded from e.g. "app://assets/index.html"; the scheme is registered as
// a secure context (WebKitGTK / WKWebView) or served through a secure https
// vhost (WebView2).
const appSchemeName = "app"

// The cross-origin-isolation header names and values every app page is
// served with (see the isolation notes on the loopback server and the
// WebView2 https vhost): COOP: same-origin + COEP: require-corp turn the
// origin cross-origin isolated, which makes SharedArrayBuffer available;
// CORP: same-origin keeps COEP from blocking the page's own scheme /
// loopback-origin subresources.
const (
	headerCOOP = "Cross-Origin-Opener-Policy"
	headerCOEP = "Cross-Origin-Embedder-Policy"
	headerCORP = "Cross-Origin-Resource-Policy"

	valSameOrigin  = "same-origin"
	valRequireCorp = "require-corp"
)

// callServe invokes serve with panic containment: content resolvers run
// inside native UI callbacks, where an unwound panic kills the process. A
// panicking resolver answers nil - the platform's "not found" - the same way
// a panicking binding answers status -1 (see callAndMarshal).
func callServe(serve serveFunc, req *request) (resp *response) {
	defer func() {
		if recover() != nil {
			resp = nil
		}
	}()
	return serve(req)
}

// --- webview lifecycle (shared by every engine) ---------------------------

// fireReady runs the view's Ready callback (View.Ready) on the UI thread,
// exactly once, when the first page load finishes.
func (w *webview) fireReady() {
	if w.onReadyFired {
		return
	}
	w.onReadyFired = true
	if w.onReady != nil {
		w.onReady()
	}
}

// releaseLoopback shuts down a window's per-view loopback server when the
// window is destroyed. A no-op for windows without one.
func (w *webview) releaseLoopback() {
	if w.transient == nil {
		return
	}
	stopLoopback(w.transient)
	w.transient = nil
	w.contentBase = ""
}

// Close tears the view down: it stops this view's run loop (if running) and
// destroys the window and web view. Idempotent; see View.Close.
func (w *webview) Close() {
	w.Terminate()
	w.Destroy()
}
