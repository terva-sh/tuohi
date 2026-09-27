// Package tray puts an icon with a menu in the system tray / menu bar, with
// no cgo and no bundled libraries.
//
// Build a Config and hand it to Run; Run blocks driving the OS event loop
// until Stop is called. Each backend binds what the OS already ships -
// NSStatusItem on macOS, Shell_NotifyIconW on Windows, a D-Bus
// StatusNotifierItem plus a com.canonical.dbusmenu export on Linux.
//
//	err := tray.Run(tray.Config{
//		Icon:    appIconPNG,
//		Tooltip: "myapp",
//		Items: []tray.Item{
//			{Label: "Open", OnClick: openUI},
//			{Separator: true},
//			{Label: "Quit", OnClick: tray.Stop},
//		},
//	})
//
// Config is fully declarative and read exactly once by Set/Run: icon,
// tooltip, tray-level click handlers and the whole menu tree. The menu stays
// fixed for the tray's lifetime - there is no imperative handle and no
// dynamic-update machinery to change items after the icon is up (a tray is a
// launcher, not a live dashboard). Menu state that a native checkbox owns
// (Item.Checked) starts from the Config and toggles when the user clicks the
// item; the backend tracks that one moving part.
//
// Threading: Run owns the process's UI event loop, so it must be called from
// the main goroutine, locked to the main OS thread:
//
//	func main() {
//		runtime.LockOSThread()
//		tray.Run(cfg)
//	}
//
// A menu item's OnClick runs on that UI thread; keep it short or hand the
// work to another goroutine. Stop, by contrast, is safe to call from any
// goroutine.
//
// Set/Remove are the non-loop-owning pair for hosts that already run the UI
// event loop - an appkit.App, for example. Set shows the icon and menu
// (call it on the UI thread before the loop runs), the host's own loop
// dispatches the menu events, and Remove hides the icon without touching
// that loop. An appkit.App with Tray set wires exactly this up around
// App.Wait.
//
// Only one tray may be active per process. Bounds reports the icon's
// on-screen rectangle where the backend exposes it (Windows); macOS and
// Linux report zeros.
package tray

import "errors"

// ErrUnsupported is returned by Run and Set on a platform with no tray
// backend.
var ErrUnsupported = errors.New("tray: not supported on this platform")

// ErrAlreadyRunning is returned by Run and Set when a tray is already active
// in this process; only one tray may run at a time.
var ErrAlreadyRunning = errors.New("tray: already running")

// Config describes the tray icon, its tray-level click handlers and its
// menu. It is read once by Set/Run.
type Config struct {
	// Icon is a PNG image for the tray / menu bar. macOS renders it scaled
	// to fit and falls back to Title when it is empty; Windows converts it
	// to an HICON with CreateIconFromResourceEx; Linux ships it as the SNI
	// IconPixmap (ARGB32). It is always safe to set.
	Icon []byte

	// DarkModeIcon is an alternative PNG shown in dark mode. On Windows the
	// tray picks it from the system theme (HKCU Personalize) at Set time and
	// switches live when the theme changes (WM_SETTINGCHANGE). macOS and
	// Linux adapt the single Icon / TemplateIcon to the theme themselves and
	// ignore this field.
	DarkModeIcon []byte

	// TemplateIcon is a macOS template image: a monochrome glyph the system
	// recolors for the current menu bar theme (light or dark). macOS only;
	// other platforms ignore it.
	TemplateIcon []byte

	// AppName names the application in desktop-integration contexts. Linux
	// uses it as the StatusNotifierItem identity (Id); desktop notifications
	// carry their own app name via notify.Show. macOS and Windows
	// derive the application identity from the OS and ignore this field.
	AppName string

	// Title is a short text label. macOS shows it in the menu bar (next to
	// the icon, or alone when Icon is empty). Windows ignores it. Linux uses
	// it as the SNI tooltip fallback when Tooltip is empty.
	Title string

	// Tooltip is shown on hover.
	Tooltip string

	// OnClick runs when the user activates the tray icon with its primary
	// action: a left click on Windows, an SNI Activate on Linux. On macOS
	// the status item opens its menu on click instead, so OnClick only fires
	// when no menu (Items) is attached.
	OnClick func()

	// OnDoubleClick runs when the user double-clicks the tray icon (Windows
	// left double-click, Linux SNI SecondaryActivate). macOS does not
	// deliver double-clicks for status items.
	OnDoubleClick func()

	// OnRightClick runs when the user right-clicks the tray icon. Windows
	// also shows the context menu on a right click, and Linux may route
	// right-click menu display through the SNI ContextMenu method.
	OnRightClick func()

	// Items are the menu entries, top to bottom. nil or empty attaches no
	// menu.
	Items []Item
}

// Item is one entry in the tray menu.
type Item struct {
	// Label is the menu text. Ignored when Separator is true.
	Label string

	// Icon is a PNG shown next to the item on macOS (NSImage) and Linux
	// (dbusmenu icon-data). Windows menus are text-only (owner-drawn item
	// icons are out of scope) and ignore this field.
	Icon []byte

	// Checkbox declares a check item: it shows a checkmark per Checked and
	// toggles it on every click (the backend keeps the runtime state) before
	// OnClick runs, matching native checkbox order. Without Checkbox the
	// item is a plain command and its checkmark, if any, is static.
	Checkbox bool

	// Checked is the initial checkmark state. Meaningful on checkbox items
	// (starting mark) and on plain items (a static mark). Ignored for
	// separators and submenu containers.
	Checked bool

	// Disabled greys the item out and suppresses OnClick.
	Disabled bool

	// Separator makes this a divider line instead of a clickable item; all
	// other fields are ignored.
	Separator bool

	// Submenu holds the nested menu entries shown in a flyout to the right
	// of the item. Submenus nest to any depth on macOS (NSMenu) and Linux
	// (dbusmenu). Windows supports one level of submenu via MF_POPUP; deeper
	// nesting is flattened onto that level.
	Submenu []Item

	// OnClick is called on the UI thread when the item is chosen.
	OnClick func()
}

// Set shows the tray icon and menu WITHOUT owning the process's UI event
// loop: the host's run loop (an auto-run appkit App or a game, anything) keeps
// dispatching events, and menu clicks arrive as usual. Call Set from the UI
// thread, before that loop runs, and pair it with Remove. It returns
// ErrUnsupported on platforms with no backend and ErrAlreadyRunning if a
// tray is already active.
func Set(cfg Config) error { return set(cfg) }

// Remove hides the tray that Set showed. It is safe to call from any
// goroutine, is a no-op when no tray is active, and leaves the host's run
// loop running.
func Remove() { remove() }

// Run shows the tray and drives the OS event loop until Stop is called. It
// blocks and must be called from the main goroutine (see the package doc on
// threading). It returns ErrUnsupported on platforms with no backend and
// ErrAlreadyRunning if a tray is already active.
func Run(cfg Config) error { return run(cfg) }

// Stop hides the tray and makes Run return. It is safe to call from any
// goroutine and is a no-op when no tray is running.
func Stop() { stop() }

// Bounds returns the on-screen rectangle of the active tray icon
// (x, y, width, height) in pixels. Windows reports the real rectangle via
// Shell_NotifyIconGetRect; macOS and Linux do not expose one and return
// zeros. It is a no-op pair with Set/Remove and returns zeros when no tray
// is active.
func Bounds() (x, y, w, h int) { return bounds() }
