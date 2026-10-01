// Package tuohi is a pure-Go foundation for web-based desktop applications:
// it embeds the platform View (WKWebView on macOS, WebKitGTK on Linux,
// WebView2 on Windows) behind a single Go API - windowing and app windows,
// drag regions, custom URL schemes, bindings and events, opening URLs and
// native file dialogs - all cgo-free.
//
// The package is the window. The desktop services live in subpackages that
// do not import it: tuohi/instance (single instance), tuohi/autostart (launch
// at login), tuohi/clipboard, tuohi/notify and tuohi/tray. A service that
// needs the UI thread, such as the tray, is set up in App.Start.
//
// # The main thread
//
// Call App.Show and App.Wait from main, or from functions main calls, and run
// everything else in goroutines. macOS requires it: AppKit accepts UI work
// only on the process's main thread, and the package keeps the main goroutine
// there from its init on. Off the main thread, Show and Wait return
// ErrNotMainThread unless a run loop is already running on the main thread to
// hand the work to. Linux and Windows have no such rule, but a program written
// this way runs on all three. A program that serves its interface itself,
// over loopback HTTP, starts the server in a goroutine and opens the window
// on main:
//
//	func main() {
//		ln, err := net.Listen("tcp", "127.0.0.1:0")
//		if err != nil {
//			log.Fatal(err)
//		}
//		go http.Serve(ln, handler)
//
//		app := &tuohi.App{Exit: true}
//		view := &tuohi.View{URL: "http://" + ln.Addr().String() + "/"}
//		if err := app.Show(view); err != nil {
//			log.Fatal(err)
//		}
//		if err := app.Wait(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// Other View methods, such as Eval, Navigate and Close, are safe from any
// goroutine; they are marshalled to the UI thread.
//
// Source layout: app*.go holds the application scope - the App type
// (configuration + runtime context), its app-scope methods (Show, Wait,
// Open/Reveal, Backend) and the per-OS app internals (app icon,
// Open/Reveal); view*.go holds the view/window API surface (the define-first
// View struct and its methods, geometry and State, scheme types, App.Show
// glue, the View Dialog method, drag regions); bind.go (registry model +
// value conversion), bind_gen.go (the generated JS) and bind_evt.go (the
// events bridge) hold the binding and events machinery; engine.go holds the
// engine interface every platform satisfies and the code they share; and
// lib_darwin.go, lib_unix.go and lib_windows.go are the per-platform engine
// layer, which talks to WKWebView, WebKitGTK and WebView2 and the platform
// APIs.
//
// Platform code lives in *_unix.go / *_windows.go / *_darwin.go files; a
// capability that a platform cannot provide returns an Err* sentinel or is a
// documented best-effort no-op rather than failing at compile time.
package tuohi

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "embed"
)

//go:embed app.png

// _icon is the embedded mark: the PNG bytes of the application icon
// (app.png). It is the DEFAULT process icon, unexported on purpose - when the
// consumer leaves App.Icon unset, tuohi applies _icon itself: setAppIcon
// receives it so every tuohi app shows that face unless it brings its own.
var _icon []byte

// boxDownscale averages a square straight-RGBA image down to size×size with a
// box filter (each destination pixel is the mean of its source box). Straight
// (non-premultiplied) averaging keeps translucent edges from darkening. It is
// the shared downscaler for icon paths that need smaller copies than the
// source PNG (the Linux Wayland hicolor install, the Windows WM_SETICON
// handles).
//
//lint:ignore U1000 macOS renders the Dock icon at any size natively, so only the Unix and Windows icon installers (app_unix.go, app_windows.go) downscale through this shared helper.
func boxDownscale(src *image.NRGBA, size int) *image.NRGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0 := y * sh / size
		y1 := (y + 1) * sh / size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0 := x * sw / size
			x1 := (x + 1) * sw / size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a uint64
			for yy := y0; yy < y1; yy++ {
				row := src.PixOffset(x0, yy)
				for xx := x0; xx < x1; xx++ {
					r += uint64(src.Pix[row])
					g += uint64(src.Pix[row+1])
					b += uint64(src.Pix[row+2])
					a += uint64(src.Pix[row+3])
					row += 4
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			if n == 0 {
				n = 1
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(b / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}

// envDebug reports whether TUOHI_DEBUG=1 is set. It turns the dev tools on
// only for an App that sets AllowEnvDebug.
func envDebug() bool { return os.Getenv("TUOHI_DEBUG") == "1" }

// App configures a tuohi application and carries its runtime scope.
//
// It is the single application-scoped object: the exported fields hold the
// application settings (content filesystem, icon, the Start hook, ...)
// and unexported fields hold the state of the scope (committed settings and
// the one-time platform initialization). It is conceptually similar to how
// http.Server holds configuration and context together.
//
// The exported settings are read once - when the first App method is called -
// and are then committed: later edits to the fields do not affect the running
// application. That first call also performs the one-time platform
// initialization (ensureInit) before the requested action runs, so the call
// that opens the scope should come from the goroutine that will own the UI
// (the main goroutine).
//
// The app-scoped operations are methods on *App (Show, Wait, Quit, Open,
// Reveal, Backend); App.Bind is a declarative map instead of a method - see
// its field doc. This is a deliberate design choice: the App scope is never
// hidden from the consumer. The desktop services - single instance,
// autostart, clipboard, notifications and the tray - are not part of App;
// they live in their own subpackages (see the package doc).
type App struct {
	// Debug turns the platform web inspector / developer tools on for every
	// window of this app (the app-wide default for View.Debug): set it once
	// for "every window is debuggable". A view's own View.Debug ORs over it,
	// and so does the TUOHI_DEBUG=1 environment variable when AllowEnvDebug
	// is set.
	//
	// Like every App field it is committed when the app scope opens (later
	// edits have no effect) and it is read exactly once per view, at window
	// creation. Backend mapping - WebView2 AreDevToolsEnabled (Windows),
	// WebKitGTK enable-developer-extras (Linux), WKPreferences
	// developerExtrasEnabled (macOS).
	Debug bool

	// AllowEnvDebug lets whoever launches the program turn the developer
	// tools on for every view by setting the TUOHI_DEBUG=1 environment
	// variable. It is off by default, so a release build opens its web
	// inspector only when the application itself sets Debug or View.Debug:
	// the environment belongs to the person launching the program, and the
	// inspector reaches every page and binding. Set it in a development
	// build, or when debugging an installed build is worth that. It is
	// committed when the app scope opens, like every App field.
	AllowEnvDebug bool

	// Events names the JavaScript global the tuohi events bridge installs on
	// every page of this app: window.<Events> with on/off/emit (see
	// View.On/Off/Emit). Empty (the default) uses the name "events". The name
	// is fixed when each view is created.
	Events string

	// Name is the application name, used where the OS asks for one: as the
	// title of a window that has no other (see View.Title), and under GTK3 on
	// Wayland as the name of the desktop entry DesktopEntry installs.
	Name string

	// Icon is a PNG image for the running application, applied on a
	// best-effort basis wherever the platform supports it at runtime (macOS
	// Dock, Linux GTK3/GTK4 window icons, ...). It is applied once, when the
	// app scope opens and before the first window exists. When Icon is unset
	// the embedded tuohi mark is used instead (unexported; tuohi applies it
	// itself), so a tuohi application always has a process face unless it
	// brings its own. Unlike the per-window page icon it sets the face of the PROCESS; a
	// stable, runtime process icon is intentionally a best-effort feature
	// because it is hard to keep identical across all platforms. An unset icon
	// is silently ignored; so are environments that cannot take a runtime icon
	// (Windows reads the icon from the executable's own resources). Under an
	// X11 window manager the icon is pushed per window; under Wayland, where
	// the protocol has no per-window icons, GTK4 (>= 4.20) sends pixels via
	// the xdg-toplevel-icon protocol. A GTK3 window on Wayland shows the icon
	// only through a desktop entry: see DesktopEntry. A PNG that cannot be
	// decoded is never fatal.
	Icon []byte

	// DesktopEntry lets tuohi install a hidden desktop entry and a themed
	// copy of Icon under the user's data directory (XDG_DATA_HOME, by default
	// ~/.local/share), named after Name, so that a GTK3 window on Wayland
	// shows the icon. Wayland has no per-window icons, and the compositor
	// finds a window's icon through the desktop entry that matches its
	// app_id; GTK3, unlike GTK4, cannot send the pixels itself.
	//
	// It is false by default, and then opening a window writes nothing under
	// the user's home directory. It has no effect anywhere but GTK3 on
	// Wayland. An entry of that name that tuohi did not write, in the user's
	// data directory or a system one (XDG_DATA_DIRS), such as one an
	// installed package ships, is left alone and used as it is; once such an
	// entry appears, the one tuohi wrote is removed so that it is the one used.
	// The files stay after the application exits.
	DesktopEntry bool

	// Bind holds the application's declarative bindings: every entry is bound
	// onto each view App.Show creates, so one entry here covers all windows.
	// Keys and values follow the rules [View.Bind] documents: a dotted key
	// names a nested page variable, and the value's kind decides whether it
	// becomes a function, an accessor property or a frozen constant.
	//
	// What differs from View.Bind: a nil entry here binds nothing, and a view
	// overrides an app-wide name - or unbinds it with a nil entry - through
	// its own View.Bind map. The entries are applied to every view in
	// alphabetical key order, before the view's own (see App.Show). Like every
	// App field, Bind is committed when the app scope opens (later edits have
	// no effect) and is read once per shown view, at window creation.
	Bind map[string]any

	// FS is the filesystem the application serves to its views - the app's
	// content: HTML, CSS, scripts and anything else the page loads. When it is
	// set (it is read once, when the app scope opens, like every App setting;
	// later edits have no effect), every view shown by App.Show is served
	// from it. The consumer never picks a serving mechanism: navigate the view
	// to the uniform "app://" origin -
	//
	//	w.Navigate("app://app/index.html")
	//
	// - and tuohi serves the file at that path in the filesystem on every
	// platform. A URL with no host, such as a bare "app://" or
	// "app://#route", is read as the root of the "app" host,
	// "app://app/#route", so the page keeps its bridge. The serving is scheme-first on Windows and Linux: WebView2's
	// https vhost for the custom "app" scheme (whose responses carry the
	// isolation headers), the registered custom scheme on Linux (WebKitGTK
	// cannot attach the headers to scheme responses, so a scheme-served Linux
	// page is not crossOriginIsolated - SharedArrayBuffer still works through
	// the JSC_useSharedArrayBuffer option). macOS always serves over a
	// per-view loopback http://localhost server (WKWebView cannot make a
	// custom scheme a secure context, and a long-standing WebKit bug keeps
	// SharedArrayBuffer off plain WKWebView pages); App.HTTP opts Linux and
	// Windows into that same loopback origin. SharedArrayBuffer is available
	// on every platform. A path without a file answers "not found". A nil FS serves
	// no content - the window shows whatever the consumer navigates it to
	// itself.
	//
	// What FS holds is readable by the views it is served to, not private to
	// the process. The loopback server answers only its own view (see HTTP),
	// but another process running as the same user can read FS anyway, from
	// the binary or from memory; keep secrets out of it.
	FS fs.FS

	// HTTP serves the app's content over a per-view loopback http://localhost
	// server instead of the platform's custom "app" scheme. On Linux and
	// Windows it is an opt-in fallback - the native scheme serves them (see
	// the FS doc), so only HTTP-opted windows load their app:// content from
	// the loopback origin. Each such view has a server of its own, listening
	// on 127.0.0.1 only, from the view's creation until it is destroyed. The
	// server answers only requests that name its own origin and carry its
	// per-server token, which the view's first navigation turns into a
	// cookie, so neither a web page reaching the port by DNS rebinding nor
	// another user's process gets anything from it. macOS always serves over the loopback origin
	// (WKWebView cannot make a custom scheme a secure context and cannot
	// provide SharedArrayBuffer on plain pages - a long-standing WebKit
	// bug). Either
	// way the consumer still navigates to the uniform "app://" origin, so
	// HTTP is a purely internal serving choice. Every served response -
	// loopback and vhost alike - carries the cross-origin-isolation headers
	// (COOP/COEP + CORP), so every app page is cross-origin isolated and can
	// use SharedArrayBuffer. Ignored when FS is nil.
	HTTP bool

	// Exit ends the application process when its last window closes: with
	// Exit true, Wait returns as soon as the last window created with
	// App.Show is gone. The default (false) keeps the process alive after the
	// windows close - menu-bar/tray/background applications - and Wait then
	// returns only when App.Quit is called.
	Exit bool

	// Start, when set, runs on the UI thread when Wait starts, before its
	// loop dispatches any event. It is where a service that needs the UI
	// thread is set up, such as a tray icon:
	//
	//	app.Start = func() error { return tray.Set(cfg) }
	//
	// and tear it down after Wait returns (tray.Remove). An error from
	// Start ends Wait, which returns it.
	//
	// Like every App field it is committed when the app scope opens (later
	// edits have no effect).
	Start func() error

	// --- internal scope state; see the App doc ---
	scopeOnce sync.Once
	scope     *appScope
}

// appConfig is a plain snapshot of an App's exported settings, taken exactly
// once when the app scope is opened. It carries no synchronization state so
// it can be passed by value freely.
type appConfig struct {
	Name         string
	Exit         bool
	Start        func() error
	Icon         []byte
	DesktopEntry bool
	FS           fs.FS
	HTTP         bool
	Debug        bool
	Events       string
	Bind         map[string]any
}

// snapshotConfig copies an App's exported settings into a plain appConfig.
// Debug commits App.Debug OR, when App.AllowEnvDebug is set, the
// TUOHI_DEBUG=1 environment variable.
func snapshotConfig(a *App) appConfig {
	return appConfig{
		Name:         a.Name,
		Exit:         a.Exit,
		Start:        a.Start,
		Icon:         a.Icon,
		DesktopEntry: a.DesktopEntry,
		FS:           a.FS,
		HTTP:         a.HTTP,
		Debug:        a.Debug || (a.AllowEnvDebug && envDebug()),
		Events:       a.Events,
		Bind:         a.Bind,
	}
}

// appScope is the committed configuration of an App plus the state of the
// app scope. It is created exactly once, on the first App method call.
type appScope struct {
	cfg     appConfig // committed settings snapshot
	initErr error

	// Lifecycle state. startOnce guards the one-time app start (the icon,
	// see (*App).start); windows counts the
	// live windows created through App.Show (owned windows only);
	// exitOnce/exitFlag end the Wait run loop (App.Quit, or the last window
	// closing when App.Exit is set). No app-scope web server exists: the
	// loopback servers (macOS always, Linux and Windows under App.HTTP) are
	// per-view and owned by the engine (see viewContentBase).
	startOnce sync.Once
	windows   int32
	exitOnce  sync.Once
	exitFlag  int32

	// views is the set of Views currently shown (and managed) by this App,
	// each with the engine App.Show created for it. App.Show registers a View
	// the first time it creates its window and View.Close unregisters it. The
	// engine lets a Close unregister only the window it closed: a View shown
	// again while an earlier Close was still tearing down keeps its new entry.
	viewsMu sync.Mutex
	views   map[*View]engine
}

// registerView records v, shown with engine w, as a window this App manages
// (called by App.Show).
func (a *App) registerView(v *View, w engine) {
	if a == nil || v == nil {
		return
	}
	s := a.scope
	if s == nil {
		return
	}
	s.viewsMu.Lock()
	if s.views == nil {
		s.views = make(map[*View]engine)
	}
	s.views[v] = w
	s.viewsMu.Unlock()
}

// unregisterView drops v from the managed set when it is still registered
// with engine w, the one being closed (called by View.Close).
func (a *App) unregisterView(v *View, w engine) {
	if a == nil || v == nil {
		return
	}
	s := a.scope
	if s == nil {
		return
	}
	s.viewsMu.Lock()
	if s.views[v] == w {
		delete(s.views, v)
	}
	s.viewsMu.Unlock()
}

// begin commits the App's current settings (later field edits are ignored)
// and performs the one-time platform initialization, returning the committed
// scope. Every App method calls begin before running its action.
func (a *App) begin() (*appScope, error) {
	if a == nil {
		return nil, errors.New("tuohi: nil *App")
	}
	a.scopeOnce.Do(func() {
		s := &appScope{cfg: snapshotConfig(a)}
		s.initErr = ensureInit()
		a.scope = s
		scopePtr.Store(s)
	})
	if a.scope.initErr != nil {
		return nil, a.scope.initErr
	}
	return a.scope, nil
}

// Wait blocks until the application exits. It is the app-level run loop: it
// opens the app scope once (committing the settings and performing the
// one-time platform initialization) and then runs the platform UI loop until
// the application should exit:
//
//   - App.Quit was called, or
//   - the last window created with App.Show was closed and App.Exit is true.
//
// With Exit false (the default) the process keeps running after its windows
// are gone - menu-bar/tray/background applications - and only App.Quit (or
// the process being killed) ends it.
//
// Create all windows with App.Show and keep every UI call on the goroutine
// that calls Wait (the main goroutine). Every app, even one with a single
// window, runs its loop through Wait: View has no Run method of its own.
func (a *App) Wait() error {
	// Before the scope opens, so that a refused call does no platform
	// initialization on the wrong thread.
	if err := uiThreadErr(); err != nil {
		return err
	}
	s, err := a.begin()
	if err != nil {
		return fmt.Errorf("tuohi: wait: %w", err)
	}
	// The icon and App.Start set up what needs the UI thread before the loop
	// below dispatches its events. Wait normally runs on that thread; on
	// macOS under a loop another owner runs, it may not, and startOnUI hands
	// the work to the main thread.
	var startErr error
	if err := startOnUI(func() {
		a.start(s)
		if s.cfg.Start != nil {
			startErr = s.cfg.Start()
		}
	}); err != nil {
		return fmt.Errorf("tuohi: start: %w", err)
	}
	if startErr != nil {
		return fmt.Errorf("tuohi: start: %w", startErr)
	}
	ui.enterLoop()
	for atomic.LoadInt32(&s.exitFlag) == 0 {
		appUIWait()
	}
	ui.exitLoop()
	return nil
}

// Quit asks a running application to terminate: Wait returns and the process
// may finish. It is safe to call from any goroutine (the UI loop is woken).
// Calling Quit before Wait is harmless - Wait then returns immediately.
func (a *App) Quit() {
	s, err := a.begin()
	if err != nil {
		return
	}
	s.requestExit()
}

// start performs the one-time application initialization: the best-effort
// runtime icon (App.Icon). It runs once - on the first window creation or
// the first Wait call - which is the single "app initialization" point of the
// scope; later calls are no-ops. App.Show and Wait both call it, so the order does
// not matter. Content serving is NOT started here: there is no permanently
// listening web server. App.FS is served per view - through WebView2's https
// vhost on Windows, the custom scheme on Linux, or a per-view loopback
// server (macOS always; Linux and Windows when App.HTTP opts in), started at
// window creation and stopped when the window is destroyed (see
// viewContentBase).
func (a *App) start(s *appScope) {
	s.startOnce.Do(func() {
		// The process icon: App.Icon when the consumer set one, otherwise the
		// embedded tuohi mark (_icon). setAppIcon is best-effort per platform
		// (Dock on macOS, GTK window icons on Linux, no-op on Windows) and its
		// errors are deliberately ignored - an un-decodable PNG must never
		// keep the application from starting.
		icon := s.cfg.Icon
		if len(icon) == 0 {
			icon = _icon
		}
		_ = setAppIcon(icon, s.cfg.Name, s.cfg.DesktopEntry)
	})
}

// scopePtr points at the currently active App scope so the per-OS engines can
// report window close events into it (appWindowClosed). One live application
// per process is the supported model.
var scopePtr atomic.Pointer[appScope]

// appWindowClosed is invoked by the per-OS engines when an owned window
// created through App.Show is destroyed (user close or View.Close); it
// lets Wait notice the last window closing.
func appWindowClosed() {
	if s := scopePtr.Load(); s != nil {
		s.windowClosed()
	}
}

func (s *appScope) windowClosed() {
	if atomic.AddInt32(&s.windows, -1) == 0 && s.cfg.Exit {
		s.requestExit()
	}
}

func (s *appScope) requestExit() {
	s.exitOnce.Do(func() {
		atomic.StoreInt32(&s.exitFlag, 1)
		appUIWake()
	})
}

// Backend reports which web-engine backend the App scope uses for its views,
// after the one-time platform initialization has run:
//
//   - "webkitgtk-6.0" or "webkit2gtk-4.1" on Linux - the stack that was
//     actually loaded, honoring the TUOHI_BACKEND environment variable
//     (see README "Linux shared libraries");
//   - "WKWebView" on macOS and "WebView2" on Windows, whose single built-in
//     backend ignores the variable.
//
// Like every App method it opens the scope first, so the returned name always
// matches the loaded backend rather than the requested one. It returns an
// empty string when the platform could not be initialized.
func (a *App) Backend() string {
	if _, err := a.begin(); err != nil {
		return ""
	}
	return platformBackend()
}

// binder is the surface bindEntry writes every declarative entry onto: the
// *webview backends and the binding-recording test stubs both implement Bind,
// which accepts one value (function / constant / accessor-pair value) or an
// explicit (getter, setter) pair - see bindEntries.
type binder interface {
	Bind(name string, vals ...any) error
}

// binderBatch is the OPTIONAL batching surface applyBinds uses when the
// engine offers it: every declarative binding of one window is prepared,
// registered and live-installed in ONE pass - a single script rebuild and a
// single live-install Eval - instead of one full rebuild per name.
// Every real engine implements it; the recording test stubs don't, so
// applyBinds falls back to per-name Bind calls for them.
type binderBatch interface {
	BindBatch(batch []bindRequest) error
}

// bindRequest is one name → values request of a bind batch (see binderBatch
// and applyBinds).
type bindRequest struct {
	name string
	vals []any
}

// bindEntry binds one declarative map entry (name → v) onto w. The entry's
// name is a DOTTED path - dots separate nested variables on the page, so a
// value bound at "app.someAPI.call" is installed as window.app.someAPI.call:
// a value that is a function becomes a JS function the page calls, and any
// other value is bound as an immutable JS constant (see the App.Bind and
// View.Bind docs). The value binds as ONE name - nothing is derived from the
// Go type, so structs and maps are never walked (the engine decides how to
// turn the value into a function or a constant, see makeBinding).
//
// It returns the bound names (always exactly [name]) and the first error
// encountered. A nil w or a nil value is an error.
func bindEntry(w binder, name string, v any) ([]string, error) {
	if w == nil {
		return nil, fmt.Errorf("tuohi: Bind requires a non-nil View")
	}
	if v == nil {
		return nil, fmt.Errorf("tuohi: Bind requires a non-nil value")
	}
	if err := w.Bind(name, v); err != nil {
		return nil, fmt.Errorf("binding %s: %w", name, err)
	}
	return []string{name}, nil
}

// applyBinds binds the declarative App.Bind and View.Bind maps onto w in a
// DETERMINISTIC order: first every non-nil App.Bind entry, then every
// View.Bind entry, each map iterated in alphabetical key order, so the result
// never depends on Go's randomized map iteration. A non-nil view entry with
// the same key as an app entry overrides it (the engine replaces the
// binding, see bindingsReplace); a nil view entry UNBINDS that key again -
// it removes whatever the app bound under that name, so a view can drop an
// app-wide binding it does not want. A nil App.Bind entry binds nothing
// (there is nothing app-wide to unbind yet; per-view nil entries do the
// unbinding). Called by App.Show at window creation; the first error
// encountered is returned, after which the remaining entries are not
// applied.
//
// Every entry name is validated before anything is bound (planBinds): the
// dotted-name rules (validateBindName), the reserved/denylist top-level
// checks (validateTopLevel) and the dotted-prefix collision check
// (checkDottedPrefixes) fail loudly HERE - at App.Show - instead of letting
// two names silently destroy each other on the page.
func applyBinds(w binder, appBinds, viewBinds map[string]any) error {
	binds, unbinds, err := planBinds(appBinds, viewBinds, eventsGlobalOf(w))
	if err != nil {
		return err
	}
	// Engine bindings register and install in ONE batch pass; stubs
	// without BindBatch fall back to the same per-name calls as before.
	if len(binds) > 0 {
		if bw, ok := w.(binderBatch); ok {
			if err := bw.BindBatch(binds); err != nil {
				return err
			}
		} else {
			for _, r := range binds {
				if _, err := bindEntry(w, r.name, r.vals[0]); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range unbinds {
		u, ok := w.(interface{ Unbind(string) error })
		if !ok {
			return fmt.Errorf("tuohi: unbinding %s: engine cannot unbind", name)
		}
		if err := u.Unbind(name); err != nil {
			return fmt.Errorf("tuohi: unbinding %s: %w", name, err)
		}
	}
	return nil
}

// planBinds turns the declarative App.Bind + View.Bind maps into the ordered
// bind requests and unbind names applyBinds performs, validating every name
// on the way: each BIND name is checked with validateBindName and
// validateTopLevel (the latter knows the view's events-global name so a
// top-level binding cannot clobber window.<events>), and the FINAL name set
// - app entries minus the nil-view-unbound ones, plus the view entries that
// override - is checked for dotted-prefix collisions (two names where one is
// nested under the other would make the installs destroy each other
// order-dependently, see checkDottedPrefixes). Unbind names get the segment
// rules only (reserved or denylisted names are fine to unbind). Returns the
// requests in deterministic order: app keys, then view keys, alphabetical
// within each map.
func planBinds(appBinds, viewBinds map[string]any, eventsGlobal string) (binds []bindRequest, unbinds []string, err error) {
	// final is the set of names that end up bound, the domain of the prefix-collision check.
	final := make(map[string]bool, len(appBinds)+len(viewBinds))
	check := func(name string) error {
		if err := validateBindName(name); err != nil {
			return err
		}
		return validateTopLevel(name, eventsGlobal)
	}
	for _, name := range sortedMapKeys(appBinds) {
		v := appBinds[name]
		if v == nil {
			continue // nil app entry binds nothing (see the App.Bind doc)
		}
		if err := check(name); err != nil {
			return nil, nil, err
		}
		binds = append(binds, bindRequest{name: name, vals: []any{v}})
		final[name] = true
	}
	for _, name := range sortedMapKeys(viewBinds) {
		v := viewBinds[name]
		if v == nil {
			// Nil view entry: unbind the app-wide binding of the same name.
			// A no-op when the app bound nothing under it - and such names
			// never enter the final set, so they cannot collide with a bind.
			if appBinds[name] == nil {
				continue
			}
			if err := validateBindName(name); err != nil {
				return nil, nil, err
			}
			unbinds = append(unbinds, name)
			delete(final, name)
			continue
		}
		if err := check(name); err != nil {
			return nil, nil, err
		}
		binds = append(binds, bindRequest{name: name, vals: []any{v}})
		final[name] = true
	}
	if err := checkDottedPrefixes(final); err != nil {
		return nil, nil, err
	}
	return binds, unbinds, nil
}

// eventsGlobalOf returns the events-global name a binder's view will install
// the events API at, or the default "events" when the binder does not expose
// it (test stubs).
func eventsGlobalOf(w binder) string {
	if g, ok := w.(eventsGlobalProvider); ok {
		return g.eventsGlobalName()
	}
	return "events"
}

// windowGlobalDenylist is the small set of top-level window names a binding
// must not take: replacing these silently breaks the page's own globals (and
// often tuohi's injected scripts) with no error anywhere. The check
// only applies to the FIRST name segment - names under a consumer-chosen
// namespace like "demo.open" are the consumer's own object and are fine. The
// list is deliberately conservative: the window built-ins every page relies
// on. "close"/"open"/"name"-style collisions were the original motivation.
var windowGlobalDenylist = map[string]bool{
	"close": true, "open": true, "name": true, "top": true, "parent": true,
	"self": true, "frames": true, "length": true, "status": true, "location": true,
	"history": true, "navigator": true, "document": true, "screen": true, "origin": true,
	"devicePixelRatio": true, "alert": true, "confirm": true, "prompt": true, "print": true,
	"fetch": true, "crypto": true, "localStorage": true, "sessionStorage": true, "indexedDB": true,
	"postMessage": true, "addEventListener": true, "removeEventListener": true,
	"requestAnimationFrame": true, "setTimeout": true, "setInterval": true,
	"clearTimeout": true, "clearInterval": true, "getComputedStyle": true, "matchMedia": true,
}

// validateTopLevel rejects bind names that would clobber tuohi's own page
// surface or a common window global: the first dot-segment of the name must
// not equal the events API global of this view (window.<eventsGlobal>), must
// not be "__webview__" (the bridge instance) and must not start with
// "__tuohi" (every internal message method and the events binding live
// there, see the internal* constants), and must not be a denylisted window
// built-in (windowGlobalDenylist). Deeper segments are not restricted: they
// live under the consumer's own namespace objects.
func validateTopLevel(name, eventsGlobal string) error {
	top := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		top = name[:i]
	}
	if top == "__webview__" || strings.HasPrefix(top, "__tuohi") {
		return fmt.Errorf("tuohi: binding name %q is reserved for tuohi's internal page API", name)
	}
	if top == eventsGlobal {
		return fmt.Errorf("tuohi: binding name %q would replace the page's events API (window.%s)", name, eventsGlobal)
	}
	if windowGlobalDenylist[top] {
		return fmt.Errorf("tuohi: binding name %q would replace the page's own window.%s", name, top)
	}
	return nil
}

// checkDottedPrefixes reports an error when any two final binding names are
// in a dotted-prefix relationship - one name is a segment-wise prefix of
// another ("api" vs "api.id", "app.x" vs "app.x.y"). The page installer
// creates namespace objects for dotted names, so binding both a leaf and a
// namespace under it is order-dependent and one of the two silently destroys
// the other; validating the whole final set up front makes the failure
// loud, deterministic and independent of Go's map order.
func checkDottedPrefixes(names map[string]bool) error {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if strings.HasPrefix(keys[i], keys[i-1]+".") {
			return fmt.Errorf("tuohi: binding names %q and %q collide: %q is nested under %q, and a leaf and its namespace cannot both be bound", keys[i], keys[i-1], keys[i], keys[i-1])
		}
	}
	return nil
}

// sortedMapKeys returns m's keys sorted alphabetically - the deterministic
// iteration order every declarative map (App.Bind, View.Bind) is applied in.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cloneBindMap returns a defensive copy of a Bind map, taken at the moment
// App.Show reads it. The declarative bind maps are shared, unlocked Go
// maps the consumer may keep mutating; binding reads them exactly once, so
// snapshotting at first read removes the "map read while a goroutine writes
// it" footgun (the app-wide App.Bind map is snapshotted the same way inside
// snapshotConfig).
func cloneBindMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Open opens rawurl with the user's default handler: the browser or the mail
// client. Only http, https, and mailto URLs are allowed. Anything else,
// including file: and a bare hostname or path with no scheme, returns
// ErrScheme. To show a local file, use Reveal.
//
// file: is refused because the platform opener runs what it is given: a
// file: URL for an executable, a .app, a .lnk, or a .desktop file launches it
// through ShellExecute, NSWorkspace, or xdg-open. Open is also where a view's
// external links are sent, so its argument may come from a page.
func (a *App) Open(rawurl string) error {
	// Refuse a disallowed scheme BEFORE opening the app scope: nothing is
	// launched either way, and a refused URL must not initialize the platform
	// backend (so Open stays usable where no GUI stack is loadable, and a
	// backend init failure can never mask the scheme refusal).
	if err := validateScheme(rawurl); err != nil {
		return err
	}
	if _, err := a.begin(); err != nil {
		return err
	}
	return openURL(rawurl)
}

// Reveal opens the platform file manager with path's location shown: Finder
// selects the file on macOS, Explorer selects it on Windows, and on Linux the
// containing folder is opened (selecting the file itself is file-manager
// specific and not portable). The path must exist.
func (a *App) Reveal(path string) error {
	if _, err := a.begin(); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("tuohi: resolve %q: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("tuohi: reveal %q: %w", path, err)
	}
	return revealFile(abs)
}

// validateScheme enforces the Open allow-list. url.Parse normalizes the scheme
// to lower case, so the comparison is already case-insensitive.
func validateScheme(rawurl string) error {
	u, err := url.Parse(rawurl)
	if err != nil {
		return fmt.Errorf("tuohi: parse %q: %w", rawurl, err)
	}
	if !allowedSchemes[u.Scheme] {
		return fmt.Errorf("%w: %q (allowed: http, https, mailto)", ErrScheme, u.Scheme)
	}
	return nil
}

// allowedSchemes is the set Open will hand to the OS. Keeping it small is the
// safety boundary: an attacker-controlled string can at worst open a web page
// or an email draft, never a local program or a custom protocol handler.
var allowedSchemes = map[string]bool{
	"http":   true,
	"https":  true,
	"mailto": true,
}

// ErrNotMainThread is returned on macOS by App.Show and App.Wait called off
// the process's main thread when no run loop is running there to take the
// work. AppKit runs only on the main thread, so call Show and Wait from main
// and run anything else, such as an HTTP server, in a goroutine (see the
// package doc). It is never returned on Linux or Windows.
var ErrNotMainThread = errors.New("tuohi: on macOS the UI runs on the main thread; call App.Show and App.Wait from main")

// ErrScheme is returned by Open when the URL's scheme is not in the allow-list.
var ErrScheme = errors.New("tuohi: refused URL scheme")

// serveAppFS returns the content resolver for an App.FS: it maps a request
// URL's path onto a file in the filesystem and answers it with the matching
// MIME type (an empty path serves the root "index.html"). A nil response
// means the path is not in the filesystem - "not found". It returns nil when
// root is nil, so a window whose App.FS is unset registers no content at all.
func serveAppFS(root fs.FS) serveFunc {
	if root == nil {
		return nil
	}
	return func(r *request) *response {
		u, err := url.Parse(r.URL)
		if err != nil {
			return nil
		}
		name := strings.TrimPrefix(u.Path, "/")
		if name == "" {
			name = "index.html"
		}
		// fs.ReadFile validates the path exactly like every io/fs operation:
		// ".." segments, absolute paths and empty names are rejected, so a
		// request path can never escape the served filesystem (no traversal).
		data, err := fs.ReadFile(root, name)
		if err != nil {
			return nil
		}
		return &response{Body: data, MIME: mimeTypeFor(name)}
	}
}

// mimeTypeFor returns the Content-Type for a served file name, chosen from
// the extensions a self-contained app UI uses; text types carry a charset so
// the page renders consistently on every platform.
func mimeTypeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}

// --- per-view content serving (App.FS) --------------------------------------

// rewriteAppURL maps a uniform app:// URL onto a loopback server's base -
// same path, same query and fragment - and passes any other URL through
// unchanged. base is the base URL of a view's loopback server,
// http://localhost:<port>/.tuohi/<token> (see loopbackServer); an empty base leaves app:// URLs untouched
// (served through the native scheme).
func rewriteAppURL(base, raw string) string {
	if base == "" || !strings.HasPrefix(raw, appSchemeName+"://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	out := base + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// viewContentBase resolves the serving origin a NEW view's app:// URLs map
// onto (see App.FS / App.HTTP). There is no app-wide web server: a view is
// either served through the engine's custom "app" scheme or over its own
// loopback server, which lives as long as the view. forceLoopback makes the view use
// the loopback origin unconditionally (macOS always does - WKWebView cannot
// make a custom scheme a secure context, and a long-standing WebKit bug
// keeps SharedArrayBuffer off plain WKWebView pages); without it, Linux and
// Windows use the loopback origin only when App.HTTP opts in (their native
// scheme serving is enough otherwise - the https vhost on Windows, the
// registered custom scheme on Linux). The caller owns the server and MUST
// stop it (stopLoopback) when the view is destroyed.
//
// The loopback responses carry the cross-origin-isolation headers (COOP/COEP
// + CORP), so every served page is a secure, cross-origin-isolated context
// with SharedArrayBuffer available.
func viewContentBase(v *View, forceLoopback bool) (base string, transient *loopbackServer, err error) {
	s := scopePtr.Load()
	if s == nil || s.cfg.FS == nil {
		return "", nil, nil
	}
	if !s.cfg.HTTP && !forceLoopback {
		return "", nil, nil
	}
	srv, err := s.startViewServer()
	if err != nil {
		return "", nil, err
	}
	if srv == nil {
		return "", nil, nil
	}
	return srv.base, srv, nil
}

// startViewServer starts the loopback server that serves App.FS to
// ONE view (see viewContentBase / App.HTTP). It returns nil when App.FS is nil.
func (s *appScope) startViewServer() (*loopbackServer, error) {
	if s.cfg.FS == nil {
		return nil, nil
	}
	srv, _, err := listenLoopbackHTTP(serveAppFS(s.cfg.FS))
	if err != nil {
		return nil, fmt.Errorf("tuohi: serve App.FS for a view: %w", err)
	}
	return srv, nil
}

// stopLoopback shuts a per-view loopback server down, when its view is
// destroyed. It is a no-op for a nil server and safe to call more than once.
func stopLoopback(srv *loopbackServer) {
	if srv == nil {
		return
	}
	_ = srv.Close()
}

// loopbackServer is a per-view loopback HTTP server created by
// listenLoopbackHTTP. It serves ONE view's content for the life of that view
// and is owned by the engine, which closes it when the view is destroyed.
// Every response carries the cross-origin-isolation headers (COOP/COEP +
// CORP, see writeResponse), so the http://localhost origin is a secure,
// cross-origin-isolated context with SharedArrayBuffer available.
//
// Only the view may read from it. A request must name the server's own
// origin in its Host header, which a web page reaching the port through DNS
// rebinding cannot, and must carry the server's token. The view's first
// navigation carries it in the path of base; the server answers that with a
// redirect to the same path without it and a cookie holding the token,
// which every later request of the page then sends. A request with neither
// is refused, so another user's process that finds the port gets nothing. A
// process running as the same user can still read App.FS: it can read the
// binary and the view's memory too, so the token does not try to stop it.
type loopbackServer struct {
	ln    net.Listener
	base  string // base URL the view navigates under, e.g. "http://localhost:41234/.tuohi/<token>"
	port  string // the bound port, which the Host header must name
	token string // the per-server secret; see tokenPrefix and cookieName

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// tokenPrefix starts the path segment that carries a server's token in the
// view's first navigation: /.tuohi/<token>/<path>.
const tokenPrefix = "/.tuohi/"

// cookieName is the cookie that carries the token after the first
// navigation. Cookies are shared by every port of a host, so each server
// names its own after its port.
func (s *loopbackServer) cookieName() string { return "tuohi-" + s.port }

// listenLoopbackHTTP binds a LOOPBACK TCP listener and serves serve on it.
// It is the constructor of every per-view loopback server. Servers always
// bind the loopback default "127.0.0.1:0" - a free loopback port; there is
// no permanently listening or exposed web server.
//
// A server lives until Close, which the engine calls when its view is
// destroyed: the page's origin is this server, so a lazy import, a route
// change or a fetch long after the first load must still reach it.
//
// It returns the running server plus the base URL the view navigates under:
// the http://localhost form of the bound port, because that origin is a
// secure context in the embedded web view, followed by the token segment
// (see loopbackServer). "localhost" resolves to the loopback address
// actually bound, so the advertised URL always reaches this server.
func listenLoopbackHTTP(serve serveFunc) (*loopbackServer, string, error) {
	if serve == nil {
		return nil, "", errors.New("loopback server: nil resolver")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, "", fmt.Errorf("loopback server: token: %w", err)
	}
	ln, err := loopbackListen("")
	if err != nil {
		return nil, "", err
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, "", errors.New("loopback server: failed to read tcp listen address")
	}
	s := &loopbackServer{
		ln:    ln,
		port:  strconv.Itoa(tcpAddr.Port),
		token: base64.RawURLEncoding.EncodeToString(secret[:]),
		conns: make(map[net.Conn]struct{}),
	}
	s.base = "http://localhost:" + s.port + strings.TrimSuffix(tokenPrefix, "/") + "/" + s.token
	go s.serve(serve)
	return s, s.base, nil
}

// isClosed reports whether the server has been shut down. Engines use it to
// notice that a view's server is gone and drop their per-view rewrite base.
func (s *loopbackServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// loopbackListen binds a loopback TCP listener on a free loopback port
// (127.0.0.1:0). Only loopback is accepted - the served resolver is the app's
// own UI, and loopback servers are never exposed - so any other address is
// refused with an error.
func loopbackListen(addr string) (net.Listener, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("loopback server: invalid listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || !ip.IsLoopback() {
		return nil, fmt.Errorf("loopback server: refusing to listen on %q; loopback only (127.0.0.1 / [::1])", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("loopback server: listen %s: %w", addr, err)
	}
	return ln, nil
}

// Close stops the server: the listener closes (no new connections) and every
// in-flight connection is shut down. It is a no-op once the server is closed
// and safe to call more than once (later calls return nil).
func (s *loopbackServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	return err
}

// serve accepts connections until the listener closes and answers each on its
// own goroutine.
func (s *loopbackServer) serve(serve serveFunc) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // listener closed via Close
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go func() {
			defer func() {
				_ = conn.Close()
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
			}()
			s.serveConn(conn, serve)
		}()
	}
}

// serveConn reads one request from conn and writes the response. Connections
// are answered once and closed (Connection: close), which keeps the
// hand-rolled parsing trivially correct: there is no keep-alive bookkeeping
// to get wrong. A request that does not name this server in its Host header,
// or carries neither the token nor its cookie, is refused (see
// loopbackServer).
func (s *loopbackServer) serveConn(conn net.Conn, serve serveFunc) {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return // client went away before sending anything; nothing to answer
	}
	method, target, version, ok := parseRequestLine(line)
	if !ok || (version != "HTTP/1.0" && version != "HTTP/1.1") {
		s.writeStatus(conn, 400)
		return
	}
	// Read and discard the header block. The read is bounded (maxHeaderBytes)
	// so a hostile client cannot make us buffer without limit. No request
	// body is read: GET/HEAD never has one, and the server answers then
	// closes.
	var host string
	var cookies []string
	headerBytes := 0
	for {
		if headerBytes > maxHeaderBytes {
			s.writeStatus(conn, 400)
			return
		}
		header, err := br.ReadString('\n')
		if err != nil {
			return
		}
		headerBytes += len(header)
		trimmed := strings.TrimRight(header, "\r\n")
		if trimmed == "" {
			break // end of headers
		}
		if name, value, ok := strings.Cut(trimmed, ":"); ok {
			name = strings.ToLower(strings.TrimSpace(name))
			switch {
			case name == "host" && host == "":
				host = strings.TrimSpace(value)
			case name == "cookie":
				cookies = append(cookies, value)
			}
		}
	}
	if method != "GET" && method != "HEAD" {
		s.writeStatus(conn, 405)
		return
	}
	if !s.ownHost(host) {
		s.writeStatus(conn, 421)
		return
	}
	if rest, ok := strings.CutPrefix(target, tokenPrefix); ok {
		// The view's navigation: swap the token in the path for the cookie,
		// and send the page to its path without it.
		token, after, _ := strings.Cut(rest, "/")
		if !s.validToken(token) {
			s.writeStatus(conn, 403)
			return
		}
		s.writeRedirect(conn, "/"+after)
		return
	}
	if !s.hasCookie(cookies) {
		s.writeStatus(conn, 403)
		return
	}
	// The resolver sees the full URL of the origin it is served from.
	reqURL := "http://" + host + target
	resp := callServe(serve, &request{Method: method, URL: reqURL})
	if resp == nil {
		s.writeStatus(conn, 404)
		return
	}
	// A HEAD response carries the same headers as GET - including the real
	// Content-Length of the body the GET would send - but no body bytes.
	s.writeResponse(conn, 200, schemeMIME(resp), resp.Body, method == "HEAD")
}

// ownHost reports whether a Host header names this server's origin: localhost
// or 127.0.0.1 with its port. A page that reached the port through DNS
// rebinding sends its own host name instead.
func (s *loopbackServer) ownHost(host string) bool {
	return strings.EqualFold(host, "localhost:"+s.port) || host == "127.0.0.1:"+s.port
}

// validToken compares a token in constant time.
func (s *loopbackServer) validToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

// hasCookie reports whether any Cookie header carries this server's token.
func (s *loopbackServer) hasCookie(headers []string) bool {
	for _, h := range headers {
		for _, c := range strings.Split(h, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(c), "=")
			if ok && name == s.cookieName() && s.validToken(value) {
				return true
			}
		}
	}
	return false
}

// writeRedirect answers the view's token-bearing navigation: a redirect to
// location, the same path and query without the token, with the cookie that
// carries the token from then on. HttpOnly keeps it from the page's script.
// SameSite=Lax keeps it from every request another site's page starts except
// a top-level navigation, which loads the app into the view and shows the
// other page nothing. Strict would withhold it from the redirected request
// itself when the view comes to the app from another site's page, as a view
// that starts on a remote URL does.
func (s *loopbackServer) writeRedirect(conn net.Conn, location string) {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", 302, statusText(302))
	fmt.Fprintf(&b, "Location: %s\r\n", location)
	fmt.Fprintf(&b, "Set-Cookie: %s=%s; Path=/; HttpOnly; SameSite=Lax\r\n", s.cookieName(), s.token)
	b.WriteString("Cache-Control: no-store\r\n")
	b.WriteString("Content-Length: 0\r\n")
	b.WriteString("Connection: close\r\n\r\n")
	_, _ = conn.Write([]byte(b.String()))
}

// maxHeaderBytes bounds the request-line + header block a client may send;
// beyond it the server answers 400 instead of buffering without limit.
const maxHeaderBytes = 64 << 10

// parseRequestLine splits "GET /path?query HTTP/1.1" into its parts. Like
// most servers it tolerates runs of spaces or tabs between the tokens; ok is
// false when there are not exactly three tokens or the target is empty.
func parseRequestLine(line string) (method, target, version string, ok bool) {
	fields := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(fields) != 3 {
		return "", "", "", false
	}
	if fields[1] == "" || strings.ContainsAny(fields[1], " \t") {
		return "", "", "", false
	}
	return fields[0], fields[1], fields[2], true
}

// writeResponse writes a full HTTP/1.1 response: status line, Content-Type,
// Content-Length, the cross-origin-isolation headers (COOP: same-origin +
// COEP: require-corp turn the http://localhost origin cross-origin isolated,
// which makes SharedArrayBuffer available to the page; CORP: same-origin
// keeps COEP from blocking the page's own loopback-origin subresources),
// and - unless head is set (a HEAD request, which must not carry a body) -
// the body itself.
func (s *loopbackServer) writeResponse(conn net.Conn, status int, mime string, body []byte, head bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", status, statusText(status))
	fmt.Fprintf(&b, "Content-Type: %s\r\n", mime)
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	fmt.Fprintf(&b, "%s: %s\r\n", headerCOOP, valSameOrigin)
	fmt.Fprintf(&b, "%s: %s\r\n", headerCOEP, valRequireCorp)
	fmt.Fprintf(&b, "%s: %s\r\n", headerCORP, valSameOrigin)
	b.WriteString("Connection: close\r\n\r\n")
	_, _ = conn.Write([]byte(b.String()))
	if len(body) > 0 && !head {
		_, _ = conn.Write(body)
	}
}

// writeStatus writes a body-less status response (errors and "not found").
func (s *loopbackServer) writeStatus(conn net.Conn, status int) {
	s.writeResponse(conn, status, "text/plain; charset=utf-8", nil, false)
}

// statusText is the reason phrase for the few statuses the server emits.
func statusText(status int) string {
	switch status {
	case 200:
		return "OK"
	case 302:
		return "Found"
	case 400:
		return "Bad Request"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 421:
		return "Misdirected Request"
	default:
		return "Status " + strconv.Itoa(status)
	}
}
