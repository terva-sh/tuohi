// Command demo is the single appkit showcase application.
//
// It replaces the old per-feature demos with ONE app that exercises the whole
// main package behind a single, borderless, cross-platform window, and doubles
// as the UI-automation target for the project:
//
//   - every interactive control has a stable id (see assets/index.html), so an
//     external driver (or the built-in self test) can find it deterministically;
//   - loading the page with "#selftest" runs a scripted suite whose verdicts
//     are reported back to Go (see main and assets/app.js);
//   - because the window is frameless and the chrome is drawn by the page
//     itself, the app looks (almost) the same on macOS, Windows and Linux.
//
// Run modes:
//
//	./demo                 windowed showcase - the page is the embedded UI,
//	                       served by App.FS from the uniform "app://" origin
//	                       on every platform.
//	./demo --framed        keep the OS window frame instead of the custom chrome
//	./demo --selftest      showcase + automated self test, exit 0/1
//	./demo -http           serve the window's app:// content over a temporary
//	                       loopback http://localhost server (App.HTTP) instead
//	                       of the native app scheme - an opt-in on Linux and
//	                       Windows; macOS always serves that way.
//	./demo -tray           windowed showcase + a tray menu (Show / Hide /
//	                       Quit) next to the window, exercising View.Show /
//	                       View.Hide. Opt-in: off by default so windowed runs
//	                       keep their Dock/taskbar icon (a tray configures the
//	                       macOS app as a menu-bar "accessory", which hides the
//	                       Dock icon). Ignored under --selftest, which runs
//	                       headless with no tray host.
//
// How the showcase is served: the demo sets ONE app-scoped App.FS (the
// embedded UI) and navigates the window to the uniform "app://" origin. The
// content is served scheme-first on Linux and Windows (the custom "app"
// scheme; Windows' https vhost carries the isolation headers, WebKitGTK's
// scheme responses cannot - Linux pages are still SharedArrayBuffer-capable
// via the JSC option), and macOS always serves over a TEMPORARY loopback
// http://localhost server (WKWebView cannot make a custom scheme a secure
// context and long-term cannot provide SharedArrayBuffer on plain pages).
// -http (App.HTTP) opts Linux and Windows into that loopback origin too,
// whose responses carry the isolation headers. Same JS bridge, events,
// chrome and self test on every platform, from one URL.
//
// The demo exercises the application lifecycle: the window is created with
// App.Show (all geometry comes from the View's Left/Top/Width/Height/State
// fields), then App.Wait runs the
// UI loop until the window closes or App.Quit is called (close button /
// self test).
// The demo window needs the platform view (WebKitGTK on Linux, WebView2 on
// Windows, WKWebView on macOS); on a headless box run it under xvfb-run.
// Every start logs the web-engine backend in use (appkit's App.Backend), so
// the run mode and the APPKIT_BACKEND override are visible at a glance.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/terva-sh/tuohi"
	"github.com/terva-sh/tuohi/dialog"
	"github.com/terva-sh/tuohi/tray"
)

// assetsFS embeds the single-page UI (index.html + app.css + app.js). The
// same tree is the demo's App.FS - appkit serves it from the uniform app://
// origin on every platform (see App.FS).
//
//go:embed assets
var assetsFS embed.FS

func assetsRoot() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err) // embed layout bug: the assets directory must exist
	}
	return sub
}

// windowDemo carries the state shared by the JS-bridge handlers: the App and
// the View (for dialogs, exit and the per-view events bridge), plus the
// self-test plumbing.
type windowDemo struct {
	w     *tuohi.View
	app   *tuohi.App
	self  *selfTest
	close chan struct{} // closed once the page asks to quit
}

type selfTest struct {
	active  bool
	ready   chan struct{}
	done    chan struct{} // closed when the page delivered its report
	reports chan []testReport
}

type testReport struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// autostartInfo is the JSON payload the page reads to render the Autostart
// section (see App.Autostart).
type autostartInfo struct {
	Enabled bool   `json:"enabled"`
	Backend string `json:"backend"`
	Path    string `json:"path"`
}

func main() {
	var (
		framed   = flag.Bool("framed", false, "use the OS window frame instead of the custom borderless chrome")
		debug    = flag.Bool("debug", false, "open the platform web inspector / dev tools")
		selftest = flag.Bool("selftest", false, "windowed showcase that runs the UI self test and exits 0/1")
		httpFn   = flag.Bool("http", false, "serve the window's app:// content over a temporary loopback HTTP server (App.HTTP); Linux/Windows opt in, macOS always does")
		trayFn   = flag.Bool("tray", false, "add a tray menu (Show/Hide/Quit) to the windowed showcase")
	)
	flag.Parse()

	// -debug is accepted for command-line compatibility; the showcase keeps
	// the inspector on regardless (Debug: true on the View below) so the demo
	// can always be inspected.
	_ = debug

	// Exit true: closing the window (or App.Quit) ends the demo process.
	// App.Icon is deliberately left unset: the process face (Dock tile on
	// macOS, GTK window icon on Linux) is appkit's embedded default mark. The
	// tray's own Icon below is left unset too - at app init appkit derives
	// the tray glyph from App.Icon (here: the embedded mark) and downscales
	// it, so the demo no longer ships its own resize code. The Dock icon
	// stays by default; it disappears only when -tray is requested (the
	// tray package runs the app under the menu-bar "accessory" policy).
	app := &tuohi.App{Name: "appkit demo x", Exit: true}

	// The tray menu captures w; it is assigned right after App.Show returns.
	// It is OFF by default (the windowed showcase keeps its Dock/taskbar
	// icon); pass -tray to showcase View.Show/View.Hide from a menu-bar
	// (macOS) tray. Skipped under --selftest (runs headless, no tray host).
	var w *tuohi.View
	if *trayFn && !*selftest {
		app.Tray = &tray.Config{
			Tooltip: "appkit demo",
			Items: []tray.Item{
				{Label: "Show", OnClick: func() {
					// Bringing the window back from the tray also un-minimizes
					// it, in case it was minimized rather than hidden.
					w.Unminimize()
					w.Show()
				}},
				{Label: "Hide", OnClick: func() { w.Hide() }},
				{Separator: true},
				{Label: "Quit", OnClick: app.Quit},
			},
		}
	}

	// Serve the app: ONE app-scoped App.FS carries the whole showcase on
	// every platform, and the window navigates to the uniform "app://"
	// origin - the consumer never picks a serving mechanism. App.FS is
	// served scheme-first (Linux's registered app scheme, WebView2's https
	// vhost on Windows); macOS always serves over a TEMPORARY loopback
	// http://localhost server (WKWebView SAB bug), and -http (App.HTTP)
	// opts Linux and Windows into that loopback origin too.
	app.FS = assetsRoot()
	app.HTTP = *httpFn

	// Define the window declaratively - a plain View struct, spawned later,
	// with its whole Bind map inlined. The default is frameless (no
	// decorations, fully transparent); the page draws its own titlebar (see
	// assets/index.html). --framed opts into a regular OS-framed window.
	// The demo state the handlers share (d) and the bindings themselves are
	// set up before Show; the closures only run once the page calls them,
	// by which time d is assigned and w points at the spawned window.
	maximized := false
	// --- Accessor bindings (readable / writable Go state) ------------------
	// A Bind value can be a length-2 array of two functions -
	// [2]any{getter, setter} - which binds as a property the page can READ
	// (await window.demo.theme runs the getter) and WRITE
	// (window.demo.theme = v runs the setter) while the state stays in Go. A
	// lone zero-argument function binds as a callable getter and a lone
	// one-argument function as a callable setter, so read-only and write-only
	// values need no pair. Bindings run off the UI thread, so the closures
	// guard the shared state with a mutex.
	themeMu := &sync.Mutex{}
	theme := "ocean"
	readTheme := func() (string, error) {
		themeMu.Lock()
		defer themeMu.Unlock()
		return theme, nil
	}
	writeTheme := func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return errors.New("theme must not be empty")
		}
		themeMu.Lock()
		theme = s
		themeMu.Unlock()
		// A Go-side log runs on every write, whether it came from the page
		// (window.demo.theme = v) or from Go code (writeTheme).
		log.Printf("demo: theme -> %q", s)
		return nil
	}

	// demo.counter is READ-ONLY: its lone getter function advances on every
	// read, so the page observes a live Go value with no timer or timing
	// assumption.
	counterMu := &sync.Mutex{}
	count := 0
	readCounter := func() (int, error) {
		counterMu.Lock()
		defer counterMu.Unlock()
		count++
		return count, nil
	}

	// demo.setp (WRITE-ONLY) and demo.setpState (read-only) share one backing
	// value: the page writes through demo.setp and observes the effect
	// through demo.setpState.
	setpMu := &sync.Mutex{}
	setpoint := 0
	writeSetpoint := func(p int) error {
		setpMu.Lock()
		setpoint = p
		setpMu.Unlock()
		return nil
	}
	readSetpoint := func() (int, error) {
		setpMu.Lock()
		defer setpMu.Unlock()
		return setpoint, nil
	}

	// demo.pair's backing value: the accessor pair's getter reads it and its
	// setter writes it (see the Bind map below). It is plain closure state,
	// guarded by nothing - bindings run off the UI thread but Go's string
	// writes are atomic enough for a showcase.
	pairValue := "left"

	var d *windowDemo
	view := &tuohi.View{
		Debug:  true, // the showcase always opens its inspector
		Frame:  *framed,
		Left:   10,
		Top:    10,
		Width:  1000,
		Height: 680,
		Bind: map[string]any{
			// --- Bind: Go values under window.* dotted names ----
			// Every form is one entry at one dotted name:
			//   - demoAdd/demoEcho/...: plain functions the page calls;
			//   - demo.clock: a zero-argument function - a callable getter
			//     (call it, or read it: await window.demo.clock);
			//   - demo.mark: a one-argument function - a callable setter
			//     (call it, or assign to it: window.demo.mark = v);
			//   - demo.meta: a plain JSON-encodable value - a frozen constant;
			//   - demo.pair: an explicit (getter, setter) function pair - a
			//     readable AND writable property (see the demo's bind-pair
			//     controls);
			//   - demo.theme: a [2]any{getter, setter} pair - readable AND
			//     writable;
			//   - demo.counter: a zero-argument function - read-only getter;
			//   - demo.setp: a one-argument function - write-only setter; its
			//     effect shows through demo.setpState, a zero-argument getter
			//     over the same backing value.
			// maximized tracks the toggle state of the maximize / unmaximize
			// button; this custom chrome is the only thing that maximizes the
			// window.
			"demoAdd":    func(a, b float64) float64 { return a + b },
			"demoEcho":   func(s string) string { return s },
			"demo.clock": func() string { return time.Now().Format("15:04:05.000") },
			"demo.mark":  func(s string) string { return "marked: " + s },
			"demo.meta":  map[string]any{"app": "appkit demo", "ui": "app://app/index.html"},
			// demo.pair: the accessor-pair form - [2]any{getter, setter}.
			// Reading the property runs the getter over the bridge, assigning
			// runs the setter; the value lives in this Go closure state.
			"demo.pair": [2]any{
				func() (string, error) { return pairValue, nil },
				func(v string) error { pairValue = v; return nil },
			},
			"demo.theme":     [2]any{readTheme, writeTheme}, // getter + setter: read + write
			"demo.counter":   readCounter,                   // zero-arg func: read only
			"demo.setp":      writeSetpoint,                 // one-arg func: write only
			"demo.setpState": readSetpoint,                  // zero-arg func: read only
			"demoEmitGo": func(msg string) {
				// Go -> JS: publishing through the view's events bridge from a
				// binding goroutine is safe (Emit dispatches the JS
				// notification to the UI).
				_ = d.w.Emit("demo:goEvent", "from Go: "+msg)
			},
			"demoCopyText": func(s string) error { return d.app.Copy([]byte(s)) },
			"demoPaste": func() (string, error) {
				b, err := d.app.Paste()
				return string(b), err
			},
			"demoNotify": func() string {
				// App.Notify returns the notify package error; unsupported
				// platforms surface it through
				// errors.Is(err, notify.ErrUnsupported).
				if err := d.app.Notify("appkit demo", "Hello from the appkit demo window!"); err != nil {
					return err.Error()
				}
				return ""
			},
			// Autostart (App.Autostart): the page shows the registration state
			// and toggles it. Binding callbacks run off the UI thread, which is
			// fine - autostart only writes a file / registry value.
			"demoAutostartState": func() autostartInfo {
				a := d.app.Autostart()
				return autostartInfo{
					Enabled: a.Enabled(),
					Backend: a.Backend(),
					Path:    a.Path(),
				}
			},
			"demoAutostartSet": func(on bool, args []string) error {
				a := d.app.Autostart()
				if on {
					return a.Enable(args...)
				}
				return a.Disable()
			},
			"demoDialog": func(kind string) []string {
				// View.Dialog blocks the calling goroutine until the user
				// dismisses the panel, so it must NOT run on the UI thread - a
				// Bind callback (this goroutine) is exactly the right place.
				opts := dialog.Options{Title: "appkit demo"}
				switch kind {
				case "save":
					opts.Type = dialog.TypeSave
					opts.Filename = "demo.txt"
				case "dir":
					opts.Type = dialog.TypeDirectory
				default:
					opts.Type = dialog.TypeOpen
				}
				paths, _ := d.w.Dialog(opts)
				return paths // cancelled -> nil (dialog package contract)
			},
			"demoOpen": func(rawurl string) string {
				if err := d.app.Open(rawurl); err != nil {
					return err.Error()
				}
				return ""
			},
			"demoReveal": func() string {
				exe, err := os.Executable()
				if err != nil {
					return err.Error()
				}
				if err := d.app.Reveal(exe); err != nil {
					return err.Error()
				}
				return ""
			},
			"demoExit": func() {
				// The close button in the custom chrome. Bindings run on
				// goroutines; Exit ends the App.Wait run loop below and the
				// process finishes.
				close(d.close)
				d.app.Quit()
			},
			"demoMinimize": func() {
				// The minimize button in the custom chrome. View.Minimize is
				// safe to call from this background goroutine (it marshals to
				// the UI thread).
				w.Minimize()
			},
			"demoMaximize": func() bool {
				// The maximize / unmaximize button in the custom chrome toggles
				// the window state and reports it, so the page can swap the
				// button's glyph. State is tracked here (this demo chrome is
				// the only thing that maximizes the window). See
				// View.Maximize for the macOS zoom-toggle note.
				//
				// Both directions un-minimize first: a maximize/restore
				// request aimed at a minimized window would otherwise only
				// make the window manager flash the taskbar entry - the button
				// always brings the window back on screen.
				maximized = !maximized
				if maximized {
					w.Unminimize()
					w.Maximize()
				} else {
					w.Unminimize()
					w.Unmaximize()
				}
				return maximized
			},
			// Self-test hooks: the page calls demoReady once it booted and
			// demoReport once its scripted suite finished (see assets/app.js).
			"demoReady": func() {
				select {
				case <-d.self.ready: // already signalled
				default:
					close(d.self.ready)
				}
			},
			"demoReport": func(reports []testReport) {
				select {
				case d.self.reports <- reports:
				default: // a watchdog already timed us out
				}
				select {
				case <-d.self.done: // already signalled
				default:
					close(d.self.done)
				}
				d.app.Quit()
			},
		},
	}
	d = &windowDemo{
		w:     view,
		app:   app,
		close: make(chan struct{}),
		self: &selfTest{
			active:  *selftest,
			ready:   make(chan struct{}),
			done:    make(chan struct{}),
			reports: make(chan []testReport, 1),
		},
	}

	// The first page: the uniform "app://" URL (with #selftest appended when
	// the automated suite runs). It is the declarative View.URL, navigated by
	// App.Show once the window is up.
	page := "app://"
	if d.self.active {
		page += "#selftest"
	}
	view.URL = page

	// --- Show the window ----------------------------------------------------
	// App.Show turns the declarative View above into a live window: it reads
	// the geometry/options, installs the events bridge, binds every entry of
	// the View.Bind map (plus the app-wide App.Bind map, empty here),
	// navigates to View.URL, and fires View.Ready once the page finished
	// loading. From here on the same *View is the window handle (Navigate,
	// Show/Hide, On/Emit, ...).
	if err := app.Show(view); err != nil {
		log.Fatalf("demo: %v", err)
	}
	w = view

	log.Printf("demo: webview backend: %s", app.Backend())

	// --- Events: subscribe to the JS side ------------------------------------
	// JS emits "demo:uiGreet" (e.g. from the Events card); we log it and reply
	// on "demo:goEvent", which the page listens to. The bridge lives on the
	// view itself (View.On/Off/Emit), no separate handle - Show installed it,
	// so the subscription below is live before the page loads
	// (window.events is injected at document start).
	d.w.On("demo:uiGreet", func(args ...json.RawMessage) {
		text := "?"
		if len(args) > 0 {
			var s string
			if json.Unmarshal(args[0], &s) == nil {
				text = s
			}
		}
		log.Printf("demo: JS greeted with %q", text)
		_ = d.w.Emit("demo:goEvent", "pong:"+text)
	})

	// --- Self-test watchdog ---------------------------------------------------
	if d.self.active {
		go func() {
			select {
			case <-d.self.ready:
			case <-time.After(20 * time.Second):
				log.Println("demo: page never became ready; aborting self test")
				d.app.Quit()
				return
			}
			// The suite finished (done) or is stuck: after the deadline force
			// the loop to end so main() can report the outcome.
			select {
			case <-d.self.done:
			case <-time.After(60 * time.Second):
				log.Println("demo: self test timed out")
				d.app.Quit()
			}
		}()
	}

	// Load the SAME app from the same uniform URL on every platform: the
	// embedded UI lives at the root of the demo's App.FS, so the window
	// opens "app://app/index.html" (set as the declarative View.URL above,
	// navigated by App.Show). appkit serves that origin through each
	// platform's native "app" scheme, so this code never varies. The JS
	// bridge is attached regardless of the origin, so the self test works
	// the same way everywhere.

	// The app run loop: Wait returns when the window closes (OS close, the
	// custom close button, the self test, or a watchdog) because the engines
	// report window closes into the App scope (App.Exit is true) - or when
	// App.Quit is called.
	code := 0
	if err := d.app.Wait(); err != nil {
		log.Printf("demo: wait: %v", err)
		code = 1
	} else if !d.self.active {
		code = 0
	} else {
		select {
		case reports := <-d.self.reports:
			code = summarize(reports)
		default:
			log.Println("demo: no self-test report received")
			code = 1
		}
	}

	// Tear the window down before the process exits (os.Exit skips defers).
	w.Close()
	os.Exit(code)
}
func summarize(reports []testReport) int {
	passed := 0
	for _, r := range reports {
		if r.Pass {
			passed++
		} else {
			fmt.Printf("FAIL %s: %s\n", r.Name, r.Detail)
		}
	}
	fmt.Printf("selftest %d/%d passed\n", passed, len(reports))
	if passed == len(reports) && len(reports) > 0 {
		return 0
	}
	return 1
}
