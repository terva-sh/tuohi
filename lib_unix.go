//go:build linux || freebsd || netbsd

// Unix View backend (Linux, FreeBSD, NetBSD) in pure Go via pure's
//
// This backend dlopen/dlsyms the system GTK and WebKitGTK shared objects
// directly, so appkit needs no cgo and no bundled native library on Unix.
// It detects the runtime stack: GTK4 + webkitgtk-6.0 when present, else
// GTK3 + webkit2gtk-4.1 (falling back to -4.0). The APPKIT_BACKEND
// environment variable pins one of the two stacks when both are installed;
// see linuxBackendOverride below.

package tuohi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/terva-sh/tuohi/pure"
)

const (
	gtkWindowToplevel = 0

	gPriorityHighIdle = 100
	gSourceRemove     = 0

	injectTopFrame        = 1 // WEBKIT_USER_CONTENT_INJECT_TOP_FRAME
	injectAtDocumentStart = 0 // WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START

	gdkHintMaxSize   = 1 << 2 // GDK_HINT_MAX_SIZE
	gSignalMatchData = 1 << 4 // G_SIGNAL_MATCH_DATA

	// Default window size applied when the View Width/Height are zero, matching the
	// other backends' default.
	defaultWidth  = 640
	defaultHeight = 480

	// jscGCRealtimeSignal is the POSIX signal appkit reconfigures
	// JavaScriptCore's stop-the-world GC machinery onto via
	// JSConfigureSignalForGC (see ensureInit): real-time signal 34, the first
	// one glibc leaves for application use (32/33 are NPTL-internal). Unlike
	// JSC's default SIGUSR1 it carries no pre-installed handler in an appkit
	// process, so JSC installs its own without the startup warning.
	jscGCRealtimeSignal = 34
)

// gdkGeometry mirrors the C GdkGeometry struct (passed by pointer for MAX hint).
type gdkGeometry struct {
	MinWidth, MinHeight   int32
	MaxWidth, MaxHeight   int32
	BaseWidth, BaseHeight int32
	WidthInc, HeightInc   int32
	MinAspect, MaxAspect  float64
	WinGravity            int32
	_                     int32
}

// --- bound C functions -----------------------------------------------------

var (
	gIdleAddFull                     func(priority int, function, data, notify uintptr) uint32
	gMainContextIteration            func(context uintptr, mayBlock bool) bool
	gThreadSelf                      func() uintptr
	gFree                            func(ptr uintptr)
	gObjectRefSink                   func(obj uintptr) uintptr
	gObjectUnref                     func(obj uintptr)
	gSignalConnectData               func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlersDisconnectMatched func(instance uintptr, mask int, signalID, detail uint32, closure, fn, data uintptr) uint32
	// g_bytes_new / g_list_append build the GTK4 application-icon GBytes and
	// GdkTexture list (see setAppIcon and webview.applySurfaceIcon).
	gBytesNew   func(data unsafe.Pointer, size uintptr) uintptr
	gListAppend func(list, data uintptr) uintptr
	// g_set_prgname advertises the desktop id before gtk_init, so the Wayland
	// app_id of every window matches the .desktop entry installed for the
	// application icon (see setAppIcon/installWaylandIdentity in app_unix.go).
	gSetPrgname func(name string)

	gtkInitCheck              func(argc, argv uintptr) bool
	gtkWindowNew              func(typ int) uintptr
	gtkWindowSetResizable     func(window uintptr, resizable bool)
	gtkWindowSetDecorated     func(window uintptr, decorated bool)
	gtkWindowResize           func(window uintptr, w, h int)
	gtkWidgetSetSizeRequest   func(widget uintptr, w, h int)
	gtkWindowSetGeometryHints func(window, widget uintptr, geom *gdkGeometry, mask int)
	gtkContainerAdd           func(container, widget uintptr)
	gtkContainerRemove        func(container, widget uintptr)
	gtkWidgetShow             func(widget uintptr)
	gtkWidgetHide             func(widget uintptr) // GTK3 only (see Show/Hide)
	gtkWidgetGrabFocus        func(widget uintptr)
	gtkWindowPresent          func(window uintptr)
	gtkWindowClose            func(window uintptr)
	gtkWindowMaximize         func(window uintptr)           // GTK3 + GTK4
	gtkWindowUnmaximize       func(window uintptr)           // GTK3 + GTK4
	gtkWindowIconify          func(window uintptr)           // GTK3 only (Minimize)
	gtkWindowDeiconify        func(window uintptr)           // GTK3 only (Unminimize)
	gtkWindowMinimize         func(window uintptr)           // GTK4 only (Minimize)
	gtkWindowUnminimize       func(window uintptr)           // GTK4 only (Unminimize)
	gtkWindowMove             func(window uintptr, x, y int) // GTK3/X11 only (see applyGeometry)

	// gdk device helpers for gdk_toplevel_begin_move/resize on GTK4 (the GTK3
	// gtk_window_begin_move/resize_drag entry points take no device).
	gdkDisplayGetDefaultSeat func(display uintptr) uintptr
	gdkSeatGetPointer        func(seat uintptr) uintptr
	gtkWidgetGetDisplay      func(widget uintptr) uintptr

	// Used to detect the Wayland backend and re-announce the decoration mode
	// of frameless windows (see applyFramelessCSD).
	gdkDisplayGetDefault func() uintptr
	gdkDisplayGetName    func(display uintptr) uintptr
	gtkWidgetGetWindow   func(widget uintptr) uintptr // GTK3 only

	// Window-background helpers (see applyBackground). The gtk3 group is
	// registered only for the GTK3 stack.
	gdkScreenGetDefault              func() uintptr
	gdkScreenGetRGBAVisual           func(screen uintptr) uintptr
	gtkWidgetSetVisual               func(widget, visual uintptr)
	gtkWidgetOverrideBackgroundColor func(widget uintptr, state int32, color *[4]float64)
	webkitWebViewSetBackgroundColor  func(webview uintptr, color *[4]float64) // resolved via Dlsym (older WebKitGTK lacks it)

	// GTK4 frameless-window transparency helpers (see applyBackground). GTK4
	// has no RGBA-visual selection and no widget-background override, so the
	// theme's CSS paints an opaque background on the toplevel behind the web
	// view; a per-window CSS provider that makes the window's own background
	// transparent is what lets the desktop show through on Wayland.
	gtkCssProviderNew            func() uintptr
	gtkCssProviderLoadFromString func(provider uintptr, css string, length int)
	gtkWidgetGetStyleContext     func(widget uintptr) uintptr
	gtkStyleContextAddProvider   func(context, provider uintptr, priority uint32)

	// Frameless-window interactive moving/resizing. GTK3 keeps the old
	// gtk_window_begin_move/resize_drag entry points (root coords, no device);
	// GTK4 removed them and drives the drag via gdk_toplevel_begin_move/resize
	// on the window's GdkSurface (surface coords + pointer device).
	gtkWindowBeginMoveDrag3   func(window uintptr, button int32, rootX, rootY int32, timestamp uint32)
	gtkWindowBeginResizeDrag3 func(window uintptr, edge int32, button int32, rootX, rootY int32, timestamp uint32)
	gdkToplevelBeginMove      func(toplevel, device uintptr, button int32, x, y float64, timestamp uint32)
	gdkToplevelBeginResize    func(toplevel uintptr, edge int32, device uintptr, button int32, x, y float64, timestamp uint32)
	gtkNativeGetSurface       func(native uintptr) uintptr

	// Application-icon plumbing for App.Icon (see setAppIcon in app_unix.go).
	// GTK3 installs the default window icon as a GdkPixbuf
	// (gtk_window_set_default_icon; gdk-pixbuf resolves through the GTK
	// handle's dependency closure). GTK4 removed the pixbuf window-icon APIs
	// and takes icon textures per toplevel surface
	// (gdk_toplevel_set_icon_list). The GTK4 entry points are optional -
	// older GTK4 builds without them skip the icon silently.
	gtkWindowSetDefaultIcon func(icon uintptr)                                                                                                                         // GTK3
	gtkWindowSetIcon        func(window, icon uintptr)                                                                                                                 // GTK3: per-window icon (see applyWindowIcon)
	gdkPixbufNewFromData    func(data unsafe.Pointer, colorspace, hasAlpha, bitsPerSample, width, height, rowstride int32, destroyNotify, destroyData uintptr) uintptr // GTK3
	gdkMemoryTextureNew     func(width, height, format int32, bytes, stride uintptr) uintptr                                                                           // GTK4
	gdkToplevelSetIconList  func(toplevel, list uintptr)                                                                                                               // GTK4
	haveGdkIcons            bool

	// Maximized-state query for the drag-area double-click toggle (see
	// toggleMaximize): GTK4 reads the window's GdkSurface via
	// gdk_toplevel_get_state, GTK3 reads the widget's GdkWindow via
	// gdk_window_get_state. Both return a state bitmask in which the maximized
	// bit is 1<<1 == GDK_WINDOW_STATE_MAXIMIZED / GDK_TOPLEVEL_STATE_MAXIMIZED.
	gdkToplevelGetState func(toplevel uintptr) uint32 // GTK4
	gdkWindowGetState   func(window uintptr) uint32   // GTK3

	// GTK 4 variants (bound + used only when gtk4 is true).
	gtk4                    bool
	gtkInitCheck0           func() bool
	gtkWindowNew0           func() uintptr
	gtkWindowSetChild       func(window, widget uintptr)
	gtkWidgetSetVisible     func(widget uintptr, visible bool)
	gtkWindowSetDefaultSize func(window uintptr, w, h int)
	webkitRegisterHandler3  func(manager uintptr, name string, world uintptr)

	webkitWebViewNew                              func() uintptr
	webkitWebViewGetUserContentManager            func(webview uintptr) uintptr
	webkitWebViewGetSettings                      func(webview uintptr) uintptr
	webkitSettingsSetEnableMediaStream            func(settings uintptr, enabled bool)
	webkitSettingsSetJavascriptCanAccessClipboard func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWriteConsoleToStdout   func(settings uintptr, enabled bool)
	webkitSettingsSetEnableDeveloperExtras        func(settings uintptr, enabled bool)
	webkitSettingsSetEnableJavascript             func(settings uintptr, enabled bool)
	webkitWebViewLoadURI                          func(webview uintptr, uri string)
	webkitWebViewLoadHTML                         func(webview uintptr, html string, baseURI string)
	webkitWebViewGetURI                           func(webview uintptr) uintptr
	webkitUserContentManagerRegisterHandler       func(manager uintptr, name string)
	webkitUserContentManagerAddScript             func(manager, script uintptr)
	webkitUserContentManagerRemoveAllScripts      func(manager uintptr)
	webkitUserScriptNew                           func(source string, frames, time int, allow, block uintptr) uintptr
	webkitUserScriptUnref                         func(script uintptr)
	webkitJavascriptResultGetJSValue              func(result uintptr) uintptr

	// The navigation policy (see decidePolicy).
	webkitNavigationPolicyDecisionGetNavigationAction func(decision uintptr) uintptr
	webkitNavigationActionGetRequest                  func(action uintptr) uintptr
	webkitNetworkErrorQuark                           func() uint32
	webkitPolicyErrorQuark                            func() uint32
	webkitResponsePolicyDecisionGetRequest            func(decision uintptr) uintptr
	webkitResponsePolicyDecisionIsMainFrameMainRes    func(decision uintptr) bool
	webkitURIRequestGetURI                            func(request uintptr) uintptr
	webkitPolicyDecisionIgnore                        func(decision uintptr)

	webkitWebViewEvaluateJavascript func(webview uintptr, script string, length int, world, source, cancellable, callback, userData uintptr)
	webkitWebViewRunJavascript      func(webview uintptr, script string, cancellable, callback, userData uintptr)
	haveEvaluateJavascript          bool

	jscValueToString func(value uintptr) uintptr
)

// --- one-time init ---------------------------------------------------------

var (
	initOnce     sync.Once
	initErr      error
	uiThreadOnce sync.Once
	// uiThread is the GThread of the thread newView pinned, the one that owns
	// GTK. Zero until the first view is created.
	uiThread atomic.Uintptr

	dispatchSourceFn uintptr
	messageHandlerFn uintptr
	windowDestroyFn  uintptr
	loadChangedFn    uintptr
	loadFailedFn     uintptr
	decidePolicyFn   uintptr

	// Library handles kept after ensureInit so other files (e.g. the file
	// dialogs in dialog_unix.go) can lazily resolve extra symbols without
	// re-dlopening or duplicating the soname-selection logic.
	gtkLib, glibLib uintptr
)

func openFirst(names ...string) (uintptr, error) {
	var lastErr error
	for _, n := range names {
		h, err := pure.Dlopen(n, pure.RTLD_NOW|pure.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("webview: none of %v could be loaded: %w", names, lastErr)
}

// --- APPKIT_BACKEND backend selection --------------------------------------

// envBackendGTK4 and envBackendGTK3 are the documented APPKIT_BACKEND values:
// the two WebKitGTK stacks appkit can use on Unix, named after their
// library family (libwebkitgtk-6.0 vs libwebkit2gtk-4.1). The values match
// the package names distros use (webkitgtk-6.0 / webkit2gtk-4.1).
const (
	envBackendGTK4 = "webkitgtk-6.0"  // GTK4 stack (libwebkitgtk-6.0.so.4 + libgtk-4.so.1)
	envBackendGTK3 = "webkit2gtk-4.1" // GTK3 stack (libwebkit2gtk-4.1.so.0, -4.0 fallback, + libgtk-3.so.0)
)

// Backend preferences returned by linuxBackendOverride.
const (
	backendAuto int = iota // auto-detect (APPKIT_BACKEND unset or unknown)
	backendGTK4            // force the GTK4 + webkitgtk-6.0 stack
	backendGTK3            // force the GTK3 + webkit2gtk-4.x stack
)

// linuxBackendOverride reads APPKIT_BACKEND and reports which stack it pins:
// backendGTK4 for envBackendGTK4, backendGTK3 for envBackendGTK3, backendAuto
// when it is unset. Any other value prints a warning on stderr and is treated
// as backendAuto, so a typo degrades to the documented auto-detection chain
// instead of failing the app. macOS and Windows always use their
// single built-in backend and never consult this variable; FreeBSD and
// NetBSD run this same GTK backend, so APPKIT_BACKEND applies there too.
func linuxBackendOverride() int {
	v := os.Getenv("APPKIT_BACKEND")
	if v == "" {
		return backendAuto
	}
	switch v {
	case envBackendGTK4:
		return backendGTK4
	case envBackendGTK3:
		return backendGTK3
	}
	fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%q is not a known value (want %q or %q); using the auto-detected stack\n",
		v, envBackendGTK4, envBackendGTK3)
	return backendAuto
}

// loadGTK4Stack dlopens the GTK4 sonames: libwebkitgtk-6.0.so.4 first (the
// deciding probe, see ensureInit), then libgtk-4.so.1 and
// libjavascriptcoregtk-6.0.so.1.
func loadGTK4Stack() (gtk, webkit, jsc uintptr, err error) {
	webkit, err = openFirst("libwebkitgtk-6.0.so.4")
	if err != nil {
		return 0, 0, 0, err
	}
	gtk, err = openFirst("libgtk-4.so.1")
	if err != nil {
		return 0, 0, 0, err
	}
	jsc, err = openFirst("libjavascriptcoregtk-6.0.so.1")
	if err != nil {
		return 0, 0, 0, err
	}
	return gtk, webkit, jsc, nil
}

// loadGTK3Stack dlopens the GTK3 sonames: libgtk-3.so.0, then
// libwebkit2gtk-4.1.so.0 (falling back to libwebkit2gtk-4.0.so.37) and the
// matching libjavascriptcoregtk-4.1/-4.0.
func loadGTK3Stack() (gtk, webkit, jsc uintptr, err error) {
	gtk, err = openFirst("libgtk-3.so.0")
	if err != nil {
		return 0, 0, 0, err
	}
	webkit, err = openFirst("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
	if err != nil {
		return 0, 0, 0, err
	}
	jsc, err = openFirst("libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18")
	if err != nil {
		return 0, 0, 0, err
	}
	return gtk, webkit, jsc, nil
}

// platformBackend reports the web-engine backend the loaded stack provides,
// using the APPKIT_BACKEND value names so callers can echo back exactly what
// they want. It is meaningful only after ensureInit has run (see App.Backend).
func platformBackend() string {
	if gtk4 {
		return envBackendGTK4
	}
	return envBackendGTK3
}

func ensureInit() error {
	initOnce.Do(func() {
		_ = os.Unsetenv("JSC_SIGNAL_FOR_GC")
		// SharedArrayBuffer: some WebKitGTK builds (e.g. Ubuntu) gate the
		// SAB global behind the JSC option useSharedArrayBuffer, which ships
		// disabled by default even for cross-origin-isolated pages. The
		// WebKit web process reads JSC options from the environment when it
		// spawns, so the option must be on BEFORE the first web view exists;
		// it is therefore set here, in the one-time engine init.
		_ = os.Setenv("JSC_useSharedArrayBuffer", "1")

		glib, err := openFirst("libglib-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		gobject, err := openFirst("libgobject-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		// Prefer the GTK4 + webkitgtk-6.0 stack; fall back to GTK3 + webkit2gtk-4.x.
		//
		// The deciding probe is the webkit library, NOT libgtk-4. A GTK3 desktop
		// commonly also has libgtk-4 installed (for newer apps), and dlopen-ing
		// both GTK3 and GTK4 into the same process corrupts the GObject type
		// system and crashes gtk_init ("cannot register existing type
		// 'GdkDisplayManager'"). So load libgtk-4 only when webkitgtk-6.0 is
		// actually present -- otherwise GTK4 never enters the process.
		//
		// APPKIT_BACKEND pins one of the two stacks before this chain runs. A
		// pinned stack whose libraries cannot be loaded prints a warning and
		// falls through to the auto-detection chain, so the app still starts on
		// the stack that works.
		var gtk, webkit, jsc uintptr
		switch linuxBackendOverride() {
		case backendGTK4:
			var err error
			if gtk, webkit, jsc, err = loadGTK4Stack(); err != nil {
				fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%s is not available on this system (%v); using the auto-detected stack\n", envBackendGTK4, err)
			} else {
				gtk4 = true
			}
		case backendGTK3:
			var err error
			if gtk, webkit, jsc, err = loadGTK3Stack(); err != nil {
				fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%s is not available on this system (%v); using the auto-detected stack\n", envBackendGTK3, err)
			}
		}
		if gtk == 0 {
			// Auto-detection (APPKIT_BACKEND unset, unknown, or its pinned
			// stack failed to load above).
			var err error
			if gtk, webkit, jsc, err = loadGTK4Stack(); err == nil {
				gtk4 = true
			} else if gtk, webkit, jsc, err = loadGTK3Stack(); err != nil {
				initErr = err
				return
			}
		}

		gtkLib, glibLib = gtk, glib

		// JavaScriptCore (JSC) suspends threads during its stop-the-world
		// garbage collections with a POSIX signal, and its default - SIGUSR1,
		// signal 10 on Linux - already carries the Go runtime's handler in
		// every appkit process. JSC would therefore print "Overriding
		// existing handler for signal 10. Set JSC_SIGNAL_FOR_GC if you want
		// WebKit to use a different signal" as the process's first stderr
		// line and replace Go's handler. That message is written by WebKit
		// directly to stderr - it never goes through glib's print hooks, so
		// the g_set_print_handler / g_set_printerr_handler wiring cannot
		// intercept it. Reconfigure JSC onto a signal nothing in the process
		// handles yet (jscGCRealtimeSignal): JSConfigureSignalForGC must run
		// before the first JSC use, which is right here - the libraries are
		// loaded but no JavaScript has run. The signal is configured for the
		// whole process and applies wherever JSC initializes, and because it
		// is not the JSC_SIGNAL_FOR_GC environment variable, JSC's options
		// scanner has nothing to complain about. Linux only (FreeBSD/NetBSD
		// number their signals differently and are compile-only targets);
		// WebKitGTK builds old enough to lack the API keep JSC's default.
		if runtime.GOOS == "linux" {
			if addr, e := pure.Dlsym(jsc, "JSConfigureSignalForGC"); e == nil {
				var configureSignalForGC func(sig int32)
				pure.RegisterFunc(&configureSignalForGC, addr)
				configureSignalForGC(jscGCRealtimeSignal)
			}
		}

		pure.RegisterLibFunc(&gIdleAddFull, glib, "g_idle_add_full")
		pure.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		pure.RegisterLibFunc(&gThreadSelf, glib, "g_thread_self")
		pure.RegisterLibFunc(&gFree, glib, "g_free")
		pure.RegisterLibFunc(&gBytesNew, glib, "g_bytes_new")
		pure.RegisterLibFunc(&gListAppend, glib, "g_list_append")
		pure.RegisterLibFunc(&gSetPrgname, glib, "g_set_prgname")
		pure.RegisterLibFunc(&gObjectRefSink, gobject, "g_object_ref_sink")
		pure.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		pure.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
		// g_signal_handlers_disconnect_by_data is a macro, not a symbol.
		pure.RegisterLibFunc(&gSignalHandlersDisconnectMatched, gobject, "g_signal_handlers_disconnect_matched")

		if gtk4 {
			pure.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
			pure.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
			pure.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
			pure.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
			pure.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
			pure.RegisterLibFunc(&gtkNativeGetSurface, gtk, "gtk_native_get_surface")
			// GTK4 removed gtk_window_begin_move/resize_drag; the drag is driven
			// on the window's GdkSurface via gdk_toplevel_begin_move/resize.
			// GDK4 ships inside libgtk-4, so the same handle resolves them.
			pure.RegisterLibFunc(&gdkToplevelBeginMove, gtk, "gdk_toplevel_begin_move")
			pure.RegisterLibFunc(&gdkToplevelBeginResize, gtk, "gdk_toplevel_begin_resize")
			pure.RegisterLibFunc(&gdkToplevelGetState, gtk, "gdk_toplevel_get_state")
			// Frameless transparency on GTK4 (see applyBackground): the window
			// background is cleared through CSS instead of an RGBA visual.
			pure.RegisterLibFunc(&gtkCssProviderNew, gtk, "gtk_css_provider_new")
			pure.RegisterLibFunc(&gtkCssProviderLoadFromString, gtk, "gtk_css_provider_load_from_string")
			pure.RegisterLibFunc(&gtkWidgetGetStyleContext, gtk, "gtk_widget_get_style_context")
			pure.RegisterLibFunc(&gtkStyleContextAddProvider, gtk, "gtk_style_context_add_provider")
			// Window-state controls (see Show/Hide/Maximize/Minimize): GTK4
			// minimizes through gtk_window_minimize (iconify is the GTK3 name).
			pure.RegisterLibFunc(&gtkWindowMinimize, gtk, "gtk_window_minimize")
			pure.RegisterLibFunc(&gtkWindowUnminimize, gtk, "gtk_window_unminimize")
			// Runtime application icon: gdk_toplevel_set_icon_list plus the
			// gdk_memory_texture_new builder. Optional - a GTK4 build without
			// them simply skips the icon (best-effort, see App.Icon).
			if _, e := pure.Dlsym(gtk, "gdk_toplevel_set_icon_list"); e == nil {
				pure.RegisterLibFunc(&gdkMemoryTextureNew, gtk, "gdk_memory_texture_new")
				pure.RegisterLibFunc(&gdkToplevelSetIconList, gtk, "gdk_toplevel_set_icon_list")
				haveGdkIcons = true
			}
		} else {
			pure.RegisterLibFunc(&gtkInitCheck, gtk, "gtk_init_check")
			pure.RegisterLibFunc(&gtkWindowNew, gtk, "gtk_window_new")
			pure.RegisterLibFunc(&gtkContainerAdd, gtk, "gtk_container_add")
			pure.RegisterLibFunc(&gtkContainerRemove, gtk, "gtk_container_remove")
			pure.RegisterLibFunc(&gtkWidgetShow, gtk, "gtk_widget_show")
			pure.RegisterLibFunc(&gtkWidgetHide, gtk, "gtk_widget_hide")
			pure.RegisterLibFunc(&gtkWindowIconify, gtk, "gtk_window_iconify")
			pure.RegisterLibFunc(&gtkWindowDeiconify, gtk, "gtk_window_deiconify")
			pure.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
			pure.RegisterLibFunc(&gtkWindowSetGeometryHints, gtk, "gtk_window_set_geometry_hints")
			pure.RegisterLibFunc(&gtkWindowBeginMoveDrag3, gtk, "gtk_window_begin_move_drag")
			pure.RegisterLibFunc(&gtkWindowBeginResizeDrag3, gtk, "gtk_window_begin_resize_drag")
			pure.RegisterLibFunc(&gdkWindowGetState, gtk, "gdk_window_get_state")
			pure.RegisterLibFunc(&gtkWindowMove, gtk, "gtk_window_move")
			// Runtime application icon (App.Icon): GTK3 window icons are
			// GdkPixbufs. The two gtk_window_* setters live in libgtk-3 itself
			// and are registered unconditionally; the pixbuf constructor comes
			// from libgdk_pixbuf-2.0, dlopen'd EXPLICITLY (never via the GTK
			// handle's dependency closure, whose visibility depends on the
			// libc's dlopen semantics). A machine without libgdk_pixbuf simply
			// skips the icon (best-effort, see App.Icon).
			pure.RegisterLibFunc(&gtkWindowSetDefaultIcon, gtk, "gtk_window_set_default_icon")
			pure.RegisterLibFunc(&gtkWindowSetIcon, gtk, "gtk_window_set_icon")
			if pb, e := openFirst("libgdk_pixbuf-2.0.so.0", "libgdk_pixbuf-2.0.so"); e == nil {
				if _, se := pure.Dlsym(pb, "gdk_pixbuf_new_from_data"); se == nil {
					pure.RegisterLibFunc(&gdkPixbufNewFromData, pb, "gdk_pixbuf_new_from_data")
				}
			}
		}
		pure.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
		pure.RegisterLibFunc(&gtkWindowSetDecorated, gtk, "gtk_window_set_decorated")
		pure.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		pure.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
		pure.RegisterLibFunc(&gtkWidgetGetDisplay, gtk, "gtk_widget_get_display")
		pure.RegisterLibFunc(&gdkDisplayGetDefaultSeat, gtk, "gdk_display_get_default_seat")
		pure.RegisterLibFunc(&gdkSeatGetPointer, gtk, "gdk_seat_get_pointer")
		pure.RegisterLibFunc(&gdkDisplayGetDefault, gtk, "gdk_display_get_default")
		pure.RegisterLibFunc(&gdkDisplayGetName, gtk, "gdk_display_get_name")
		if !gtk4 {
			// gtk_widget_get_window does not exist in GTK4 (GtkWidgets there
			// have no GdkWindow); it is only used by the GTK3 frameless path.
			pure.RegisterLibFunc(&gtkWidgetGetWindow, gtk, "gtk_widget_get_window")
			// Window-background helpers, all GTK3-only (GTK4 has native
			// transparency and no widget-background override API).
			pure.RegisterLibFunc(&gdkScreenGetDefault, gtk, "gdk_screen_get_default")
			pure.RegisterLibFunc(&gdkScreenGetRGBAVisual, gtk, "gdk_screen_get_rgba_visual")
			pure.RegisterLibFunc(&gtkWidgetSetVisual, gtk, "gtk_widget_set_visual")
			pure.RegisterLibFunc(&gtkWidgetOverrideBackgroundColor, gtk, "gtk_widget_override_background_color")
		}
		// Present exists in GTK3 and GTK4 alike, so no version split here.
		pure.RegisterLibFunc(&gtkWindowPresent, gtk, "gtk_window_present")
		pure.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
		pure.RegisterLibFunc(&gtkWindowMaximize, gtk, "gtk_window_maximize")
		pure.RegisterLibFunc(&gtkWindowUnmaximize, gtk, "gtk_window_unmaximize")

		pure.RegisterLibFunc(&webkitWebViewNew, webkit, "webkit_web_view_new")
		pure.RegisterLibFunc(&webkitWebViewGetUserContentManager, webkit, "webkit_web_view_get_user_content_manager")
		pure.RegisterLibFunc(&webkitWebViewGetSettings, webkit, "webkit_web_view_get_settings")
		pure.RegisterLibFunc(&webkitSettingsSetEnableMediaStream, webkit, "webkit_settings_set_enable_media_stream")
		pure.RegisterLibFunc(&webkitSettingsSetJavascriptCanAccessClipboard, webkit, "webkit_settings_set_javascript_can_access_clipboard")
		pure.RegisterLibFunc(&webkitSettingsSetEnableWriteConsoleToStdout, webkit, "webkit_settings_set_enable_write_console_messages_to_stdout")
		pure.RegisterLibFunc(&webkitSettingsSetEnableDeveloperExtras, webkit, "webkit_settings_set_enable_developer_extras")
		pure.RegisterLibFunc(&webkitSettingsSetEnableJavascript, webkit, "webkit_settings_set_enable_javascript")
		pure.RegisterLibFunc(&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri")
		pure.RegisterLibFunc(&webkitWebViewLoadHTML, webkit, "webkit_web_view_load_html")
		pure.RegisterLibFunc(&webkitWebViewGetURI, webkit, "webkit_web_view_get_uri")
		pure.RegisterLibFunc(&webkitUserContentManagerAddScript, webkit, "webkit_user_content_manager_add_script")
		pure.RegisterLibFunc(&webkitUserContentManagerRemoveAllScripts, webkit, "webkit_user_content_manager_remove_all_scripts")
		pure.RegisterLibFunc(&webkitUserScriptNew, webkit, "webkit_user_script_new")
		pure.RegisterLibFunc(&webkitUserScriptUnref, webkit, "webkit_user_script_unref")
		pure.RegisterLibFunc(&webkitNavigationPolicyDecisionGetNavigationAction, webkit, "webkit_navigation_policy_decision_get_navigation_action")
		pure.RegisterLibFunc(&webkitNavigationActionGetRequest, webkit, "webkit_navigation_action_get_request")
		pure.RegisterLibFunc(&webkitNetworkErrorQuark, webkit, "webkit_network_error_quark")
		pure.RegisterLibFunc(&webkitPolicyErrorQuark, webkit, "webkit_policy_error_quark")
		pure.RegisterLibFunc(&webkitResponsePolicyDecisionGetRequest, webkit, "webkit_response_policy_decision_get_request")
		pure.RegisterLibFunc(&webkitResponsePolicyDecisionIsMainFrameMainRes, webkit, "webkit_response_policy_decision_is_main_frame_main_resource")
		pure.RegisterLibFunc(&webkitURIRequestGetURI, webkit, "webkit_uri_request_get_uri")
		pure.RegisterLibFunc(&webkitPolicyDecisionIgnore, webkit, "webkit_policy_decision_ignore")
		if gtk4 {
			// GTK4: the script-message callback delivers a JSCValue* directly, and
			// the handler registration takes a world-name argument.
			pure.RegisterLibFunc(&webkitRegisterHandler3, webkit, "webkit_user_content_manager_register_script_message_handler")
		} else {
			pure.RegisterLibFunc(&webkitUserContentManagerRegisterHandler, webkit, "webkit_user_content_manager_register_script_message_handler")
			pure.RegisterLibFunc(&webkitJavascriptResultGetJSValue, webkit, "webkit_javascript_result_get_js_value")
		}

		_, e := pure.Dlsym(webkit, "webkit_web_view_evaluate_javascript")
		if e == nil {
			pure.RegisterLibFunc(&webkitWebViewEvaluateJavascript, webkit, "webkit_web_view_evaluate_javascript")
			haveEvaluateJavascript = true
		} else {
			pure.RegisterLibFunc(&webkitWebViewRunJavascript, webkit, "webkit_web_view_run_javascript")
		}

		pure.RegisterLibFunc(&jscValueToString, jsc, "jsc_value_to_string")

		// webkit_web_view_set_background_color is present in every current
		// WebKitGTK, but older ones lack it; resolve optionally so the
		// window-background feature degrades to a no-op there.
		if addr, e := pure.Dlsym(webkit, "webkit_web_view_set_background_color"); e == nil {
			pure.RegisterFunc(&webkitWebViewSetBackgroundColor, addr)
		}

		dispatchSourceFn = pure.NewCallback(func(data uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[data]
			delete(dispatchMap, data)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return gSourceRemove
		})
		messageHandlerFn = pure.NewCallback(func(_, jsResult, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				// WebKitGTK does not say which frame posted the message, so
				// the sender is the top-level page. A null URI means no page
				// is loaded, and names no trusted origin.
				w.onMessage(jsResultToString(jsResult), cstr(webkitWebViewGetURI(w.webview)), true)
			}
			return 0
		})
		windowDestroyFn = pure.NewCallback(func(_, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				w.onWindowDestroy()
			}
			return 0
		})
		loadChangedFn = pure.NewCallback(func(_, loadEvent, userData uintptr) uintptr {
			// WEBKIT_LOAD_FINISHED == 3: the page finished loading; View.Ready
			// fires exactly once per view on this moment.
			if int32(loadEvent) == 3 {
				if w := lookupEngine(userData); w != nil {
					w.fireReady()
				}
			}
			return 0
		})
		loadFailedFn = pure.NewCallback(func(_, loadEvent, failingURI, gerror, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w == nil || !w.loadFailed(int32(loadEvent), cstr(failingURI), gerror) {
				return 0 // WebKit's error page
			}
			return 1
		})
		decidePolicyFn = pure.NewCallback(func(_, decision, decisionType, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w == nil || !w.decidePolicy(decision, int(decisionType)) {
				return 0 // WebKit's default decision
			}
			return 1
		})
	})
	return initErr
}

// jsResultToString turns the script-message callback's second argument into a
// Go string. On GTK4 it is a JSCValue* directly; on GTK3 it is a
// WebKitJavascriptResult* that must be unwrapped first.
func jsResultToString(arg uintptr) string {
	value := arg
	if !gtk4 {
		value = webkitJavascriptResultGetJSValue(arg)
	}
	cs := jscValueToString(value)
	s := cstr(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

// gtkInit, gtkNewWindow and registerScriptHandler hide the GTK3/GTK4 call-arity
// differences.
func gtkInit() bool {
	if gtk4 {
		return gtkInitCheck0()
	}
	return gtkInitCheck(0, 0)
}

func gtkNewWindow() uintptr {
	if gtk4 {
		return gtkWindowNew0()
	}
	return gtkWindowNew(gtkWindowToplevel)
}

func registerScriptHandler(manager uintptr, name string) {
	if gtk4 {
		webkitRegisterHandler3(manager, name, 0) // default script world
		return
	}
	webkitUserContentManagerRegisterHandler(manager, name)
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

// --- instance + dispatch registries ----------------------------------------

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func registerEngine(w *webview) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = w
	regMu.Unlock()
	return id
}

func unregisterEngine(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id uintptr) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	gIdleAddFull(gPriorityHighIdle, dispatchSourceFn, id, 0)
}

// postUI is the dispatcher's post hook (see uiDispatcher). The default main
// context always takes an idle source, so it never refuses.
func postUI(f func()) bool {
	dispatchMain(f)
	return true
}

// uiLoopExternal is the dispatcher's external hook. Only App.Wait and Run
// iterate the GTK main context, so no loop runs that tuohi does not know of.
func uiLoopExternal() bool { return false }

// --- webview ---------------------------------------------------------------

// webview is the Unix implementation behind the View struct (Linux,
// FreeBSD, NetBSD).
type webview struct {
	id         uintptr
	window     uintptr
	webview    uintptr
	manager    uintptr
	ownsWindow bool

	// frameless windows drop the WM decorations via gtk_window_set_decorated;
	// the page's -app-region boxes then drive the move drag
	// (gtk_window_begin_move_drag on GTK3, gdk_toplevel_begin_move on GTK4) and
	// the edge bands drive the matching begin_resize drag.
	frameless bool

	stopRunLoop   bool
	isWindowShown bool
	isSizeSet     bool

	schemeCB uintptr // retained pure trampoline

	viewCore
}

// registerSchemes wires the app-scope "app" scheme (when App.FS is set) onto
// the web view's WebKitWebContext and marks the scheme as a secure context.
// Called before the first Navigate. It returns an error when the scheme
// cannot be registered (a missing library, handle, or symbol), rather than
// silently leaving the scheme unregistered so app:// pages fail to load with
// no diagnostic anywhere.
func (w *webview) registerSchemes() error {
	if w.serve == nil {
		return nil
	}
	if w.webview == 0 {
		return errors.New("webview: register scheme: web view not created")
	}
	webkitSonames := []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"}
	if gtk4 {
		webkitSonames = []string{"libwebkitgtk-6.0.so.4"}
	}
	webkit, err := openFirst(webkitSonames...)
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}
	gio, err := openFirst("libgio-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gio: %w", err)
	}
	gobject, err := openFirst("libgobject-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gobject: %w", err)
	}
	gFreeAddr, err := pure.Dlsym(glibLib, "g_free")
	if err != nil {
		return fmt.Errorf("webview: register schemes: resolve g_free: %w", err)
	}
	// g_memdup2 (gsize length) only exists on GLib >= 2.68; on older GLib
	// (Debian 11, Ubuntu 20.04) fall back to g_memdup (guint length). Resolve
	// with Dlsym, not RegisterLibFunc, which panics when a symbol is absent.
	memdup, err := resolveMemdup(glibLib)
	if err != nil {
		return fmt.Errorf("webview: register schemes: %w", err)
	}

	var (
		getContext               func(uintptr) uintptr
		registerScheme           func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager       func(uintptr) uintptr
		registerAsSecure         func(sm uintptr, scheme string)
		requestGetURI            func(uintptr) uintptr
		schemeRequestFinish      func(req, stream uintptr, streamLen int64, contentType string)
		schemeRequestFinishError func(req, err uintptr)
		memInputStreamNew        func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		gObjectUnref             func(uintptr)
		newErrorLiteral          func(domain uint32, code int32, message string) uintptr
		freeError                func(err uintptr)
		ioErrorQuark             func() uint32
	)
	pure.RegisterLibFunc(&getContext, webkit, "webkit_web_view_get_context")
	pure.RegisterLibFunc(&registerScheme, webkit, "webkit_web_context_register_uri_scheme")
	pure.RegisterLibFunc(&getSecurityManager, webkit, "webkit_web_context_get_security_manager")
	pure.RegisterLibFunc(&registerAsSecure, webkit, "webkit_security_manager_register_uri_scheme_as_secure")
	pure.RegisterLibFunc(&requestGetURI, webkit, "webkit_uri_scheme_request_get_uri")
	pure.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	pure.RegisterLibFunc(&schemeRequestFinishError, webkit, "webkit_uri_scheme_request_finish_error")
	pure.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	pure.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
	pure.RegisterLibFunc(&newErrorLiteral, glibLib, "g_error_new_literal")
	pure.RegisterLibFunc(&freeError, glibLib, "g_error_free")
	pure.RegisterLibFunc(&ioErrorQuark, gio, "g_io_error_quark")

	ctx := getContext(w.webview)
	if ctx == 0 {
		return errors.New("webview: register schemes: web context is nil")
	}
	sm := getSecurityManager(ctx)
	if sm == 0 {
		return errors.New("webview: register schemes: security manager is nil")
	}

	// void (*WebKitURISchemeRequestCallback)(WebKitURISchemeRequest*, gpointer).
	// user_data is the engine id, so this resolves back to the right webview.
	w.schemeCB = pure.NewCallback(func(req uintptr, data uintptr) uintptr {
		eng := lookupEngine(data)
		if eng == nil {
			return 0
		}
		url := cstr(requestGetURI(req))
		resp := callServe(eng.serve, &request{URL: url})
		if resp == nil {
			// A nil response means "not found": finish with an error so the load
			// fails, matching macOS (didFailWithError:) and Windows (default 404)
			// instead of delivering a successful empty document.
			const gIOErrorNotFound = 1 // G_IO_ERROR_NOT_FOUND
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(req, gerr)
			freeError(gerr) // finish_error copies it; we own our reference
			return 0
		}
		body, mime := resp.Body, schemeMIME(resp)
		// Copy into glib-owned memory freed by g_free once the stream is done, so
		// the bytes outlive this callback (the stream is read asynchronously).
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = memdup(unsafe.Pointer(&body[0]), len(body)) // #nosec G103 -- copied into glib memory, freed by g_free
		}
		stream := memInputStreamNew(dataPtr, len(body), uintptr(gFreeAddr))
		schemeRequestFinish(req, stream, int64(len(body)), mime)
		gObjectUnref(stream)
		return 0
	})
	registerScheme(ctx, appSchemeName, w.schemeCB, w.id, 0)
	registerAsSecure(sm, appSchemeName)
	return nil
}

// resolveMemdup returns a "copy into glib-owned memory" function, preferring
// g_memdup2 (GLib >= 2.68, gsize length) and falling back to g_memdup (older
// GLib, guint length) so custom schemes still work on Debian 11 / Ubuntu 20.04.
// The copy is freed with g_free once the input stream has been read. Dlsym is
// used instead of RegisterLibFunc because the latter panics on a missing symbol
// and g_memdup2 legitimately is absent on older systems.
func resolveMemdup(glib uintptr) (func(mem unsafe.Pointer, size int) unsafe.Pointer, error) {
	if f, ok := memdupFn[uint64](glib, "g_memdup2"); ok {
		return f, nil
	}
	if f, ok := memdupFn[uint32](glib, "g_memdup"); ok {
		return f, nil
	}
	return nil, errors.New("neither g_memdup2 nor g_memdup is available")
}

// memdupFn resolves one g_memdup symbol and returns a copy function taking
// the size as int. T is the symbol's length parameter width - gsize (uint64)
// for g_memdup2, guint (uint32) for the older g_memdup - so the registered
// signature matches the C ABI exactly; ok is false when the symbol is absent.
func memdupFn[T uint32 | uint64](glib uintptr, symbol string) (func(mem unsafe.Pointer, size int) unsafe.Pointer, bool) {
	addr, err := pure.Dlsym(glib, symbol)
	if err != nil || addr == 0 {
		return nil, false
	}
	var f func(unsafe.Pointer, T) unsafe.Pointer
	pure.RegisterFunc(&f, addr)
	return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, T(size)) }, true
}

func (w *webview) windowInit(window uintptr) error {
	if window != 0 {
		w.window = window
		w.ownsWindow = false
	} else {
		// gtk_init_check returns false (rather than aborting) when the windowing
		// system cannot be initialized, e.g. no display. Surface that as an error
		// from NewWindow instead of panicking.
		if !gtkInit() {
			return errors.New("webview: gtk_init_check failed (no display?)")
		}
		w.window = gtkNewWindow()
		if w.frameless {
			// Drop the WM decorations before the window is realized; the page
			// provides the chrome and the drag/resize regions.
			gtkWindowSetDecorated(w.window, false)
		}
		gSignalConnectData(w.window, "destroy", windowDestroyFn, w.id, 0, 0)
	}

	w.webview = webkitWebViewNew()
	gObjectRefSink(w.webview)
	w.manager = webkitWebViewGetUserContentManager(w.webview)

	// load-changed fires on every navigation commit; WEBKIT_LOAD_FINISHED (3)
	// marks the "page fully loaded" moment View.Ready waits for.
	gSignalConnectData(w.webview, "load-changed", loadChangedFn, w.id, 0, 0)
	gSignalConnectData(w.webview, "decide-policy", decidePolicyFn, w.id, 0, 0)
	gSignalConnectData(w.webview, "load-failed", loadFailedFn, w.id, 0, 0)

	gSignalConnectData(w.manager, "script-message-received::__webview__",
		messageHandlerFn, w.id, 0, 0)
	registerScriptHandler(w.manager, "__webview__")

	w.mu.Lock()
	w.rebuildScriptsLocked() // installs the bridge
	w.mu.Unlock()
	return nil
}

// applyBackground makes the window background match View.Frame. Frame
// windows are ordinary opaque OS windows and are left alone (WebKit's default
// white background). Frameless windows are fully transparent by default: the
// web view gets a fully transparent RGBA background, so the page's
// transparent areas reveal the desktop (the page's html/body can set its own
// background).
func (w *webview) applyBackground() {
	if !w.frameless {
		return
	}
	rgba := [4]float64{0, 0, 0, 0}
	if webkitWebViewSetBackgroundColor != nil && w.webview != 0 {
		webkitWebViewSetBackgroundColor(w.webview, &rgba)
	}
	if gtk4 {
		// GTK4 removed the GTK3 mechanisms below (RGBA visual selection,
		// gtk_widget_override_background_color): the theme's CSS paints an
		// opaque background on the toplevel behind the web view, so a
		// transparent web view alone still shows a solid rectangle. Clear the
		// window's own background through a per-window CSS provider instead.
		// Wayland surfaces carry alpha, so with no opaque backdrop painted the
		// desktop shows through; GTK4 X11 windows cannot be per-pixel
		// transparent at all (GTK4 dropped RGBA visuals there).
		if w.ownsWindow {
			makeWindowBackgroundTransparent(w.window)
		}
		return
	}
	if !w.ownsWindow {
		// Embedded windows are already realized; only the web view's own
		// background can be set without touching the host.
		return
	}
	// GTK3: install the RGBA visual before the window is realized (that is
	// what enables alpha blending with the desktop) and paint the window
	// itself transparent so no default flash shows before the page maps.
	screen := gdkScreenGetDefault()
	if vis := gdkScreenGetRGBAVisual(screen); vis != 0 {
		gtkWidgetSetVisual(w.webview, vis)
		gtkWidgetSetVisual(w.window, vis)
	}
	gtkWidgetOverrideBackgroundColor(w.window, 0, &rgba)
}

// gtkStyleProviderPriorityApplication is GTK_STYLE_PROVIDER_PRIORITY_APPLICATION,
// high enough to beat the theme's own window background rule.
const gtkStyleProviderPriorityApplication = 600

// makeWindowBackgroundTransparent attaches a CSS provider to a single GTK4
// toplevel that paints no background on it. Widget-scoped (not display-wide),
// so other windows - a framed window in the same app - keep the theme's
// opaque background. The provider is created with refcount 1 and deliberately
// never released: the style context it is attached to would otherwise be left
// pointing at freed memory on teardown.
func makeWindowBackgroundTransparent(window uintptr) {
	if gtkCssProviderNew == nil || gtkCssProviderLoadFromString == nil ||
		gtkWidgetGetStyleContext == nil || gtkStyleContextAddProvider == nil {
		return // symbol resolution failed; best effort
	}
	provider := gtkCssProviderNew()
	if provider == 0 {
		return
	}
	gtkCssProviderLoadFromString(provider, "window.background { background-color: transparent; }", -1)
	if ctx := gtkWidgetGetStyleContext(window); ctx != 0 {
		gtkStyleContextAddProvider(ctx, provider, gtkStyleProviderPriorityApplication)
	}
}

func (w *webview) onWindowDestroy() {
	// Closed via the OS or Destroy(): reclaim the engine registry entry so the
	// webview is not pinned when Destroy() is never called. unregisterEngine is
	// idempotent, so a later Destroy() is fine; signal callbacks resolve to nil
	// and no-op.
	unregisterEngine(w.id)
	w.window = 0
	dispatchMain(func() { w.stopRunLoop = true })
	// The destroy signal is only connected for owned windows, so this is the
	// single per-window close event the App scope counts (App.Wait).
	appWindowClosed()
}

func (w *webview) Run() {
	ui.enterLoop()
	defer ui.exitLoop()
	w.stopRunLoop = false
	for !w.stopRunLoop {
		gMainContextIteration(0, true)
	}
}

func (w *webview) Terminate() {
	dispatchMain(func() { w.stopRunLoop = true })
}
func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// onUIThread reports whether the caller runs on the thread that owns GTK. It
// is true before any view exists, when there is no other thread to defer to.
func onUIThread() bool {
	t := uiThread.Load()
	return t == 0 || gThreadSelf() == t
}

// Destroy tears the window and web view down. GTK is not thread-safe, so when
// Destroy runs on another goroutine (View.Close is documented safe from any
// goroutine, and bindings run on their own goroutines) the GTK part is
// marshalled onto the UI thread's main context, as the Windows engine does.
// Running it in place raced the main loop and crashed in gtk_window_close or
// g_object_unref. The marshalled teardown runs when the UI thread next
// iterates the default main context; a Close from another goroutine after the
// loop has stopped for good leaves it queued, like every other marshalled
// call here. It never waits, so such a Close cannot hang.
func (w *webview) Destroy() {
	// A window closed before its first load finished (blank window, early
	// close) still owns a temporary loopback server: stop it here - the
	// load-finished path (fireReady) never ran.
	w.releaseLoopback()
	if !onUIThread() {
		dispatchMain(w.destroyOnUI)
		return
	}
	w.destroyOnUI()
}

// destroyOnUI performs the GTK part of Destroy, on the UI thread. See Destroy.
func (w *webview) destroyOnUI() {
	// Remember whether this Destroy actually owns an open window: closing it
	// below reports the close to the App scope, and a Destroy that merely
	// cleans up after an OS close must not report twice (the destroy-signal
	// path already did).
	hadWindow := w.window != 0 && w.ownsWindow
	if w.window != 0 && w.ownsWindow {
		// g_signal_handlers_disconnect_by_data is a macro -> _disconnect_matched.
		gSignalHandlersDisconnectMatched(w.window, gSignalMatchData, 0, 0, 0, 0, w.id)
		gtkWindowClose(w.window)
		w.window = 0
	}
	if w.webview != 0 {
		// Disconnect the manager's script-message handler (matched by data = w.id)
		// before the manager is freed with the web view.
		if w.manager != 0 {
			gSignalHandlersDisconnectMatched(w.manager, gSignalMatchData, 0, 0, 0, 0, w.id)
			w.manager = 0
		}
		gSignalHandlersDisconnectMatched(w.webview, gSignalMatchData, 0, 0, 0, 0, w.id)
		gObjectUnref(w.webview)
		w.webview = 0
	}
	unregisterEngine(w.id)
	if w.ownsWindow {
		// Destroy bypasses the destroy-signal callback (its handlers were
		// disconnected above), so report the window close to the App scope
		// here instead - App.Wait returns when the last window is gone.
		if hadWindow {
			appWindowClosed()
		}
		done := false
		dispatchMain(func() { done = true })
		for i := 0; i < 10000 && !done; i++ {
			gMainContextIteration(0, true)
		}
	}
}

// applySize applies the initial geometry bounds/state to the window and shows
// it (see applyGeometry). It is the creation-time counterpart of the old
// runtime SetSize: window geometry is deliberately fixed after creation.
func (w *webview) applySize(width, height int, state State) {
	gtkWindowSetResizable(w.window, state != StateFixed)
	switch state {
	case StateMin:
		gtkWidgetSetSizeRequest(w.window, width, height)
	case StateMax:
		if !gtk4 { // gtk_window_set_geometry_hints is GTK3/X11-only
			g := gdkGeometry{MaxWidth: int32(width), MaxHeight: int32(height)}
			gtkWindowSetGeometryHints(w.window, 0, &g, gdkHintMaxSize)
		}
	default: // StateNone, StateFixed
		if gtk4 {
			gtkWindowSetDefaultSize(w.window, width, height)
		} else {
			gtkWindowResize(w.window, width, height)
		}
	}
	w.isSizeSet = true
	w.windowShow()
	if w.frameless {
		// Keep the tracker's resize-edge handling in sync with the actual
		// resizability (no-op before the first page has loaded; the tracker's
		// initial state came from View.State).
		w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onAppRegionState({resizable:%v})}", state != StateFixed))
	}
}

// waylandDisplay reports whether the default GDK display is a Wayland one; it
// decides whether the View geometry may be applied at all (a client cannot
// place its own toplevel on Wayland).
func waylandDisplay() bool {
	d := gdkDisplayGetDefault()
	return d != 0 && strings.HasPrefix(cstr(gdkDisplayGetName(d)), "wayland")
}

// applyGeometry realizes an owned window with its creation-time geometry from
// the View: Width/Height (backend default when zero) with State, and - where
// the platform supports it - Left/Top before the window maps.
func (w *webview) applyGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	if !gtk4 && !waylandDisplay() && (v.Left != 0 || v.Top != 0) {
		// gtk_window_move is GTK3/X11-only; GTK4 and Wayland never let a
		// client position its own toplevel.
		gtkWindowMove(w.window, v.Left, v.Top)
	}
	w.applySize(width, height, v.State)
}

// gdkButton converts the DOM MouseEvent.button reported by the app-region
// tracker (0 = primary, 1 = middle, 2 = secondary) into the GDK button
// numbering GDK's begin-move/resize entry points expect (1 = primary, 2 =
// middle, 3 = secondary). Anything else maps to the primary button.
func gdkButton(domButton int32) int32 {
	switch domButton {
	case 1:
		return 2
	case 2:
		return 3
	default:
		return 1
	}
}

// beginMoveDrag starts an interactive window move from the mouse-down the
// tracker reported inside a "drag" box. GTK3 uses gtk_window_begin_move_drag
// with root coordinates (device pixels - the tracker multiplies by the device
// scale); GTK4 removed that entry point and moves the window's GdkSurface via
// gdk_toplevel_begin_move with surface coordinates (logical clientX/clientY).
func (w *webview) beginMoveDrag(p dragRequestParams) {
	if w.window == 0 || !w.frameless {
		return
	}
	button := gdkButton(p.Button)
	if gtk4 {
		w.beginMoveDrag4(button, p.ClientX, p.ClientY, p.Time)
		return
	}
	gtkWindowBeginMoveDrag3(w.window, button, p.ScreenX, p.ScreenY, p.Time)
}

func (w *webview) beginMoveDrag4(button int32, x, y float64, timestamp uint32) {
	surface := gtkNativeGetSurface(w.window)
	if surface == 0 {
		return
	}
	gdkToplevelBeginMove(surface, w.pointerDevice(), button, x, y, timestamp)
}

// beginResizeDrag starts an interactive edge resize (gtk_window_begin_resize_drag
// on GTK3, gdk_toplevel_begin_resize on GTK4) for the hovered edge band
// ("nw"/"n"/.../"w") of a resizable frameless window.
func (w *webview) beginResizeDrag(p dragRequestParams) {
	if w.window == 0 || !w.frameless {
		return
	}
	edge := gdkEdgeFor(p.Direction)
	if edge < 0 {
		return
	}
	button := gdkButton(p.Button)
	if gtk4 {
		surface := gtkNativeGetSurface(w.window)
		if surface == 0 {
			return
		}
		gdkToplevelBeginResize(surface, edge, w.pointerDevice(), button, p.ClientX, p.ClientY, p.Time)
		return
	}
	gtkWindowBeginResizeDrag3(w.window, edge, button, p.ScreenX, p.ScreenY, p.Time)
}

// gdkStateMaximized is the maximized bit of the GdkWindowState (GTK3) and
// GdkToplevelState (GTK4) masks returned by gdk_window_get_state /
// gdk_toplevel_get_state: GDK_WINDOW_STATE_MAXIMIZED = 1<<1 (GTK3) and
// GDK_TOPLEVEL_STATE_MAXIMIZED = 1<<1 (GTK4) share the value.
const gdkStateMaximized = 1 << 1

// toggleMaximize flips the window between maximized and its normal size; the
// app-region tracker asks for it when a "drag" box is double-clicked (see
// view.go). The maximized state is queried from GDK rather than tracked in
// Go: the window manager is the authority, and gtk_window_maximize may be
// refused or pre-empted by the WM. Script-message handlers run on the GTK
// main context, so the GTK/GDK calls are direct, like beginMoveDrag.
func (w *webview) toggleMaximize() {
	if w.window == 0 {
		return
	}
	var state uint32
	if gtk4 {
		surface := gtkNativeGetSurface(w.window)
		if surface == 0 {
			return
		}
		state = gdkToplevelGetState(surface)
	} else {
		gdkWindow := gtkWidgetGetWindow(w.window)
		if gdkWindow == 0 {
			return
		}
		state = gdkWindowGetState(gdkWindow)
	}
	if state&gdkStateMaximized != 0 {
		gtkWindowUnmaximize(w.window)
	} else {
		gtkWindowMaximize(w.window)
	}
}

// pointerDevice resolves the current GdkDevice* pointer, needed by the GTK4
// gdk_toplevel_begin_move/resize signatures.
func (w *webview) pointerDevice() uintptr {
	display := gtkWidgetGetDisplay(w.window)
	if display == 0 {
		return 0
	}
	seat := gdkDisplayGetDefaultSeat(display)
	if seat == 0 {
		return 0
	}
	return gdkSeatGetPointer(seat)
}

// resolveURL maps the uniform app:// origin onto this view's serving origin:
// the loopback-server base configured at creation (App.HTTP on Linux, always
// on macOS), or - no server up - the URL unchanged, so the engine serves the
// app:// scheme natively. Once the temporary server's idle timeout has
// closed it, the dead base is dropped here and later app:// navigations use
// the scheme again. Every other URL passes through untouched.
func (w *webview) resolveURL(url string) string {
	if w.contentBase != "" {
		if w.transient != nil && w.transient.isClosed() {
			w.transient = nil
			w.contentBase = ""
			return url
		}
		return rewriteAppURL(w.contentBase, url)
	}
	return url
}

func (w *webview) Navigate(url string) {
	if w.webview == 0 {
		return // web view destroyed (e.g. a navigation queued before Close).
	}
	if url == "" {
		url = "about:blank"
	}
	// The uniform content origin is "app://" (see App.FS). resolveURL maps it
	// onto this view's serving origin: this window's temporary loopback
	// server's http://localhost base while its initial page loads under
	// App.HTTP (same path, query and fragment, so the page really loads from
	// the HTTP origin), or - with no server up - the engine serves the app://
	// scheme natively.
	url = w.resolveURL(url)
	url = canonicalNavigateURL(url)
	w.trust(url)
	webkitWebViewLoadURI(w.webview, url)
}

func (w *webview) trust(urls ...string) {
	if !w.trustURLs(urls) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rebuildScriptsLocked()
}

// WebKitPolicyDecisionType values.
const (
	policyNavigationAction = 0
	policyNewWindowAction  = 1
	policyResponse         = 2
)

// responseSchemes are the schemes whose top-level loads reach a response,
// where decidePolicy judges them, plus javascript:, which runs in the page.
// A navigation to any other scheme, such as mailto: or a custom one, never
// gets a response, so decidePolicy judges it when it starts.
var responseSchemes = map[string]bool{
	"http": true, "https": true, "data": true, "blob": true, "file": true,
	"about": true, "javascript": true, appSchemeName: true,
}

// decidePolicy applies the navigation policy (see viewCore.navigationPolicy)
// to one decide-policy signal, and reports whether it decided; when it did
// not, WebKit's default decision stands.
//
// WebKitGTK's navigation actions do not say whether they are for the main
// frame or a frame, so a top-level page is judged at its response instead,
// which WebKit marks as the main frame's main resource. By then the request
// has been sent and any redirects followed, but nothing is shown. New windows
// are always top-level, so they are judged when they start, as are schemes
// that have no response to judge. A new window is never opened: see
// handleNewWindow.
func (w *webview) decidePolicy(decision uintptr, decisionType int) bool {
	var request uintptr
	switch decisionType {
	case policyNavigationAction, policyNewWindowAction:
		action := webkitNavigationPolicyDecisionGetNavigationAction(decision)
		if action == 0 {
			return false
		}
		request = webkitNavigationActionGetRequest(action)
	case policyResponse:
		if !webkitResponsePolicyDecisionIsMainFrameMainRes(decision) {
			return false
		}
		request = webkitResponsePolicyDecisionGetRequest(decision)
	default:
		return false
	}
	if request == 0 {
		return false
	}
	uri := cstr(webkitURIRequestGetURI(request))
	if decisionType == policyNavigationAction {
		scheme, _, _ := strings.Cut(uri, ":")
		if responseSchemes[strings.ToLower(scheme)] {
			return false // judged at its response, if it is top-level
		}
	}
	if decisionType == policyNewWindowAction {
		webkitPolicyDecisionIgnore(decision)
		w.handleNewWindow(uri)
		return true
	}
	action := w.navigationPolicy(uri)
	if action == navProceed {
		return false
	}
	webkitPolicyDecisionIgnore(decision)
	refuseNavigation(uri, action)
	return true
}

// WebKit load-failed details: WEBKIT_LOAD_STARTED is the load event a
// provisional load fails in, and WEBKIT_NETWORK_ERROR_CANCELLED is a load
// stopped on purpose.
const (
	loadEventStarted      = 0
	networkErrorCancelled = 302
)

// loadFailed handles a main-frame load that failed before its page
// committed, and reports whether it did; when it did not, WebKit shows its
// error page. A page on an origin the view does not trust, which the policy
// hands to the system, is handed over here too: a link to a host that does
// not resolve fails before any response, so decidePolicy never judged it,
// and would otherwise do nothing. A failure the policy caused (an ignored
// decision) and a cancelled load are left alone, as is a trusted page.
func (w *webview) loadFailed(loadEvent int32, uri string, gerror uintptr) bool {
	if loadEvent != loadEventStarted || gerror == 0 {
		return false
	}
	// GError: GQuark domain (guint32), gint code, gchar *message.
	e := *(*unsafe.Pointer)(unsafe.Pointer(&gerror))
	domain, code := *(*uint32)(e), *(*int32)(unsafe.Add(e, 4))
	if domain == webkitPolicyErrorQuark() ||
		(domain == webkitNetworkErrorQuark() && code == networkErrorCancelled) {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return false
	}
	action := w.navigationPolicy(uri)
	if action != navExternal {
		return false
	}
	refuseNavigation(uri, action)
	return true
}

func (w *webview) loadHTML(html string) {
	w.trust(loadHTMLBase)
	webkitWebViewLoadHTML(w.webview, html, loadHTMLBase)
}

func (w *webview) Init(js string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pushUserScript(js)
}

func (w *webview) Eval(js string) {
	if w.webview == 0 {
		return // web view destroyed (e.g. a late reply dispatched after Destroy).
	}
	if webkitWebViewGetURI(w.webview) == 0 {
		return // URI is null before content has begun loading.
	}
	if haveEvaluateJavascript {
		webkitWebViewEvaluateJavascript(w.webview, js, len(js), 0, 0, 0, 0, 0)
	} else {
		webkitWebViewRunJavascript(w.webview, js, 0, 0, 0)
	}
}

// --- application icon (App.Icon) ------------------------------------------

// Runtime app-icon state (see gtk3InstallAppIcon/gtk4InstallAppIcon below):
// the icon is established once when the app scope opens and kept for the
// life of the process - GTK3 keeps the default-icon pixbuf plus the pixel
// buffer it borrows, GTK4 keeps the GBytes and the GdkTexture list it hands
// to every toplevel. One small icon per app, kept deliberately.
var (
	appIconPixbuf uintptr // GTK3: default window icon (GdkPixbuf)
	appIconPix    []byte  // backing pixels the pixbuf borrows (no destroy notify)
	appIconList   uintptr // GTK4: GList* of GdkTexture icons
	appIconBytes  uintptr // GTK4: GBytes backing the textures
)

// gdkMemoryR8G8B8A8Premultiplied is GDK_MEMORY_R8G8B8A8_PREMULTIPLIED, the
// byte layout gdk_memory_texture_new consumes.
const gdkMemoryR8G8B8A8Premultiplied = 2

// gtk3InstallAppIcon wraps the decoded straight-RGBA icon in a GdkPixbuf and
// installs it as GTK3's default window icon (gtk_window_set_default_icon),
// which every window created afterwards inherits - setAppIcon runs before the
// first window exists. Each owned window additionally receives the pixbuf
// directly at its first show (webview.applyWindowIcon), because some window
// managers only honor a window's own icon. X11 window managers show the icon
// in the taskbar and window switcher; on Wayland the compositor derives icons
// from the .desktop file and ignores window icons.
func gtk3InstallAppIcon(pix []byte, w, h int) error {
	if gdkPixbufNewFromData == nil || gtkWindowSetDefaultIcon == nil {
		return nil // GTK3 build without the gdk-pixbuf symbols: best-effort
	}
	stride := 4 * w
	// gdk_pixbuf_new_from_data borrows the pixel buffer (no destroy notify was
	// given), so the backing slice is retained in appIconPix for the process
	// lifetime and the pixbuf is created from that retained buffer.
	appIconPix = pix
	icon := gdkPixbufNewFromData(unsafe.Pointer(&appIconPix[0]),
		0 /*GDK_COLORSPACE_RGB*/, 1 /*has alpha*/, 8,
		int32(w), int32(h), int32(stride), 0, 0) // #nosec G115 -- icon dimensions bounded by image/png
	if icon == 0 {
		appIconPix = nil
		return errors.New("appkit: application icon: gdk_pixbuf_new_from_data failed")
	}
	// gtk_window_set_default_icon takes its own reference; the pixbuf handle
	// and the backing pixels it borrows stay retained for the process lifetime.
	appIconPixbuf = icon
	gtkWindowSetDefaultIcon(icon)
	return nil
}

// gtk4InstallAppIcon converts the decoded straight-RGBA icon into a
// premultiplied GdkTexture and stores it in the app-wide list that
// applySurfaceIcon hands to every toplevel surface at its first show. GDK
// maps the list to the X11 _NET_WM_ICON property; surfaces without icon
// support (Wayland) ignore it, matching the best-effort App.Icon contract.
func gtk4InstallAppIcon(pix []byte, w, h int) error {
	if !haveGdkIcons {
		return nil // older GTK4 without the surface-icon entry points
	}
	stride := 4 * w
	premul := make([]byte, stride*h)
	// Straight RGBA -> premultiplied RGBA, with the rounding image/png uses.
	for i := 0; i+3 < len(pix); i += 4 {
		r, g, b, a := pix[i], pix[i+1], pix[i+2], pix[i+3]
		if a == 255 {
			premul[i], premul[i+1], premul[i+2], premul[i+3] = r, g, b, a
			continue
		}
		aa := uint32(a)
		premul[i] = uint8((uint32(r)*aa + 127) / 255)
		premul[i+1] = uint8((uint32(g)*aa + 127) / 255)
		premul[i+2] = uint8((uint32(b)*aa + 127) / 255)
		premul[i+3] = a
	}
	// g_bytes_new copies the pixels; the texture keeps the GBytes alive, and
	// holding our own reference (appIconBytes) keeps the lifetime unambiguous.
	appIconBytes = gBytesNew(unsafe.Pointer(&premul[0]), uintptr(len(premul)))
	if appIconBytes == 0 {
		return errors.New("appkit: application icon: g_bytes_new failed")
	}
	tex := gdkMemoryTextureNew(int32(w), int32(h), gdkMemoryR8G8B8A8Premultiplied, appIconBytes, uintptr(stride)) // #nosec G115 -- icon dimensions bounded by image/png
	if tex == 0 {
		appIconBytes = 0
		return errors.New("appkit: application icon: gdk_memory_texture_new failed")
	}
	appIconList = gListAppend(appIconList, tex)
	return nil
}

// applySurfaceIcon hands the GTK4 application icon to this window's toplevel
// surface. It runs after the first show (windowShow) and after every Show, so
// a surface that only materialized later still receives the icon.
func (w *webview) applySurfaceIcon() {
	if !gtk4 || w.window == 0 || appIconList == 0 {
		return
	}
	surface := gtkNativeGetSurface(w.window)
	if surface == 0 {
		return
	}
	gdkToplevelSetIconList(surface, appIconList)
}

// applyWindowIcon hands the GTK3 application icon to this window as its own
// pixbuf icon (gtk_window_set_icon), complementing the default window icon:
// some window managers only honor a window's explicit icon, so the app icon
// is applied directly to every owned window before it is first shown. X11
// window managers display it in the taskbar/switcher; Wayland compositors
// derive icons from the .desktop file and ignore window icons.
func (w *webview) applyWindowIcon() {
	if gtk4 || w.window == 0 || appIconPixbuf == 0 || gtkWindowSetIcon == nil {
		return
	}
	gtkWindowSetIcon(w.window, appIconPixbuf)
}

func (w *webview) windowShow() {
	if w.isWindowShown {
		return
	}
	if gtk4 {
		gtkWindowSetChild(w.window, w.webview)
		gtkWidgetSetVisible(w.webview, true)
	} else {
		gtkContainerAdd(w.window, w.webview)
		gtkWidgetShow(w.webview)
		if w.ownsWindow {
			// GTK3: give the window its own icon (the default window icon is
			// also set at app start) before it is first shown, so the window
			// manager reads the icon when the window maps.
			w.applyWindowIcon()
		}
	}
	if w.ownsWindow {
		gtkWidgetGrabFocus(w.webview)
		if gtk4 {
			gtkWidgetSetVisible(w.window, true)
		} else {
			gtkWidgetShow(w.window)
		}
		w.applyFramelessCSD()
	}
	w.isWindowShown = true
	w.applySurfaceIcon() // GTK4: the surface now exists - hand it the icon list
}

// announceCSD is gdk_wayland_window_announce_csd (an exported but
// non-public GTK3 symbol), resolved lazily so the library also runs on GTK3
// builds that do not ship it.
var (
	announceCSDOnce sync.Once
	announceCSD     func(window uintptr)
)

// applyFramelessCSD keeps a frameless GTK3 window frameless on Wayland.
//
// gtk_window_set_decorated(false) is honored by X11 window managers (they see
// the motif hints), but GDK's Wayland backend ignores it: the decoration mode
// is instead negotiated with the compositor via the
// org_kde_kwin_server_decoration protocol, and an undecorated GtkWindow
// announces SERVER-side decorations (gtk_window_should_use_csd() returns FALSE
// for it), so KWin draws its own frame - a visible border around an otherwise
// frameless window. Re-announcing client-side decorations flips the mode to
// CLIENT; GTK itself still draws nothing (its client_decorated flag is off, so
// no CSD title bar or shadow), and the compositor then draws nothing either.
//
// It is a no-op on X11 and on compositors without the decoration protocol, and
// the raw symbol is never called on a non-Wayland GdkWindow (where its
// wl_surface would be NULL).
func (w *webview) applyFramelessCSD() {
	if gtk4 || !w.frameless || !w.ownsWindow || w.window == 0 {
		return
	}
	announceCSDOnce.Do(func() {
		addr, err := pure.Dlsym(gtkLib, "gdk_wayland_window_announce_csd")
		if err == nil && addr != 0 {
			pure.RegisterFunc(&announceCSD, addr)
		}
	})
	if announceCSD == nil {
		return
	}
	name := cstr(gdkDisplayGetName(gdkDisplayGetDefault()))
	if !strings.HasPrefix(name, "wayland") {
		return
	}
	gw := gtkWidgetGetWindow(w.window)
	if gw == 0 {
		return
	}
	announceCSD(gw)
}

func (w *webview) Focus() {
	if w.webview == 0 {
		return
	}
	// GtkWindow remembers its focus widget and restores it on re-activation, so
	// the first-show grab in windowShow already covers Alt-Tab; this is the
	// explicit, on-demand version.
	gtkWidgetGrabFocus(w.webview)
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	// gtk_window_present raises the window and asks the window manager for
	// focus. What the WM actually grants is its business - some refuse focus
	// stealing and flash the taskbar entry instead, which is the right call on
	// a desktop the user configured that way.
	gtkWindowPresent(w.window)
}

// Show re-maps a hidden or minimized window and presents it. dispatchMain is
// used so the call is safe from any goroutine (see the View doc).
//
// A minimized (iconified) window is de-iconified FIRST: presenting an
// iconified window makes the window manager flash its taskbar entry instead
// of showing it, which is exactly what must not happen when Show is used to
// bring a window back.
func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() {
		if gtk4 {
			gtkWindowUnminimize(w.window)
		} else {
			gtkWindowDeiconify(w.window)
		}
		if gtk4 {
			gtkWidgetSetVisible(w.window, true)
		} else {
			gtkWidgetShow(w.window)
		}
		gtkWindowPresent(w.window)
		w.applySurfaceIcon() // in case the surface only appeared on this show
	})
}

// Hide un-maps the window, which removes it from the screen AND from the
// taskbar / window list.
func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() {
		if gtk4 {
			gtkWidgetSetVisible(w.window, false)
		} else {
			gtkWidgetHide(w.window)
		}
	})
}

// Maximize asks the window manager to maximize the window (a desktop may
// refuse; gtk_window_maximize itself cannot fail).
func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { gtkWindowMaximize(w.window) })
}

// onGTK34 runs the GTK4 symbol on a GTK4 stack and the GTK3 one otherwise -
// GTK4 renamed several window-state calls (iconify/deiconify became
// minimize/unminimize).
func (w *webview) onGTK34(gtk4Fn, gtk3Fn func(uintptr)) {
	if gtk4 {
		gtk4Fn(w.window)
	} else {
		gtk3Fn(w.window)
	}
}

// Minimize iconifies (GTK3) / minimizes (GTK4) the window to the taskbar.
func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { w.onGTK34(gtkWindowMinimize, gtkWindowIconify) })
}

// Unminimize deiconifies (GTK3) / unminimizes (GTK4) the window. No-op when
// the window is not minimized.
func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { w.onGTK34(gtkWindowUnminimize, gtkWindowDeiconify) })
}

// Unmaximize restores a maximized window to its previous normal size. No-op
// when the window is not maximized.
func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { gtkWindowUnmaximize(w.window) })
}

// updateBindings changes the binding table and rebuilds the user scripts
// under one hold of mu (see engine.updateBindings).
func (w *webview) updateBindings(mutate func(bindings map[string]binding) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := mutate(w.bindings); err != nil {
		return err
	}
	w.rebuildScriptsLocked()
	return nil
}

// --- user scripts + message routing ----------------------------------------

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

// rebuildScriptsLocked re-injects the bridge, Init() scripts and the current
// bind script in order. Assumes w.mu is held.
func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	webkitUserContentManagerRemoveAllScripts(w.manager)
	addUserScript(w.manager, w.bridgeScriptLocked(bridgePostFn))
	for _, src := range w.userScriptSrcs {
		addUserScript(w.manager, src)
	}
	addUserScript(w.manager, createBindScript(w.bindingEntriesLocked()))
}
func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
}

func (w *webview) handleInternal(method string, params json.RawMessage) bool {
	// Script-message handlers run on the GTK main context, so the window
	// drags below can call GTK directly.
	switch method {
	case internalWindowDrag:
		w.beginMoveDrag(parseDragRequest(params))
	case internalWindowResize:
		w.beginResizeDrag(parseDragRequest(params))
	case internalWindowToggleMaximize:
		// Double-click on a drag box (see the tracker in view.go): flip
		// between maximized and normal. The tracker exists only on frameless
		// windows, so a framed window ignores the message.
		if w.frameless {
			w.toggleMaximize()
		}
	default:
		return false
	}
	return true
}

// interceptOutsideLinks: WebKitGTK judges a top-level page only at its
// response (see decidePolicy), after the request has gone out, so the bridge
// hands the navigations a page visibly starts to Go before they are requested
// (see initOutsideLinks and webview.openOutside).
const interceptOutsideLinks = true

// bridgePostFn for the WebKit backends (macOS WKWebView, Linux WebKitGTK): the
// script message handler registered under the name "__webview__".
const bridgePostFn = `function(message) {
  return window.webkit.messageHandlers.__webview__.postMessage(message);
}`

// newView creates a window and its web view on Unix (Linux, FreeBSD,
// NetBSD). The App.Show method
// opens the app scope first and then calls this constructor with the
// committed App.FS; the meaning of opts (Debug, Window, ...) is
// documented there. The first successful call pins the calling goroutine to
// its OS thread.
func newView(v *View, serve serveFunc) (*webview, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(func() {
		runtime.LockOSThread()
		uiThread.Store(gThreadSelf())
	})

	w := &webview{
		ownsWindow: true,
		frameless:  !v.Frame,
		bindings:   map[string]binding{},
		serve:      serve,
	}
	w.id = registerEngine(w)
	err = w.windowInit(uintptr(v.window))
	if err != nil {
		unregisterEngine(w.id)
		return nil, err
	}
	err = w.registerSchemes()
	if err != nil {
		w.Destroy()
		return nil, err
	}
	// Window settings: apply appkit's tuned WebKitSettings right after the
	// web view is created (WebKitSettings changes only take effect on the
	// next navigation). Page JavaScript is always on (WebKit's native
	// default). Media-stream and clipboard access are native-OFF in
	// WebKitGTK; appkit enables them so the page can use
	// navigator.mediaDevices (getUserMedia / getDisplayMedia) and the
	// Copy/Paste bindings behave. The debug-driven pair - the dev-tools
	// switch and console forwarding - tracks the view's resolved Debug flag
	// (View.Debug OR App.Debug / APPKIT_DEBUG). Every other WebKitSettings
	// property keeps the loaded library's own compiled-in defaults.
	st := webkitWebViewGetSettings(w.webview)
	webkitSettingsSetEnableMediaStream(st, true)
	webkitSettingsSetJavascriptCanAccessClipboard(st, true)
	webkitSettingsSetEnableJavascript(st, true)
	webkitSettingsSetEnableWriteConsoleToStdout(st, v.Debug)
	webkitSettingsSetEnableDeveloperExtras(st, v.Debug)
	// The window background is transparent by default: set the web view's and
	// the window's background to transparent before the window is realized
	// (windowShow), so the RGBA visual - required for alpha blending with the
	// desktop - is in place before the first frame.
	w.applyBackground()
	if w.frameless && w.ownsWindow {
		// Track the page's -app-region boxes: mouse-downs in a "drag"
		// box start gtk_window_begin_move_drag, and the edge bands of a
		// resizable window get the proper resize cursors and
		// gtk_window_begin_resize_drag. Runs at document-start of every
		// navigation so the regions follow the content.
		w.pushUserScript(createAppRegionScript(v.State != StateFixed, false, "linux"))
	}
	if w.ownsWindow {
		// Apply the creation-time geometry (the View fields; the backend
		// default size when Width/Height are zero), which realizes and shows
		// the window. newView already runs on the UI thread, so this is done
		// synchronously. Window geometry is fixed after creation - there is no
		// runtime move/resize API.
		w.applyGeometry(v)
	}
	// Per-view serving origin: SCHEME-FIRST on Linux - the registered custom
	// "app" scheme serves the content (see registerSchemes), so no loopback
	// server exists unless App.HTTP opts this window into the temporary
	// http://localhost origin (viewContentBase decides from the committed
	// App.HTTP setting). WebKitGTK cannot attach the cross-origin-isolation
	// headers to scheme responses, so a scheme-served Linux page is not
	// crossOriginIsolated - SharedArrayBuffer still works because
	// JSC_useSharedArrayBuffer is enabled (see ensureInit); the loopback
	// origin (App.HTTP) delivers the headers. A started server is stopped
	// again by releaseLoopback once the window's first load finishes. A
	// start failure tears the freshly created window down.
	w.contentBase, w.transient, err = viewContentBase(v, false)
	if err != nil {
		w.Destroy()
		return nil, err
	}
	return w, nil
}

// --- app-level run loop (App.Wait) -----------------------------------------

// appUIWait runs one iteration of the GTK main context; App.Wait loops on it
// until the app scope asks to exit.
func appUIWait() {
	gMainContextIteration(0, true)
}

// appUIWake wakes a blocked appUIWait from another goroutine (App.Exit).
func appUIWake() {
	dispatchMain(func() {})
}
