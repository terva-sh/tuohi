# tray

Put an icon with a menu in the system tray / menu bar, cgo-free. Each backend
binds what the OS already ships - `NSStatusItem` on macOS, `Shell_NotifyIconW`
on Windows, a D-Bus `StatusNotifierItem` + `com.canonical.dbusmenu` export on
Linux - with no C toolchain and no bundled native libraries.

The tray supports PNG icons with light/dark/macOS-template variants,
checkbox/submenu/disabled menu items, per-item icons, tray-level click
handlers, dark-mode icon switching and (on Windows) on-screen icon bounds.

```go
import "github.com/malivvan/appkit/tray"

err := tray.Run(tray.Config{
	Icon:    appIconPNG, // PNG bytes
	Tooltip: "myapp",
	Items: []tray.Item{
		{Label: "Open", OnClick: openUI},
		{Separator: true},
		{Label: "Quit", OnClick: tray.Stop},
	},
})
```

## API

| Func | Description |
| --- | --- |
| `Run(cfg Config) error` | Show the tray and own the OS event loop; blocks until `Stop`. Must be called on the main goroutine, locked to the main thread. |
| `Stop()` | Hide the tray and make `Run` return. Safe from any goroutine; no-op when idle. |
| `Set(cfg Config) error` | Show the tray WITHOUT owning the event loop (a host such as an appkit `App` or another loop already runs). Call on the UI thread, pair with `Remove`. |
| `Remove()` | Hide a `Set` tray, leaving the host loop running. Safe from any goroutine; no-op when idle. |
| `Bounds() (x, y, w, h int)` | On-screen rectangle of the active icon (Windows via `Shell_NotifyIconGetRect`); zeros elsewhere. |
| `ErrUnsupported` | Sentinel returned by `Run`/`Set` on a platform with no backend. |
| `ErrAlreadyRunning` | Sentinel returned by `Run`/`Set` when a tray is already active - only one tray per process. |

The `Config` is **fully declarative and read once**: PNG `Icon` (plus
`DarkModeIcon` for Windows theme switching and `TemplateIcon`, the monochrome
image macOS recolors for the menu bar), `AppName`, `Title`, `Tooltip`,
tray-level `OnClick`/`OnDoubleClick`/`OnRightClick`, and `Items []Item`. An
`Item` is `Label`, `Icon`, `Checkbox` (toggles its mark on click, keeping the
runtime state), `Checked` (initial/static mark), `Disabled`, `Separator`,
`Submenu []Item` (nested to any depth on macOS/Linux) and `OnClick`. There
are no imperative handles to mutate the menu after `Run`/`Set`, and no
dynamic-update machinery: a tray is a static launcher, so plan its menu up
front and keep it fixed for the tray's lifetime.

## Threading

`Run` owns the process's UI event loop, so it must be called from the main
goroutine, locked to the main OS thread:

```go
func main() {
	runtime.LockOSThread()
	tray.Run(cfg)
}
```

A menu item's `OnClick` (and the tray-level click handlers) run on that UI
thread; keep them short or hand work to another goroutine. `Stop` and `Remove`
are safe from any goroutine.

## Platforms

| OS | Backend | Status |
| --- | --- | --- |
| macOS | `NSStatusItem` + `NSMenu` via the Objective-C runtime (pure) | supported |
| Windows | `Shell_NotifyIconW` popup menu (pure) | supported |
| Unix (Linux, FreeBSD, NetBSD) | D-Bus `StatusNotifierItem` + `com.canonical.dbusmenu` (godbus) | supported on Linux; FreeBSD/NetBSD compile only; needs a session bus at runtime |
| others | - | `Run`/`Set` return `ErrUnsupported` |

Check the unsupported case with `errors.Is(err, tray.ErrUnsupported)`.

### Per-OS behavior notes

- **macOS** - the status item opens its menu on click, so `Config.OnClick`
  fires only when no menu (`Items`) is attached; there is no separate right
  click or double click. `Icon` renders scaled to fit, `Title` appears next
  to it (or alone), and `TemplateIcon` wins over `Icon`. Item icons and
  checkbox marks render natively.
- **Windows** - left click runs `OnClick`, a double click runs
  `OnDoubleClick`, and a right click runs `OnRightClick` and opens the menu.
  `DarkModeIcon` is picked from the system theme at `Set` time and switches
  live on `WM_SETTINGCHANGE`. `Icon` becomes an `HICON` via
  `CreateIconFromResourceEx`; item icons are not supported (owner-drawn menus
  are out of scope). Submenus nest one level (`MF_POPUP`).
- **Linux** - needs a session D-Bus (a desktop session provides one); the icon
  is the SNI `IconPixmap` (ARGB32), the tooltip the SNI `Title`/`ToolTip`, and
  the menu is served through the dbusmenu `GetLayout`/`Event` protocol.
  Checkboxes toggle via `ItemsPropertiesUpdated`. `Activate` → `OnClick`,
  `SecondaryActivate` → `OnDoubleClick`, `ContextMenu` → `OnRightClick`.

Notifications are deliberately not part of this package - see
[`../notify`](../notify/) (`App.Notify`).

## Example

A runnable demo lives in [`demo/`](demo/): icons (light + dark + macOS
template), click handlers, a nested submenu, a checkbox and a notification
item:

```bash
go run ./tray/demo
```

The menu is deliberately **static**: the Config is read once by `Set`/`Run`,
so nothing about the icon or its menu changes at runtime (the only moving
part is a checkbox's own mark toggling on click, which the backend tracks).
A tray is a launcher, not a live dashboard. There is exactly one per
application - a second `Set`/`Run` returns `ErrAlreadyRunning`.

## Conventions

Part of the appkit module. Public API lives in the tag-free `tray.go`,
per-platform backends in `tray_{darwin,windows,linux}.go`, and
`tray_other.go` returns `ErrUnsupported` so every `GOOS` builds. No
`internal/` packages.
