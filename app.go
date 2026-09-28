// Package appkit is a pure-Go foundation for web-based desktop applications:
// it embeds the platform View (WKWebView on macOS, WebKitGTK on Linux,
// WebView2 on Windows) behind a single Go API and layers desktop services on
// top of it - windowing and app windows, drag regions, custom URL schemes,
// notifications, clipboard, single-instance, opening URLs and native file
// dialogs - all cgo-free.
//
// Source layout: the package is split into three file families. app*.go
// holds the application scope - the App type (configuration + runtime
// context), its app-scope methods (Show, Wait, Copy/Paste,
// Open/Reveal, Notify) and the per-OS app internals
// (single-instance, app icon); view*.go holds the view/window
// API surface (the define-first View struct and its methods, the geometry +
// State/Config types, scheme types, App.Show glue, the View Dialog method,
// drag regions); the binding/events machinery may be split further into
// bind.go (registry model + value conversion), bind_gen.go (the generated
// JS) and bind_evt.go (the events bridge) - see AGENTS.md "Source layout";
// lib*.go holds the pure per-platform engine layer
// (lib_{darwin,linux,windows}.go talk to WKWebView/WebKitGTK/WebView2 and the
// platform APIs).
//
// Platform code lives in *_unix.go / *_windows.go / *_darwin.go files; a
// capability that a platform cannot provide returns an Err* sentinel or is a
// documented best-effort no-op rather than failing at compile time.
package tuohi

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
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

	"github.com/atotto/clipboard"
	"github.com/terva-sh/tuohi/notify"
	"github.com/terva-sh/tuohi/tray"

	_ "embed"
)

//go:embed app.png

// _icon is the embedded appkit mark: the PNG bytes of the application icon
// (app.png). It is the DEFAULT process icon, unexported on purpose - when the
// consumer leaves App.Icon (or the tray's own Icon) unset, appkit applies
// _icon itself: setAppIcon receives it so every appkit app shows the appkit
// face unless it brings its own, and the tray glyph is downscaled from it.
var _icon []byte

// trayGlyphSize is the side length, in pixels, of the tray icon appkit
// derives from App.Icon when a configured tray leaves its own Icon unset.
const trayGlyphSize = 32

// resizePNG downscales a square PNG to size×size with a box filter and
// returns the result as PNG bytes, or nil when pngData is not a decodable
// image. The box average runs over straight (non-premultiplied) color, so
// translucent edges keep their hue. It is appkit's shared resizer for icon
// paths that need a smaller copy of a PNG - the tray glyph derived from
// App.Icon (or the embedded default) when the tray config sets no icon.
func resizePNG(pngData []byte, size int) []byte {
	if size <= 0 {
		return nil
	}
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil
	}
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, x1 := x*sb.Dx()/size, (x+1)*sb.Dx()/size
			y0, y1 := y*sb.Dy()/size, (y+1)*sb.Dy()/size
			if x1 <= x0 || y1 <= y0 { // size exceeds the source: degenerate box
				x1, y1 = x0+1, y0+1
			}
			var r, g, b, a uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r += uint64(c.R)
					g += uint64(c.G)
					b += uint64(c.B)
					a += uint64(c.A)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			if n == 0 {
				n = 1
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: uint8(a / n)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil
	}
	return buf.Bytes()
}

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

// envDebug reports whether APPKIT_DEBUG=1 override-enables the dev tools.
func envDebug() bool { return os.Getenv("APPKIT_DEBUG") == "1" }

// App configures an appkit application and carries its runtime scope.
//
// It is the single application-scoped object: the exported fields hold the
// application settings (content filesystem, tray, icon, single-instance, ...)
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
// All app-scoped services are methods on *App (Show, Wait, Copy, Open,
// Paste, Reveal, Notify, ...); App.Bind is a declarative map instead
// of a method - see its field doc. This is a deliberate design
// choice: the App scope is never hidden from the consumer.
type App struct {
	// Debug turns the platform web inspector / developer tools on for every
	// window of this app (the app-wide default for View.Debug): set it once
	// for "every window is debuggable". A view's own View.Debug ORs over it,
	// and the APPKIT_DEBUG=1 environment variable forces the tools on for
	// every view no matter what.
	//
	// Like every App field it is committed when the app scope opens (later
	// edits have no effect) and it is read exactly once per view, at window
	// creation. Backend mapping - WebView2 AreDevToolsEnabled (Windows),
	// WebKitGTK enable-developer-extras (Linux), WKPreferences
	// developerExtrasEnabled (macOS).
	Debug bool

	// Events names the JavaScript global the appkit events bridge installs on
	// every page of this app: window.<Events> with on/off/emit (see
	// View.On/Off/Emit). Empty (the default) uses the name "events". The name
	// is fixed when each view is created.
	Events string

	// ID uniquely identifies this application - e.g.
	// "com.github.malivvan.appkit" - the key the single-instance rule locks
	// on. It is optional outside Single Instance Mode: with App.Exec nil it is
	// unused and several instances of the same binary run side by side. When
	// App.Exec is set (Single Instance Mode is on), ID is REQUIRED: a later
	// launch of an app whose ID matches an already-running primary forwards
	// its command-line arguments to that primary (see Exec) and exits
	// quietly. Launching the binary with "--new-instance" in its arguments
	// always starts a fresh instance, even in single-instance mode.
	ID string

	// Exec, when non-nil, ENABLES Single Instance Mode: only one process of
	// this application runs at a time, and every later launch is redirected to
	// it instead of starting a new process. App.ID must then uniquely identify
	// the application (see the ID doc). With Exec nil (the default),
	// single-instance mode is off and every launch runs its own process.
	//
	// Exec is invoked on the primary instance with the command-line arguments
	// of each redirected later launch. It runs on its own goroutine, so hand
	// the arguments to the UI thread if you touch UI state.
	Exec func(args []string)

	// Name is the application name, used where the OS asks for one - most
	// visibly as the source shown by desktop notifications (App.Notify).
	Name string

	// Icon is a PNG image for the running application, applied on a
	// best-effort basis wherever the platform supports it at runtime (macOS
	// Dock, Linux GTK3/GTK4 window icons, ...). It is applied once, when the
	// app scope opens and before the first window exists. When Icon is unset
	// the embedded appkit mark is used instead (unexported; appkit applies it
	// itself), so an appkit application always has a process face unless it
	// brings its own. App.Icon is ALSO the source of the tray glyph when the
	// app configures a tray (App.Tray) whose own Icon is unset: appkit
	// downscales App.Icon (or the embedded mark) to a tray-sized PNG at app
	// init. Unlike the per-window page icon it sets the face of the PROCESS; a
	// stable, runtime process icon is intentionally a best-effort feature
	// because it is hard to keep identical across all platforms. An unset icon
	// is silently ignored; so are environments that cannot take a runtime icon
	// (Windows reads the icon from the executable's own resources). Under an
	// X11 window manager the icon is pushed per window; under Wayland, where
	// the protocol has no per-window icons, GTK4 (>= 4.20) sends pixels via
	// the xdg-toplevel-icon protocol, and the GTK3 stack installs a matching
	// per-user .desktop entry and themed icon keyed to App.Name so the
	// compositor's app_id lookup finds it. A PNG that cannot be decoded is
	// never fatal.
	Icon []byte

	// Bind holds the application's declarative bindings: every entry is bound
	// onto each view App.Show creates, so one entry here covers all windows.
	// A key is a DOTTED path - dots separate nested variables on the page, so
	// a value bound at "app.someAPI.call" appears as window.app.someAPI.call.
	// What a value becomes is decided by its kind alone:
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
	//     the setter (`window.name = v`) - see makeAccessorBinding;
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
	// A nil entry binds nothing. A view may override an app-wide name - or
	// unbind it with a nil entry - through its own View.Bind map.
	//
	// Entries are applied to every view deterministically: in alphabetical
	// key order, before the view's own View.Bind entries (see App.Show), so
	// the result never depends on Go's map iteration order.
	//
	// Like every App field it is committed when the app scope opens (later
	// edits have no effect) and is read once per shown view, at window
	// creation.
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
	// - and appkit serves the file at that path in the filesystem on every
	// platform. The serving is scheme-first on Windows and Linux: WebView2's
	// https vhost for the custom "app" scheme (whose responses carry the
	// isolation headers), the registered custom scheme on Linux (WebKitGTK
	// cannot attach the headers to scheme responses, so a scheme-served Linux
	// page is not crossOriginIsolated - SharedArrayBuffer still works through
	// the JSC_useSharedArrayBuffer option). macOS always serves over a
	// temporary loopback http://localhost server (WKWebView cannot make a
	// custom scheme a secure context, and a long-standing WebKit bug keeps
	// SharedArrayBuffer off plain WKWebView pages); App.HTTP opts Linux and
	// Windows into that same loopback origin. SharedArrayBuffer is available
	// on every platform. A path without a file answers "not found". A nil FS serves
	// no content - the window shows whatever the consumer navigates it to
	// itself.
	FS fs.FS

	// HTTP serves the app's content over a TEMPORARY loopback http://localhost
	// server instead of the platform's custom "app" scheme. On Linux and
	// Windows it is an opt-in fallback - the native scheme serves them (see
	// the FS doc), so only HTTP-opted windows load their app:// content from
	// the temporary loopback origin; the server is torn down again once the
	// first page load finishes. macOS always serves over the loopback origin
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

	// Tray optionally puts an icon with a menu in the system tray / menu bar
	// for the life of the application. When set, the icon appears as App.Wait
	// starts running the platform UI loop (that loop dispatches the tray's
	// menu events) and is removed when Wait returns. It is the app-scoped,
	// declarative form of the tray subpackage's Set/Remove pair; see the tray
	// package for the full API.
	//
	// The Config is read once when the app scope opens - icon (PNG, plus
	// dark-mode and macOS template variants), tooltip, tray-level click
	// handlers and the whole menu tree (checkboxes, submenus, separators,
	// per-item icons). When the config leaves its Icon unset, appkit derives
	// the tray glyph at app init from App.Icon - falling back to the embedded
	// appkit mark - downscaled to a tray-sized PNG (see the Icon doc). The
	// tray is a launcher, not a live dashboard: the menu
	// stays fixed for its lifetime, so there is no runtime update machinery.
	//
	// A menu item's OnClick runs on the UI thread; keep it short or hand the
	// work to a goroutine. A typical use ends the app from the menu:
	//
	//	Tray: &tray.Config{
	//		Icon:    iconPNG,
	//		Tooltip: "my app",
	//		Items: []tray.Item{
	//			{Label: "Open", OnClick: openUI},
	//			{Label: "Quit", OnClick: app.Quit},
	//		},
	//	}
	//
	// On macOS the tray switches the application to the accessory (menu-bar)
	// activation policy, so the app's Dock icon disappears while the tray is
	// up; that is the tray package's behavior for standalone use too. A
	// configured tray that cannot be created (for example a second tray in
	// the same process) makes Wait fail with the tray package's error.
	Tray *tray.Config

	// --- internal scope state; see the App doc ---
	scopeOnce sync.Once
	scope     *appScope
}

// appConfig is a plain snapshot of an App's exported settings, taken exactly
// once when the app scope is opened. It carries no synchronization state so
// it can be passed by value freely.
type appConfig struct {
	Name   string
	Exit   bool
	Tray   *tray.Config
	Icon   []byte
	ID     string
	Exec   func(args []string)
	FS     fs.FS
	HTTP   bool
	Debug  bool
	Events string
	Bind   map[string]any
}

// snapshotConfig copies an App's exported settings into a plain appConfig.
// Debug commits App.Debug OR the APPKIT_DEBUG=1 environment override (the
// environment variable forces the dev tools on for every view).
func snapshotConfig(a *App) appConfig {
	return appConfig{
		Name:   a.Name,
		Exit:   a.Exit,
		Tray:   a.Tray,
		Icon:   a.Icon,
		ID:     a.ID,
		Exec:   a.Exec,
		FS:     a.FS,
		HTTP:   a.HTTP,
		Debug:  a.Debug || envDebug(),
		Events: a.Events,
		Bind:   a.Bind,
	}
}

// appScope is the committed configuration of an App plus the state of the
// app scope. It is created exactly once, on the first App method call.
type appScope struct {
	cfg     appConfig // committed settings snapshot
	initErr error

	// Lifecycle state. startOnce guards the one-time app start (single
	// instance + icon, see (*App).start); windows counts the
	// live windows created through App.Show (owned windows only);
	// exitOnce/exitFlag end the Wait run loop (App.Quit, or the last window
	// closing when App.Exit is set). No app-scope web server exists: the
	// loopback servers (macOS always, Linux and Windows under App.HTTP) are
	// per-view, temporary, and owned by the engine (see viewContentBase).
	startOnce   sync.Once
	startErr    error
	releaseInst func()
	windows     int32
	exitOnce    sync.Once
	exitFlag    int32

	// views is the set of Views currently shown (and managed) by this App.
	// App.Show registers a View the first time it creates its window and
	// View.Close unregisters it, so Show(view) on a live window knows to
	// reveal instead of recreating.
	viewsMu sync.Mutex
	views   map[*View]bool
}

// registerView records v as a window this App manages (called by App.Show).
func (a *App) registerView(v *View) {
	if a == nil || v == nil {
		return
	}
	s := a.scope
	if s == nil {
		return
	}
	s.viewsMu.Lock()
	if s.views == nil {
		s.views = make(map[*View]bool)
	}
	s.views[v] = true
	s.viewsMu.Unlock()
}

// unregisterView drops v from the managed set (called by View.Close).
func (a *App) unregisterView(v *View) {
	if a == nil || v == nil {
		return
	}
	s := a.scope
	if s == nil {
		return
	}
	s.viewsMu.Lock()
	delete(s.views, v)
	s.viewsMu.Unlock()
}

// begin commits the App's current settings (later field edits are ignored)
// and performs the one-time platform initialization, returning the committed
// scope. Every App method calls begin before running its action.
func (a *App) begin() (*appScope, error) {
	if a == nil {
		return nil, errors.New("appkit: nil *App")
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
// that calls Wait (the main goroutine). A simple single-window app may skip
// Wait and call View.Run on its window instead; the two models must not be
// mixed.
func (a *App) Wait() error {
	s, err := a.begin()
	if err != nil {
		return fmt.Errorf("appkit: wait: %w", err)
	}
	if err := a.start(s); err != nil {
		return err
	}
	// A configured tray shows its icon before the loop runs - Wait runs on the
	// UI thread, which is where tray.Set must be called - and the loop below
	// dispatches the tray's menu events. Remove hides the icon when the loop
	// ends; it leaves the (now-finished) loop alone and is safe to call on
	// every platform.
	if s.cfg.Tray != nil {
		if err := tray.Set(*s.cfg.Tray); err != nil {
			return fmt.Errorf("appkit: tray: %w", err)
		}
		defer tray.Remove()
	}
	ui.enterLoop()
	for atomic.LoadInt32(&s.exitFlag) == 0 {
		appUIWait()
	}
	ui.exitLoop()
	if s.releaseInst != nil {
		s.releaseInst()
		s.releaseInst = nil
	}
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

// start performs the one-time application initialization: the single-instance
// rule (enabled by App.Exec, keyed on App.ID) and the best-effort runtime
// icon (App.Icon). It runs once - on the first window creation or the first
// Wait call - which is the single "app initialization" point of the scope;
// later calls are no-ops. App.Show and Wait both call it, so the order does
// not matter. Content serving is NOT started here: there is no permanently
// listening web server. App.FS is served per view - through WebView2's https
// vhost on Windows, or over a TEMPORARY per-view loopback server (macOS and
// Linux always; Windows when App.HTTP opts in), started at window creation
// and stopped once the window has been loaded (see viewContentBase).
func (a *App) start(s *appScope) error {
	s.startOnce.Do(func() {
		release, err := enforceSingleInstance(&s.cfg)
		if err != nil {
			s.startErr = err
			return
		}
		s.releaseInst = release
		// The process icon: App.Icon when the consumer set one, otherwise the
		// embedded appkit mark (_icon). setAppIcon is best-effort per platform
		// (Dock on macOS, GTK window icons on Linux, no-op on Windows) and its
		// errors are deliberately ignored - an un-decodable PNG must never
		// keep the application from starting.
		icon := s.cfg.Icon
		if len(icon) == 0 {
			icon = _icon
		}
		_ = setAppIcon(icon, s.cfg.Name)
		// Tray icon default: when the app configures a tray but leaves the
		// tray's own Icon unset, appkit derives the tray glyph from the same
		// process icon (App.Icon, falling back to the embedded mark) and
		// downscales it to trayGlyphSize - the resize that used to be
		// hand-written in the showcase. The derived icon goes on a COPY of
		// the config, so the consumer's tray.Config is left untouched, and a
		// PNG that cannot be decoded simply leaves the tray icon unset.
		if s.cfg.Tray != nil && len(s.cfg.Tray.Icon) == 0 {
			if glyph := resizePNG(icon, trayGlyphSize); len(glyph) > 0 {
				cp := *s.cfg.Tray
				cp.Icon = glyph
				s.cfg.Tray = &cp
			}
		}
	})
	return s.startErr
}

// scopePtr points at the currently active App scope so the per-OS engines can
// report window close events into it (appWindowClosed). One live application
// per process is the supported model (matching the single-instance lock).
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

// Notify displays an OS-level notification with the given title and message,
// named after the app (App.Name). It delegates to the notify subpackage,
// which needs no window and no tray icon: each platform binds the
// notification service the OS ships - NSUserNotificationCenter on macOS, a
// Shell_NotifyIconW balloon on Windows, org.freedesktop.Notifications on
// Linux. The App method stays plain (title + message only, no options); for
// custom icons, urgency or an alert/beep use the notify subpackage's
// ShowOpts/Alert/Beep directly.
//
// It is safe to call from any goroutine once the App scope is open and
// returns the notify package's ErrUnsupported on platforms without a backend
// (anything but macOS, Windows and Linux). On macOS it can return
// ErrUnavailable when the process has no notification center - the deprecated
// NSUserNotificationCenter needs a bundled .app the user granted Notification
// permission (see the notify package docs); check with errors.Is.
func (a *App) Notify(title, message string) error {
	s, err := a.begin()
	if err != nil {
		return err
	}
	return notify.Show(s.cfg.Name, title, message)
}

// Copy puts b onto the system clipboard, replacing whatever was there. It is
// an App method over github.com/atotto/clipboard (which reads the platform
// clipboard through pbcopy/pbpaste on macOS, xclip/xsel/wl-copy on Linux and
// the Win32 API on Windows). Safe to call from any goroutine once the App
// scope is open.
//
// The platform backends are text clipboards, so Copy is binary-safe only
// opportunistically: arbitrary bytes round-trip where the backend preserves
// them verbatim (the command-line tools treat the payload as opaque), while
// Windows maps the payload through its text clipboard and may not preserve
// non-text bytes. It returns an error when no clipboard backend is available
// (for example a headless Linux box without xclip/xsel/wl-copy).
func (a *App) Copy(b []byte) error {
	if _, err := a.begin(); err != nil {
		return err
	}
	return clipboard.WriteAll(string(b))
}

// Paste returns the current clipboard content as raw bytes. Text copied from
// other applications arrives as its UTF-8 encoding. An empty clipboard yields
// an empty slice with a nil error; an error is returned only when no
// clipboard backend is available.
func (a *App) Paste() ([]byte, error) {
	if _, err := a.begin(); err != nil {
		return nil, err
	}
	s, err := clipboard.ReadAll()
	return []byte(s), err
}

// Backend reports which web-engine backend the App scope uses for its views,
// after the one-time platform initialization has run:
//
//   - "webkitgtk-6.0" or "webkit2gtk-4.1" on Linux - the stack that was
//     actually loaded, honoring the APPKIT_BACKEND environment variable
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

// errAlreadyRunning is returned by the internal single-instance machinery when
// another process holds the lock for the requested id.
var errAlreadyRunning = errors.New("appkit: another instance is already running")

// instanceLock is a held single-instance lock. Single-instance handling is
// deliberately exposed only through App (Exec / ID) - there is no
// public manual API. Release relinquishes the lock and stops listening for
// hand-offs; it is safe to call more than once (later calls are no-ops
// returning the first result).
type instanceLock struct {
	once    sync.Once
	relErr  error
	release func() error
}

// Release relinquishes the lock. See instanceLock.
func (l *instanceLock) Release() error {
	l.once.Do(func() { l.relErr = l.release() })
	return l.relErr
}

// acquireInstance tries to become the single instance identified by id (a
// stable application identifier such as "com.example.app"). It returns the
// owning lock when no instance is running, or errAlreadyRunning when one
// already is. onMessage, when non-nil, receives the arguments of later
// launches that were redirected to this primary.
func acquireInstance(id string, onMessage func([]string)) (*instanceLock, error) {
	return acquire(id, onMessage)
}

// sendInstance delivers args to the instance already running under id, for use
// after acquireInstance returned errAlreadyRunning. It returns an error when no
// instance is listening.
func sendInstance(id string, args []string) error { return send(id, args) }

// instanceKey derives a short, filesystem- and pipe-name-safe key from an arbitrary
// id, so the lock/socket/pipe names stay bounded in length and collision-free.
func instanceKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

// enforceSingleInstance applies the single-instance rule. It is only active
// when App.Exec is set (a non-nil Exec enables Single Instance Mode): with a
// nil Exec every launch runs its own process. When active, App.ID names the
// application the rule locks on - with Exec set and an empty ID it returns an
// error, because a single instance needs a stable identifier to lock. When
// active, this process becomes the primary instance (its Release is returned
// so the caller defers it); a second launch forwards its arguments to the
// primary and terminates the process quietly (os.Exit(0)). The rule is also
// skipped when the binary was started with the "--new-instance" override flag
// (which always starts a fresh instance) or when this process already holds
// the primary lock.
func enforceSingleInstance(opts *appConfig) (func(), error) {
	if opts.Exec == nil || hasNewInstanceFlag(os.Args) {
		return func() {}, nil
	}
	id := opts.ID
	if id == "" {
		return nil, errors.New("appkit: App.ID is required when App.Exec enables single-instance mode")
	}
	if instanceHeld() {
		return func() {}, nil
	}
	inst, err := acquireInstance(id, opts.Exec)
	if errors.Is(err, errAlreadyRunning) {
		// Another instance owns the lock: hand it our arguments and go away.
		_ = sendInstance(id, os.Args[1:])
		os.Exit(0)
	}
	if err != nil {
		return nil, err
	}
	setInstanceHeld(true)
	return func() {
		setInstanceHeld(false)
		_ = inst.Release()
	}, nil
}

// hasNewInstanceFlag reports whether the process was started with the
// "--new-instance" override, which always starts a fresh instance.
func hasNewInstanceFlag(args []string) bool {
	for _, a := range args {
		if a == "--new-instance" {
			return true
		}
	}
	return false
}

var (
	instanceMu  sync.Mutex
	primaryHeld bool
)

func instanceHeld() bool {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	return primaryHeld
}

func setInstanceHeld(v bool) {
	instanceMu.Lock()
	primaryHeld = v
	instanceMu.Unlock()
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
// single live-install Eval - instead of one full rebuild per name (P1).
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
		return nil, fmt.Errorf("appkit: Bind requires a non-nil View")
	}
	if v == nil {
		return nil, fmt.Errorf("appkit: Bind requires a non-nil value")
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
// two names silently destroy each other on the page (R2/RE2/E4).
func applyBinds(w binder, appBinds, viewBinds map[string]any) error {
	binds, unbinds, err := planBinds(appBinds, viewBinds, eventsGlobalOf(w))
	if err != nil {
		return err
	}
	// Engine bindings register and install in ONE batch pass (P1); stubs
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
			return fmt.Errorf("appkit: unbinding %s: engine cannot unbind", name)
		}
		if err := u.Unbind(name); err != nil {
			return fmt.Errorf("appkit: unbinding %s: %w", name, err)
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
	// final is the set of names that end up bound (R2's collision domain).
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
// often appkit's injected scripts) with no error anywhere (E4). The check
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

// validateTopLevel rejects bind names that would clobber appkit's own page
// surface or a common window global: the first dot-segment of the name must
// not equal the events API global of this view (window.<eventsGlobal>), must
// not be "__webview__" (the bridge instance) and must not start with
// "__appkit" (every internal message method and the events binding live
// there, see the internal* constants), and must not be a denylisted window
// built-in (windowGlobalDenylist). Deeper segments are not restricted: they
// live under the consumer's own namespace objects.
func validateTopLevel(name, eventsGlobal string) error {
	top := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		top = name[:i]
	}
	if top == "__webview__" || strings.HasPrefix(top, "__appkit") {
		return fmt.Errorf("appkit: binding name %q is reserved for appkit's internal page API", name)
	}
	if top == eventsGlobal {
		return fmt.Errorf("appkit: binding name %q would replace the page's events API (window.%s)", name, eventsGlobal)
	}
	if windowGlobalDenylist[top] {
		return fmt.Errorf("appkit: binding name %q would replace the page's own window.%s", name, top)
	}
	return nil
}

// checkDottedPrefixes reports an error when any two final binding names are
// in a dotted-prefix relationship - one name is a segment-wise prefix of
// another ("api" vs "api.id", "app.x" vs "app.x.y"). The page installer
// creates namespace objects for dotted names, so binding both a leaf and a
// namespace under it is order-dependent and one of the two silently destroys
// the other (R2); validating the whole final set up front makes the failure
// loud, deterministic and independent of Go's map order.
func checkDottedPrefixes(names map[string]bool) error {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if strings.HasPrefix(keys[i], keys[i-1]+".") {
			return fmt.Errorf("appkit: binding names %q and %q collide: %q is nested under %q, and a leaf and its namespace cannot both be bound", keys[i], keys[i-1], keys[i], keys[i-1])
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
// App.Show reads it (RE3). The declarative bind maps are shared, unlocked Go
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
		return fmt.Errorf("appkit: resolve %q: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("appkit: reveal %q: %w", path, err)
	}
	return revealFile(abs)
}

// validateScheme enforces the Open allow-list. url.Parse normalizes the scheme
// to lower case, so the comparison is already case-insensitive.
func validateScheme(rawurl string) error {
	u, err := url.Parse(rawurl)
	if err != nil {
		return fmt.Errorf("appkit: parse %q: %w", rawurl, err)
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

// ErrScheme is returned by Open when the URL's scheme is not in the allow-list.
var ErrScheme = errors.New("appkit: refused URL scheme")

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
// unchanged. base is the http://localhost:<port> origin of a view's
// temporary loopback server; an empty base leaves app:// URLs untouched
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
// onto (see App.FS / App.HTTP). Loopback servers are always TEMPORARY -
// there is no permanently listening web server - so a view is either served
// through the engine's custom "app" scheme or, while its first page loads,
// over its own temporary loopback server. forceLoopback makes the view use
// the loopback origin unconditionally (macOS always does - WKWebView cannot
// make a custom scheme a secure context, and a long-standing WebKit bug
// keeps SharedArrayBuffer off plain WKWebView pages); without it, Linux and
// Windows use the loopback origin only when App.HTTP opts in (their native
// scheme serving is enough otherwise - the https vhost on Windows, the
// registered custom scheme on Linux). The caller owns the server and
// MUST stop it (stopLoopback) once the view's first load has finished - or
// when the view closes before any load.
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

// startViewServer starts the temporary loopback server that serves App.FS to
// ONE view (see viewContentBase / App.HTTP). It returns nil when App.FS is nil.
func (s *appScope) startViewServer() (*loopbackServer, error) {
	if s.cfg.FS == nil {
		return nil, nil
	}
	srv, _, err := listenLoopbackHTTP(serveAppFS(s.cfg.FS))
	if err != nil {
		return nil, fmt.Errorf("appkit: serve App.FS for a view: %w", err)
	}
	return srv, nil
}

// stopLoopback shuts a temporary per-view loopback server down. It is a
// no-op for a nil server and safe to call more than once (fireReady and
// Destroy both stop it).
func stopLoopback(srv *loopbackServer) {
	if srv == nil {
		return
	}
	_ = srv.Close()
}

// loopbackServer is a temporary per-view loopback HTTP server created by
// listenLoopbackHTTP. It serves ONE view's content while that view's first
// page loads, lives until the load finished (or the view closed), and is
// owned by the engine. Every response carries the cross-origin-isolation
// headers (COOP/COEP + CORP, see writeResponse), so the http://localhost
// origin is a secure, cross-origin-isolated context with SharedArrayBuffer
// available.
type loopbackServer struct {
	ln   net.Listener
	base string // base URL of the served origin, e.g. "http://localhost:41234"

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	timer  *time.Timer // idle timeout: closes the server when no request arrives
}

// loopbackIdleTimeout is how long a temporary per-view loopback server stays
// up without serving a request. The page's initial load (HTML + CSS + JS and
// any other subresource) keeps issuing requests, each resetting the timer;
// once the page is fully loaded no further requests arrive, the timeout runs
// out and the server shuts itself down. 3 seconds covers a page's whole load
// (even late stylesheet fetches) while still tearing the server down quickly
// after the load settles.
const loopbackIdleTimeout = 3 * time.Second

// listenLoopbackHTTP binds a LOOPBACK TCP listener and serves serve on it.
// It is the constructor of every (temporary, per-view) loopback server.
// Servers always bind the loopback default "127.0.0.1:0" - a free loopback
// port; there is no permanently listening or exposed web server.
//
// A temporary server shuts itself down after loopbackIdleTimeout without a
// request (each request resets the timer), so it serves exactly the page's
// initial load and then goes away - no engine-side "page loaded" signal is
// involved. Call loopbackServer.Close to tear it down sooner.
//
// It returns the running server plus the base URL pages navigate to - always
// the http://localhost form of the bound port, because that origin is a
// secure context in the embedded web view. "localhost" resolves to the
// loopback address actually bound, so the advertised URL always reaches this
// server.
func listenLoopbackHTTP(serve serveFunc) (*loopbackServer, string, error) {
	if serve == nil {
		return nil, "", errors.New("loopback server: nil resolver")
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
		base:  fmt.Sprintf("http://localhost:%d", tcpAddr.Port),
		conns: make(map[net.Conn]struct{}),
	}
	s.mu.Lock()
	s.timer = time.AfterFunc(loopbackIdleTimeout, func() { _ = s.Close() })
	s.mu.Unlock()
	go s.serve(serve)
	return s, s.base, nil
}

// keepAlive resets the idle timeout because a request arrived: while the
// page is still loading it keeps fetching (each request re-arms the timer),
// and once the load settles no request arrives, the timer fires and Close
// shuts the server down. Guarded by s.mu so it never races Close's stop.
func (s *loopbackServer) keepAlive() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Reset(loopbackIdleTimeout)
	}
	s.mu.Unlock()
}

// isClosed reports whether the server has been shut down (an idle timeout or
// an explicit Close). Engines use it to notice that a temporary server has
// expired and drop their per-view rewrite base.
func (s *loopbackServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// loopbackListen binds a loopback TCP listener on a free loopback port
// (127.0.0.1:0). Only loopback is accepted - the served resolver is the app's
// own UI, and loopback servers are deliberately temporary and never exposed -
// so any other address is refused with an error.
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

// Close stops the server: the idle timer is stopped, the listener closes (no
// new connections) and every in-flight connection is shut down. It is a
// no-op once the server is closed and safe to call more than once (later
// calls return nil).
func (s *loopbackServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
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
// to get wrong. Every request also re-arms the idle timeout (keepAlive): the
// server lives as long as the page keeps fetching, and shuts itself down
// once no request has arrived for loopbackIdleTimeout.
func (s *loopbackServer) serveConn(conn net.Conn, serve serveFunc) {
	s.keepAlive()
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
			if name == "host" && host == "" {
				host = strings.TrimSpace(value)
			}
		}
	}
	if method != "GET" && method != "HEAD" {
		s.writeStatus(conn, 405)
		return
	}
	if host == "" {
		host = "localhost"
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
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	default:
		return "Status " + strconv.Itoa(status)
	}
}

// --- Autostart -------------------------------------

// App-scope autostart service: App.Autostart() hands out an *Autostart that
// registers the application to launch when the user logs in. The mechanism is
// per platform (a .desktop file under XDG autostart, an HKCU Run registry
// value, a launchd LaunchAgent plist or - for bundled macOS 13+ apps -
// SMAppService); every registration points at the running executable and
// takes effect on the next login, not immediately.

// Autostart backend names, reported by Autostart.Backend for an existing
// registration (empty when nothing is registered).
const (
	autostartBackendSMAppService = "smappservice"  // macOS 13+, bundled .app
	autostartBackendLaunchAgent  = "launchagent"   // macOS ~/Library/LaunchAgents plist
	autostartBackendRegistryRun  = "registry-run"  // Windows HKCU …\Run value
	autostartBackendXDGAutostart = "xdg-autostart" // freedesktop .desktop autostart
)

// ErrAutostartNotSupported is returned by Autostart methods when the current
// platform has no autostart backend.
var ErrAutostartNotSupported = errors.New("appkit: autostart is not supported on this platform")

// Autostart controls whether the application starts at user login. Get it
// from App.Autostart.
//
// A registration points at the running executable and takes effect on the
// next login, not immediately. Re-enabling overwrites the registration; each
// platform keeps at most one entry per executable, so a previous registration
// under a different identifier (e.g. after App.Name changed) is replaced, not
// duplicated.
//
// Enabled, Path and Backend report the CURRENT registration: they scan
// the platform's registration store for an entry whose command is the running
// executable, so they work regardless of the identifier used at Enable time.
type Autostart struct {
	cfg  appConfig // committed App settings: ID and Name are the identifier source
	impl autostartBackend
}

// autostartBackend is one platform's registration mechanism. Implementations
// live in the per-OS app_autostart files.
type autostartBackend interface {
	// enable registers the running executable under the given identifier
	// with the given login arguments.
	enable(id string, args []string) error
	// disable removes the registration for the running executable, if any.
	disable() error
	// status reports whether a registration for the running executable
	// exists, the path of its artefact (registry sub-key path on Windows,
	// the bundle identifier for SMAppService) and the backend name.
	status() (enabled bool, path, backend string)
}

// Autostart returns the application's autostart controller. The identifier a
// registration is stored under derives from App.ID when set, otherwise from a
// filesystem-safe slug of App.Name (or of the executable name when App.Name
// is empty) - see Autostart.Enable.
func (a *App) Autostart() *Autostart {
	cfg := snapshotConfig(a)
	return &Autostart{cfg: cfg, impl: newAutostartBackend(cfg)}
}

// Enabled reports whether a registration for the running executable exists.
// It does not verify that a registered entry still points at the running
// binary; Disable and Enable always reconcile that themselves.
func (a *Autostart) Enabled() bool {
	if a == nil || a.impl == nil {
		return false
	}
	enabled, _, _ := a.impl.status()
	return enabled
}

// Enable registers the application to launch at login with the given command
// line arguments (appended after the executable path). It is safe to call
// repeatedly: an existing registration is overwritten, and a stale entry
// pointing at this executable under a different identifier is removed first.
//
// The registration identifier is App.ID when set; otherwise a slug of
// App.Name, or of the executable name when App.Name is empty. An App.ID
// containing characters outside A-Za-z0-9._- is rejected.
func (a *Autostart) Enable(args ...string) error {
	if a == nil || a.impl == nil {
		return ErrAutostartNotSupported
	}
	id, err := autostartIdentifier(a.cfg)
	if err != nil {
		return err
	}
	return a.impl.enable(id, args)
}

// Disable removes the autostart registration for the running executable. It
// is a no-op (nil error) when nothing is registered.
func (a *Autostart) Disable() error {
	if a == nil || a.impl == nil {
		return ErrAutostartNotSupported
	}
	return a.impl.disable()
}

// Path returns the path of the registration artefact for the current
// registration: the .desktop file path on Linux, the registry sub-key path
// (HKCU\…\Run\<id>) on Windows, the LaunchAgent plist path on macOS, or the
// bundle identifier for an SMAppService registration. Empty when nothing is
// registered.
func (a *Autostart) Path() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, path, _ := a.impl.status()
	return path
}

// Backend returns the name of the mechanism the current registration uses:
// "xdg-autostart", "registry-run", "launchagent" or "smappservice". Empty
// when nothing is registered.
func (a *Autostart) Backend() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, _, backend := a.impl.status()
	return backend
}

// autostartIdentifier derives the registration identifier from the committed
// App settings: App.ID verbatim (validated), else a slug of App.Name, else a
// slug of the executable's base name, else the built-in fallback.
func autostartIdentifier(cfg appConfig) (string, error) {
	if cfg.ID != "" {
		if err := validateAutostartIdentifier(cfg.ID); err != nil {
			return "", err
		}
		return cfg.ID, nil
	}
	if cfg.Name != "" {
		return autostartSlug(cfg.Name), nil
	}
	if exe, err := os.Executable(); err == nil {
		if id := autostartSlug(filepath.Base(exe)); id != "" && id != defaultAutostartSlug {
			return id, nil
		}
	}
	return defaultAutostartSlug, nil
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// defaultAutostartSlug is the registration identifier used when the app has
// neither App.ID nor App.Name and the executable name slugifies to nothing.
const defaultAutostartSlug = "appkit-app"

// autostartSlug turns a free-form application name into something usable as
// the basename of a registration artefact (and as a launchd label): ASCII
// letters, digits, '.', '_' and '-', lower-cased, with whitespace collapsing
// to '-'. It never returns an empty string for non-empty input; the caller
// guarantees non-empty input.
func autostartSlug(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r == ' ', r == '\t':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return defaultAutostartSlug
	}
	return out
}

// validateAutostartIdentifier rejects identifiers that would be unsafe as a
// filename, registry value name or launchd Label.
func validateAutostartIdentifier(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > 200 {
		return fmt.Errorf("appkit: autostart identifier too long (max 200): %q", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("appkit: autostart identifier %q contains invalid character %q (allowed: A-Za-z0-9._-)", id, r)
		}
	}
	return nil
}

// resolvedExecutable returns os.Executable() after resolving symlinks, so
// registrations don't break when the binary is installed through a symlink
// farm (Homebrew, Scoop). Falls back to the unresolved path when symlink
// resolution fails.
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("appkit: autostart: get executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// writeFileAtomic writes data to path via a temp file + rename in the same
// directory, so a partial write never leaves a half-formed plist or .desktop
// file in place.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	// os.File.Write reports short writes as errors, but double-check n ==
	// len(data) so a future change of writer type cannot silently rename a
	// truncated artefact into place.
	n, err := tmp.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
