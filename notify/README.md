# notify

Show OS-level desktop notifications, alerts and beeps, cgo-free and
standalone - no tray icon and no window required. Each platform binds the
notification service the OS ships: `NSUserNotificationCenter` on macOS, a
`Shell_NotifyIconW` balloon on Windows, `org.freedesktop.Notifications` over
D-Bus on Linux.

```go
import "github.com/terva-sh/tuohi/notify"

err := notify.Show("backup", "done", "Snapshot finished at 09:41")
```

Beyond the plain **name + title + message** notification, `ShowOpts` takes an
`Options` struct for a custom **icon** and an **urgency** level, `Alert`
posts a critical notification with the platform's attention sound, and
`Beep` sounds a tone directly. These helpers are read-only; the managed client
below adds activation and lifecycle control. The `name` argument identifies the source application shown by the
desktop (Linux `org.freedesktop.Notifications` `app_name`); an empty name
falls back to the running executable's name, and macOS/Windows derive the
identity from the OS and ignore it.

```go
err := notify.ShowOpts("backup", "Snapshot complete", "All files copied.",
	notify.Options{Icon: "/usr/share/icons/.../dialog-information.png", Urgency: notify.UrgencyNormal})

err := notify.Alert("backup", "Snapshot failed", "The disk is full.",
	notify.Options{Icon: "dialog-error"}) // urgency forced to UrgencyCritical + sound

err := notify.Beep(notify.DefaultFreq, notify.DefaultDuration)
```

## Managed notifications

`NewClient(ctx, Config)` adds opt-in correlation, activation, replacement and
dismissal. The existing `Show`/`ShowOpts` helpers retain their behavior. A
managed client never falls back to a read-only CLI or balloon and reports
`ErrUnsupported` when a requested capability cannot be honored.

```go
client, err := notify.NewClient(ctx, notify.Config{
    Name: "my app",
    OnActivate: func(id string) {
        // Resolve this opaque key in a Go-owned account/room map, then
        // app.Show(view) to reveal and focus the existing view.
    },
})
if err != nil { return err }
defer client.Close()

// Query first; call RequestPermission only after an explicit user choice.
status, err := client.Permission(ctx)
// Handle err and status before posting (Linux: PermissionNotRequired).
handle, err := client.Post(ctx, notify.Notification{
    ID: opaqueID, Title: "New activity", // composition is the app's policy
})
// After checking err:
err = handle.Replace(ctx, notify.Notification{Title: "More activity"})
err = handle.Dismiss(ctx) // e.g. the corresponding room was read
```

`Notification.ID` is required, at most 256 bytes, and unique among a client's
live handles. Keep it opaque and keep its routing map in Go. The ID is never
sent to the OS: Linux uses daemon IDs and macOS uses unrelated random request
identifiers. Titles and messages **are** sent to the OS; credentials and
message content do not belong in routing keys or diagnostic logs. The caller
owns its notification privacy/composition policy.

| API | Behavior |
| --- | --- |
| `Client.Capabilities()` | Activation, replacement, dismissal and native permission-API support, as discovered at creation. Acceptance never proves display. |
| `Client.Permission(ctx)` / `RequestPermission(ctx)` | Query without prompting / explicitly request permission. Reports unknown, not determined, denied, authorized, provisional or not required. |
| `Client.Post(ctx, Notification)` | Returns `*Handle`; `ErrDuplicateID` rejects a second live handle for the same application key. |
| `Handle.ID()` | The application's opaque key. |
| `Handle.Replace(ctx, Notification)` | Keeps the application key and handle; an empty replacement ID means the current ID. Failure leaves the handle tracked. |
| `Handle.Dismiss(ctx)` | Removes the notification and suppresses queued activation. Idempotent for an already closed handle in a live client. |
| `Handle.Done()` / `Err()` | Completion and its reason: nil for normal close, `ErrUnavailable` for daemon loss, `ErrClosed` for client shutdown. See macOS limitations below. |
| `Client.Close()` | Stops callbacks, best-effort dismisses outstanding handles with a shared five-second deadline, and releases resources. Returns cleanup errors; idempotent. |

Managed methods are safe from any goroutine and serialize native operations;
they may wait, so call them off the UI thread. Activation callbacks run
serially on a worker and may call tuohi View methods or `Client.Close`.
Cancellation/Close drops queued callbacks; an already admitted callback may
finish afterwards. Dismiss also suppresses queued activation for that handle.
An operation context stops waiting but cannot undo a request the OS already
accepted or close an already visible permission dialog. Cancelling the client
context initiates the same cleanup as Close. A crash cannot perform cleanup;
correlation and activation are limited to this client's running process and
are not a persistent activation protocol across application restarts.

| Platform | Managed behavior |
| --- | --- |
| Linux (BSD compile only) | Dedicated session D-Bus connection; `GetCapabilities` decides action support. `Notify` default actions/replacement IDs and `CloseNotification` provide lifecycle control. Permission is `PermissionNotRequired`; the protocol cannot query do-not-disturb or per-app desktop suppression. A client requesting activation fails if the server omits `actions`. |
| macOS | Modern `UNUserNotificationCenter`, with authorization queries/requests, default activation, replacement by request identifier and removal of pending/delivered requests. Requires a real bundled `.app` and an unused notification-center delegate. Managed icon attachments return `ErrUnsupported`; legacy helpers' image options are unchanged. |
| Windows | `NewClient` returns `ErrUnsupported`: a shared balloon has no reliable independent notification identity or managed activation/dismissal lifetime. Existing read-only helpers continue to work. |
| Other platforms | `ErrUnsupported`. |

Linux signals and operations bind to the notification daemon's unique owner.
If it disappears or restarts, outstanding handles end with `ErrUnavailable`;
create a new client. Old daemon IDs are never reused against the new daemon.
Only that daemon's default-action signals for tracked IDs can activate them.

For modern macOS, package the executable as
`Name.app/Contents/MacOS/executable`, with `Contents/Info.plist` declaring
`CFBundlePackageType=APPL`, `CFBundleIdentifier`, and `CFBundleExecutable`.
The main bundle, executable location and identity are checked **before**
requesting a notification center; a bare `go run`/test executable returns
`ErrUnavailable` without calling an API that can raise a native exception.
tuohi retains its delegate and refuses to replace another library's delegate;
one managed client may own the center at a time. Set it up during startup and
keep the application's normal native event loop running for activation.
Denial cannot trigger another prompt; direct the user to System Settings.
The removal API acknowledges submission, not visible disappearance. macOS
does not report every passive expiration or dismissal; explicitly dismiss or
close handles when their application state ends. No reply actions, remote
push registration, or critical-alert entitlement is requested.

The [managed notification example](../examples/managednotify/) demonstrates
activation focusing an existing view, replacement, room-read dismissal and
optional permission requests. Linux protocol behavior is tested on a private
D-Bus service; native macOS bundle delivery/permission behavior requires a
macOS desktop and is separate from cross-compilation. See Apple's
[notification-center documentation](https://developer.apple.com/documentation/usernotifications/unusernotificationcenter)
for native thread and delegate guarantees.

## API

| Func / Type | Description |
| --- | --- |
| `Show(name, title, message string) error` | Display a notification from the named application - equivalent to `ShowOpts` with a zero `Options`. Safe from any goroutine. |
| `ShowOpts(name, title, message string, opts Options) error` | Like `Show`, plus an icon and an urgency. |
| `Alert(name, title, message string, opts Options) error` | `ShowOpts` at forced `UrgencyCritical`, followed by the platform's attention sound. |
| `Beep(freq float64, duration int) error` | Play a tone; zero freq/duration select `DefaultFreq`/`DefaultDuration`. |
| `Options struct` | `Icon string`, `IconData []byte` (set at most one), `Urgency Urgency`. The zero value behaves exactly like `Show`. |
| `Urgency` | `UrgencyLow`, `UrgencyNormal`, `UrgencyCritical`; zero value (and `Options{}`) means normal. |
| `DefaultFreq` / `DefaultDuration` | 440 Hz / 200 ms - the tone used when `Beep`/`Alert` get a zero value. |
| `ErrUnsupported` | Sentinel when the platform has no backend (or cannot honor an option, e.g. PNG icon data on Windows). |
| `ErrUnavailable` | Sentinel when a backend exists but can't be used in this process/environment (macOS without a bundled `.app`; Linux with no session bus *and* no `notify-send`/`kdialog`). |

Options are validated before the platform backends run: setting both `Icon`
and `IconData` (or an empty `IconData`) is an error on every OS, so the rules
never differ between platforms.

## Icons and urgency per platform

| OS | `Options.Icon` | `Options.IconData` | Urgency |
| --- | --- | --- | --- |
| Linux | image file path **or** themed icon name (e.g. `"dialog-warning"`) | ✅ PNG bytes sent as the D-Bus `image-data` hint | `urgency` hint byte 0/1/2; critical also requests the desktop's alert sound |
| macOS | image file path (any format NSImage reads) | ✅ PNG bytes | critical attaches the default notification sound |
| Windows | **`.ico`/`.bmp` file path**, or stock name `"info"`, `"information"`, `"warning"`, `"error"` (case-insensitive) | ❌ `ErrUnsupported` - no PNG decoding | critical → error-styled balloon; low/normal → information balloon |

Windows balloons show the stock glyph or (for a `.ico`/`.bmp` file) the custom
image; PNG bytes are rejected instead of silently showing nothing. On Linux an
`Icon` path that does not exist is passed through as a themed icon name.

## Threading

All functions are safe to call from any goroutine. Windows opens its hidden
notification icon lazily on the first call and keeps it for the process
lifetime; Linux opens one session-bus connection, reused and reopened
automatically if the daemon restarts.

## Platforms

| OS | Backend | Status |
| --- | --- | --- |
| macOS | `NSUserNotificationCenter` via the Objective-C runtime (pure) | supported |
| Windows | `Shell_NotifyIconW` balloon (`NIM_MODIFY` + `NIF_INFO`) | supported |
| Unix (Linux, FreeBSD, NetBSD) | `org.freedesktop.Notifications` over D-Bus (godbus), falling back to `notify-send` and `kdialog` | supported on Linux; FreeBSD/NetBSD compile only; needs a session bus or one of the CLIs at runtime |
| others | - | `Show`/`ShowOpts`/`Alert`/`Beep` return `ErrUnsupported` |

Check the unsupported/unavailable cases with `errors.Is(err, notify.ErrUnsupported)`
/ `errors.Is(err, notify.ErrUnavailable)`.

### Notes

- **macOS** - uses the deprecated `NSUserNotification` API. To actually post a
  banner the process must be a **bundled `.app`** the user granted Notification
  permission in System Settings; the legacy center is nil for an unbundled
  binary and the modern `UNUserNotificationCenter` crashes without an app
  bundle. When no usable center exists, the functions return `ErrUnavailable`
  instead of crashing. The icon is attached as the notification's
  `contentImage` (macOS 10.9+); older releases ignore it.
- **Windows** - balloons attach to a notification-area icon, so the first
  call registers a small application icon that lives for the process. Titles
  are truncated to 63 chars and messages to 255 (Win32 limits). Custom file
  icons are loaded with `LoadImageW`, which reads `.ico`/`.bmp` only - a PNG
  path fails with a clear error and PNG bytes return `ErrUnsupported`.
  `Beep` calls the kernel `Beep` function and blocks for the duration.
- **Linux** - needs a session D-Bus (a desktop session provides one); when
  neither the bus nor `notify-send`/`kdialog` is usable, the returned error
  wraps `ErrUnavailable`. `Beep` writes a tone to the PC speaker
  (`/dev/input/by-path/platform-pcspkr-event-spkr`; needs the `pcspkr` module
  and `input`-group access) and falls back to the terminal bell character.
- **Beep** - on macOS there is no tone API: `Beep` plays the system beep via
  `osascript` (frequency/duration ignored), falling back to the terminal
  bell. On Linux the frequency is capped at 20 kHz, on Windows clamped to the
  37–32767 Hz range the driver accepts.

## App integration

tuohi's `App` has no notification method, and the root package does not
import this one, so a program that opens a window without notifying does not
link it. Call `notify.Show` directly and pass `App.Name` as the source:

```go
app := &tuohi.App{Name: "backup tool"}
if err := notify.Show(app.Name, "Backup finished", "Snapshot complete"); err != nil {
	// errors.Is(err, notify.ErrUnsupported) on unsupported platforms
}
```

## Example

A runnable example lives in [`examples/notify`](../examples/notify/): it fires a plain notification, one
with a custom icon, an alert and a beep, exiting after short pauses so the
desktop can present them. It also exits cleanly (exit code 0) when no
notification service exists in the environment.

```bash
cd examples && go run ./notify
```

## Conventions

Part of the tuohi module. Public API lives in the tag-free `notify.go`,
per-platform backends in `notify_{darwin,windows,linux}.go` (icons, urgency,
beep and the alert sound each live in their platform file), and
`notify_other.go` returns `ErrUnsupported` so every `GOOS` builds. No
`internal/` packages.
