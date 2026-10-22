# tray

Put an icon with a menu in the system tray / menu bar, cgo-free. Each backend
binds what the OS already ships - `NSStatusItem` on macOS, `Shell_NotifyIconW`
on Windows, a D-Bus `StatusNotifierItem` + `com.canonical.dbusmenu` export on
Linux - with no C toolchain and no bundled native libraries.

The tray supports PNG icons with light/dark/macOS-template variants,
checkbox/submenu/disabled menu items, per-item icons, tray-level click
handlers, dark-mode icon switching and (on Windows) on-screen icon bounds.

```go
import "github.com/terva-sh/tuohi/tray"

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
| `Set(cfg Config) error` | Show the tray WITHOUT owning the event loop (a host such as a tuohi `App`, from `App.Start`, or another loop already runs). Call on the UI thread, pair with `Remove`. |
| `Remove()` | Hide a `Set` tray, leaving the host loop running. Safe from any goroutine; no-op when idle. |
| `Update(ctx context.Context, changes Changes) error` | Patch icons/text/menu on the active tray, with cancellation and lifetime checks. |
| `CurrentAvailability() Availability` | Snapshot of registration/host state; visibility is not guaranteed. |
| `Bounds() (x, y, w, h int)` | On-screen rectangle of the active icon (Windows via `Shell_NotifyIconGetRect`); zeros elsewhere. |
| `ErrUnsupported` | Sentinel returned by `Run`/`Set` on a platform with no backend. |
| `ErrAlreadyRunning` | Sentinel returned by `Run`/`Set` when a tray is already active - only one tray per process. |

`Set`/`Run` copy the initial `Config` and recursively copy icons/menus. Existing
static consumers keep their behavior. `Update(ctx, Changes)` patches the active
tray without replacing its status item or changing Dock policy/identity/click
handlers. PNG data is validated before native changes.

```go
items := []tray.Item{
    {Label: "Open", OnClick: openUI},
    {Label: "Muted", Checkbox: true, Checked: muted, OnClick: toggleMute},
    {Label: "Mark read", Disabled: unread == 0, OnClick: markRead},
}
tooltip := "New activity"
err := tray.Update(ctx, tray.Changes{
    Icon: unreadPNG, DarkModeIcon: unreadDarkPNG,
    Tooltip: &tooltip, Items: &items,
})
```

Nil patch fields preserve current values. Non-nil empty icon slices clear that
variant; pointer strings can clear text. `Items` replaces the entire menu and
its callbacks/checked/disabled state; a pointer to an empty slice removes it.
Omit `Items` to preserve native checkbox toggles. `Submenu` nests items, and
`Separator` inserts a separator. `AppName`, `Dock`, tray-level click handlers
and `OnAvailability` remain startup settings. Linux/Windows resolve their
initial tooltip fallback once; explicit updates can subsequently clear it.

Calls copy their input before queuing. Cancellation drops work still queued;
work admitted by the native thread may finish after cancellation and cannot be
rolled back. A queued update is tied to its tray lifetime and cannot affect a
later tray. `ErrNotRunning` means that lifetime ended; `ErrUnavailable` reports
a missing native registration/connection. With a Windows popup open, an
external menu update waits until tracking ends (bounded by ctx); a synchronous
menu update from that UI thread returns `ErrMenuOpen`. Icons/tooltips can still
update while tracking. Give calls from worker goroutines a deadline, especially
before the host starts its loop.

## Registration and host availability

`CurrentAvailability()` returns `Generation`, `Running`, `Registered`,
`HostPresent` and `Err`. `Config.OnAvailability` observes changes on a serial
worker, outside native/UI locks. Rapid changes may coalesce. Removal drops
queued callbacks; an already admitted callback may finish. An old-generation
callback can finish after another tray is created, so consult the current state
before changing background policy.

```go
state := tray.CurrentAvailability()
canHide := state.Running && state.Registered && state.HostPresent && state.Err == nil
// Keep the only window reachable when canHide is false. Background-running
// policy, fallback UI and how to recover a hidden window belong to the app.
```

This is a conservative prerequisite, never proof that the icon is visible.
Hosts can suppress icons, put them in overflow, or stop responding after a
snapshot. A successful `Set` alone is insufficient for close-to-tray policy.

- Linux subscribes before initial registration, registers against the watcher's
  unique owner, and separately queries `IsStatusNotifierHostRegistered`.
  Missing watchers, rejection, no host, owner replacement, host changes and bus
  disconnection are observable. Registration failures retry on changes and
  periodically; watcher calls have a shared three-second deadline. A lost
  session bus requires removing/recreating the tray; watcher restarts recover
  within the same tray lifetime. `Set` preserves its nonfatal missing-watcher
  behavior. GNOME may need an extension; WSLg and nested desktop sessions may
  have no host. None of those environments is assumed usable merely because
  SNI export succeeded.
- macOS reports successful status-item creation. It has no separate tray-host
  protocol or guarantee that menu-bar customization leaves the item visible.
- Windows reports Shell_NotifyIcon registration, probes it every two seconds,
  and re-registers after taskbar restart or a failed probe. Probes, native
  callbacks and updates need the owning message loop to run. State can be
  stale between probes or while that loop is blocked; registration does not
  prove visibility in the notification area.

## Threading

`Run` owns the process's UI event loop, so it must be called from the main
goroutine, locked to the main OS thread:

```go
func main() {
	runtime.LockOSThread()
	tray.Run(cfg)
}
```

macOS/Windows click callbacks run on that UI thread. Linux click callbacks run
on D-Bus handler goroutines and can overlap. Keep callbacks short or hand work
to a worker. `Update`, `Stop` and `Remove` are safe from any goroutine.
Off-thread Windows removal queues native teardown; keep servicing the host
loop until it completes. macOS removal marshals synchronously to its main
thread. Neither attached mode stops the host's loop.

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
  checkbox marks render natively. The tray makes the application a menu-bar
  app, with no Dock icon and no place in the application switcher, unless
  `Dock` is set.
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
[`../notify`](../notify/).

## Example

A runnable example lives in [`examples/tray`](../examples/tray/): icons (light + dark + macOS
template), click handlers, a nested submenu, a checkbox and a notification
item:

```bash
cd examples && go run ./tray
```

The example changes unread icons, tooltip and checked/disabled menu state at
runtime and prints registration/host changes. There is exactly one tray per
application; a second `Set`/`Run` returns `ErrAlreadyRunning`.

Private Linux D-Bus tests exercise actual SNI/dbusmenu and watcher wire
behavior, including cancellation, stale IDs and host recovery. They do not
prove a real panel displayed an icon. This development host cannot execute
macOS/Windows/BSD native behavior; cross builds establish compilation only.

## Conventions

Part of the tuohi module. Public API lives in the tag-free `tray.go`,
per-platform backends in `tray_{darwin,windows,unix}.go`, and
`tray_other.go` returns `ErrUnsupported` so every `GOOS` builds. No
`internal/` packages.
