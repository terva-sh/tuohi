# tuohi's architecture, and the shape it is moving to

This page records the architecture review of tuohi, TKT-01M3HWWRSJC4QVVGPW04H5CQBD
(Review tuohi's architecture and write down its target shape), done on
2026-09-27 against `main` at `9bdaa18`. It describes what appkit left us, what
tuohi should become, and why. The work each change needs is a ticket, named
beside the decision it carries out. Line numbers are as of that commit.

The review took its facts from reading the source. The GUI scenarios had run
and passed on Linux the same day, after
TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG (GUI tests skip wherever bubblewrap is installed)
and TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown
on both WebKitGTK stacks). Where a claim comes from reasoning about an engine
rather than from code or a run, it says so.

## The consumer tuohi is for

tuohi exists for one shape of program: a Go binary that already serves its web
interface over loopback HTTP and wants a native window on it instead of an
Electron package. terva, lampi, ketju, and git-ticket-canvas are that shape.
git-ticket-canvas measured the options in its `docs/desktop-shell.md` and wrote
down what it needs from the window build:

- open a window on `http://127.0.0.1:PORT`, an ephemeral port it chose;
- send links that leave the application to the system browser;
- set the window title;
- show a folder picker;
- keep one instance per user.

It does not need `App.FS`, the `app://` scheme, tuohi's own loopback server,
the tray, notifications, the clipboard, or autostart. appkit was built the
other way round: `App.FS` is the first-class path, and loading an outside URL
works but is barely documented (`view.go:733-737` lists only `app://`,
`https://`, and `data:`).

The review keeps every feature, because the owner asked for that, and makes
the loopback consumer the case the design serves first. Where the two pull in
different directions, the loopback consumer wins.

## Decisions at a glance

| Input | Decision | Ticket |
|---|---|---|
| Bridge security model | Each view trusts an allowlist of origins, by default the one it opened. One gate covers bindings, events, and internal messages. A navigation policy sends other origins to the system browser. | TKT-01M3HWWRT7X1RZZYY6KFEP0ERE |
| Permissions | Denied by default on every engine, granted per view by the app. | TKT-01M3HWWRTVWVYSEDPRKSDPE783 |
| Single instance | Its own package, hardened as designed IPC. | TKT-01M3HWWRVGMZTXBYJ86FCTH0V8, TKT-01M3J59M1H9PZ04J2C9JJZ7V13 |
| The FFI layer | Upstream purego replaces `pure/`. | TKT-01M3HWWRW7YGCHHZY4BFEX3A6W |
| How the engines share one interface | An unexported `engine` interface and a shared bridge core. | TKT-01M3J59M0VJYQ3E652Y0FD90H3 |
| Threading | Every exported method is safe from any goroutine on every engine. | TKT-01M3J1H8CPMZX9EJX8R2CQRA6P |
| Package boundaries | The root package is the window. Desktop services move to subpackages. | TKT-01M3J59M1H9PZ04J2C9JJZ7V13 |
| `atotto/clipboard` | Replaced by native clipboards. | TKT-01M3J59M2BR91XQBDT1TPPM4G5 |
| `App.HTTP` loopback server | Kept. It gets a Host check and a per-server token, and its idle shutdown gets tested. | TKT-01M3J59M4EJRPMKHWBS7K4XD3S |
| `App.Open` | `file:` is dropped. | TKT-01M3J59M32VGK83J7JKYPKSHJE |
| Borrowed code | Wails autostart kept and credited, with its quoting rewritten. The WebView2 loader is kept and credited. | TKT-01M3J59M535T9QH2RS1ZY6PSJM |
| Side effects | The GTK3 Wayland desktop-file writing becomes opt-in. | TKT-01M3HWWRYD7GNZEZA2JCGWGDJS |
| Hygiene | Review tags, stale docs, and "appkit" strings go with the rename. | TKT-01M3HWWRWX3D9XTA5RTW5AC26R |
| Support tiers and testing | Tier 1: Linux, macOS, Windows, tested on the real engine. Tier 2: FreeBSD and NetBSD, cross-built only. CI mechanics stay with their ticket. | TKT-01M3HWWRXMN56AG2GNC3M92GWZ |
| Go minimum | 1.26. | TKT-01M3HWWRXMN56AG2GNC3M92GWZ |

The review also found four defects outside those inputs:

- TKT-01M3J59M3SQBSXV2KVZEFBNBAV (Stop a non-string bridge message crashing
  macOS views);
- TKT-01M3J59M5V12QW1WRBEJPJ5H38 (Make the macOS main-thread rule explicit
  and enforced);
- TKT-01M3J59M6JQZJWB23HPXXQZVZR (Report missing WebKitGTK symbols as an
  error, not a panic);
- TKT-01M3J59M7A0XK9NFE0RG72ZBFA (Find what installs a SIGSEGV handler
  without SA_ONSTACK under GTK4).

## The root package is the window

Today the root package `tuohi` is everything. It exports 62 identifiers and no
package-level functions. Everything hangs off `App` or `View`, and `App` is
both configuration and runtime, in the style of `http.Server`. The root
imports `notify`, `tray`, `dialog`, and `github.com/atotto/clipboard`
(`app.go:51-53`, `view.go:10`). A program that opens one window therefore
links godbus for the tray and notifications, and atotto, whose `init` scans
PATH for clipboard tools at process start.

The owner chose on 2026-09-27 to split the services out. The target layout:

| Package | Holds |
|---|---|
| `tuohi` | `App`, `View`, bindings, events, window state, `App.Icon`, `App.Open`, `App.Reveal`, and serving: `App.FS`, `App.HTTP`, `app://` |
| `tuohi/dialog` | file and folder dialogs. `View.Dialog` stays as a helper, because a dialog needs its parent window. |
| `tuohi/instance` | single instance, taken from `App.ID` and `App.Exec` |
| `tuohi/autostart` | launch at login, taken from `App.Autostart()` |
| `tuohi/clipboard` | native clipboard, taken from `App.Copy` and `App.Paste` |
| `tuohi/notify` | notifications. It exists already, and `App.Notify` goes. |
| `tuohi/tray` | tray icon and menu. It exists already, and `App.Tray` goes. |

None of the subpackages imports the root today, and that direction stays.
Where a service needs the UI thread, such as the macOS tray or a GTK
clipboard, the subpackage reaches it through a hook that `tuohi` exports.
`tuohi` does not import the subpackage.

Keeping one package was the alternative. It keeps appkit's single-`App`
design, which is pleasant for an application that wants everything. It loses
because every consumer pays for every service, and the consumers tuohi exists
for want a window and little else. Single instance is a clean example. It is
about 490 lines across four files, touches no `App` state, and its only
coupling is a key derived from `App.ID`.

`App.Open` and `App.Reveal` stay in the root. They are small, need no
dependency, and are what the navigation policy calls to send a link to the
system browser.

## One engine interface, one bridge core

Each platform defines its own `webview` type: `lib_unix.go` (1,831 lines),
`lib_darwin.go` (1,782), and `lib_windows.go` (2,592). `View.w` is a concrete
`*webview`, and no Go interface says what an engine must provide. The shared
code calls about sixteen methods on it:

- `Navigate`, `Eval`, `Init`, and `BindBatch`;
- `Focus`, `Raise`, `Show`, and `Hide`;
- `Maximize`, `Minimize`, `Unminimize`, and `Unmaximize`;
- `Dispatch`, `Window`, `Terminate`, and `Destroy`.

It also writes six fields that each engine must declare by hand:
`eventsGlobal`, `onReady`, `onReadyFired`, `events`, `transient`, and
`contentBase`.

That contract is only implied, and it has already drifted. `View.Focus`'s doc
comment says the backends marshal to the UI thread, and on Unix and Windows
they do not. The three engines also carry six pieces of near-identical logic:

- the message envelope parse and the dispatch tail of `onMessage`;
- the reply path, `resolve`;
- `BindBatch` and `Unbind`;
- the rewrite of `app://` URLs onto the loopback origin;
- the default size and position in geometry;
- the engine ID registry.

The target, which is TKT-01M3J59M0VJYQ3E652Y0FD90H3 (Declare the engine
interface and share the bridge core):

- **An unexported `engine` interface** that the shared code depends on and
  each platform satisfies, checked at compile time.
- **A shared `bridge`** that owns the bindings, the message envelope, the
  reply path, the events, and the internal-message switch. An engine hands it
  a raw message body and whatever it knows about the sender, and evaluates the
  JavaScript the bridge gives back.
- **Per-view state the shared code needs lives in a struct the shared code
  owns,** not in fields each engine must remember to declare.

What stays per engine is what differs: native windows, the web view's
settings, scheme handling, the event loop, and the navigation and permission
hooks.

This refactor comes before the security work on purpose. The origin gate, the
navigation policy, and the threading rule all touch the bridge. Written
against a shared bridge, each lands once. Written against today's code, each
lands three times and drifts again.

The review considered exporting the interface and rejected it. Nobody outside
tuohi should implement an engine, and an exported interface is an API promise
we would keep for no user.

## One threading rule

GTK, AppKit, and Win32 windows must be driven from the thread that owns them.
tuohi's bindings run on their own goroutines (`serialQueue`, `bind.go:418`),
so code inside a binding is off the UI thread by construction. appkit's rule
for what may be called from there differs by engine:

| Method | Unix | macOS | Windows |
|---|---|---|---|
| `Navigate`, `Eval`, `Focus`, `Raise` | direct | synchronous marshal | direct |
| `Show`, `Hide`, and the window-state methods | asynchronous marshal | synchronous marshal | asynchronous marshal |
| `Destroy` | asynchronous marshal when off the UI thread | synchronous marshal | asynchronous marshal when off the UI thread |
| internal `BindBatch` | direct | synchronous marshal | direct, and pumps messages on the caller's thread |

"Direct" means the native call runs on the caller's goroutine. That is the
class of bug that crashed every Linux GUI scenario in `Destroy`, fixed in
`ab07b8a` for `Destroy` alone.

The rule from now on: **every exported `View` and `App` method is safe to call
from any goroutine on every engine.** On the UI thread a call runs in place.
Elsewhere it is marshalled to the UI thread asynchronously.

A call whose caller needs a result, such as a dialog, waits for it, and must
neither hang nor report a failure that later turns out false:

- **Each marshalled operation carries a state:** pending, running, or
  cancelled. The UI thread claims an operation atomically before running it,
  and skips a cancelled one.
- **The loop owner marks the loop stopped when its loop exits for good,** at
  the end of `App.Wait` or `Run`. Anything still pending is then cancelled,
  and a later call returns an error at once instead of queuing.
- **A waiting caller gives up only when the loop is marked stopped,** never on
  a timer. It then cancels its operation atomically. If the operation has
  already started, the caller waits for it instead.

A timeout was the first draft and lost. A timeout cannot tell a stopped loop
from a busy one, so a call could report failure and then run anyway when the
loop resumed, and a caller that retried a dialog would get two. macOS's
`performOnMain` waits forever today (`lib_darwin.go:550-561`) and changes to
match. TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call
from any goroutine) carries this out in the shared layer.

Running the native call in place when the loop is not running was also
considered. It is the crash the rule exists to prevent.

macOS adds one more rule. AppKit must run on the process's main thread, and
tuohi does nothing to put it there: there is no `init` that locks the main
goroutine, and the documentation does not say so. A loopback consumer that
starts its HTTP server on the main goroutine and opens the window from another
gets AppKit on a secondary thread. TKT-01M3J59M5V12QW1WRBEJPJ5H38 (Make the
macOS main-thread rule explicit and enforced) adds the `init`, an error when
`Show` or `Wait` is called off the main thread, and an example of the right
shape: the server in a goroutine, the window on main.

## The bridge answers only the origins a view trusts

A page calls Go through `window.__webview__`, which the bridge script defines
(`bind_gen.go:303-337`). The script is injected at document start into every
top-level document on every navigation, whatever its origin (`lib_unix.go:1671`,
`lib_darwin.go:1576`, `lib_windows.go:1269`). Each engine has exactly one place
where a message arrives (`lib_unix.go:911`, `lib_darwin.go:219-224`,
`lib_windows.go:691-699`), and none of them looks at who sent it. A view that
ends up on a page the application did not write, through a link, a redirect,
or content it displays, hands that page every binding.

The design, which TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins
call a view's Go bindings) carries out:

- **Each view has an allowlist of origins.** By default it holds the origin of
  the first URL the engine actually loads, after `resolveURL`. For a loopback
  consumer that is its own `http://127.0.0.1:PORT`, trusted with no
  configuration. For `App.FS` it is `app://` on Linux, `https://app.localhost`
  on Windows, and the temporary server's `http://localhost:PORT` on macOS or
  under `App.HTTP`. That port changes with every server, so the gate reads it
  from the resolved URL. `View` gets a field to add more origins.
- **The bridge is installed only in allowed origins.** None of the three
  engines filters document-start scripts by origin (WebKitGTK's allow list
  ignores the port), so the script filters itself. It carries the view's
  trusted origins and returns before defining anything unless it runs in the
  top-level document of one of them. It also carries a random per-view token,
  which `post` puts in front of every message and the gate requires. A
  document on any other origin, or a frame, never holds the token.
- **Every engine checks the sender at its one entry point,** with what that
  engine can see:
  - WKWebView gives the frame and its security origin in
    `message.frameInfo`, so macOS can accept main-frame messages from allowed
    origins exactly.
  - WebView2 gives the top-level source through `GetSource`, which is
    declared in the vtable but never called (`lib_windows.go:289`).
  - WebKitGTK gives no frame with `script-message-received`, and the URI it
    reports is read when the message arrives. The token covers both: a frame
    from another origin cannot read it, and a document that was never trusted
    never received it. An isolated script world was the first plan. The token
    replaced it because it does the same job on all three engines with one
    mechanism.
- **One gate for everything a page can send.** The events binding
  (`__appkit_event__`) and the internal window messages go through the same
  gate as ordinary bindings: drag, resize, toggle maximize, app regions, and
  bind errors. Today a page can maximise any window, because `toggleMaximize`
  has no `frameless` guard on Unix or Windows. On Windows it can also turn the
  whole client area into a title bar.
- **A navigation policy.** It lands only after `App.Open` stops accepting
  `file:`, because it hands `App.Open` URLs that a page chose. A top-level
  navigation to an allowed origin goes ahead. Anything else is cancelled in the view and handed to `App.Open`,
  which sends it to the system browser. This is exactly what git-ticket-canvas
  asked for. `window.open` and `target=_blank` follow the same rule. On
  Windows that matters twice, because WebView2 opens its own popup window
  today, and that window has none of the bridge.

A per-binding allowlist was the alternative. It lost because the consumer case
is one trusted origin per view, and per-binding policy multiplies
configuration for no user.

`App.Open` becomes the path a page's links take, so what it accepts matters
more. It allows `file:` today (`app.go:1110-1125`), and a `file:` URL handed
to `ShellExecuteW`, `NSWorkspace`, or `xdg-open` can launch a program. The
owner chose on 2026-09-27 to drop it: TKT-01M3J59M32VGK83J7JKYPKSHJE. A local
file is shown with `Reveal`.

One defect sits in the macOS entry point regardless of the design. It sends
`UTF8String` to `message.body` without checking its type, so
`postMessage({})` probably raises an Objective-C exception and aborts the
process. That comes from reading the code. Nobody has run it yet.
TKT-01M3J59M3SQBSXV2KVZEFBNBAV fixes it.

## Permissions are denied unless the app grants them

Each engine loosens something different, and none asks the application:

- **Linux** turns on `enable_media_stream` and
  `javascript_can_access_clipboard` (`lib_unix.go:1775-1776`) and registers no
  `permission-request` handler.
- **macOS** grants every camera and microphone request from any origin
  (`lib_darwin.go:468-472`) and turns `fullScreenEnabled` on against the
  native default.
- **Windows** registers no `PermissionRequested` handler, so WebView2 shows
  its own prompts.
- **Every platform** turns dev tools on when the environment sets
  `APPKIT_DEBUG=1` (`app.go:167`), whatever the build.

TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the
app allows them) replaces this with a per-view policy the application sets,
denying by default, enforced in one handler per engine. The review leans
against letting an environment variable turn dev tools on in a release build.
`View.Debug` is the application's decision, and an environment variable is
whoever launched it.

## Serving: the loopback consumer first, `App.FS` kept

A view reaches its content in one of three ways:

1. **An outside URL,** set in `View.URL` or passed to `Navigate`.
   `resolveURL` rewrites only `app://` (`app.go:1208`), so
   `http://127.0.0.1:PORT/` reaches the engine untouched. This is the
   consumer case. It works today, and the review makes it the documented
   first path.
2. **`App.FS` over the `app://` scheme,** registered natively on Linux and
   served as `https://app.localhost` on Windows.
3. **`App.FS` over a temporary loopback server,** always on macOS, and on
   Linux and Windows when `App.HTTP` is set. WKWebView cannot make a custom
   scheme a secure context, and a WebKit bug keeps `SharedArrayBuffer` off
   plain WKWebView pages. The server sends COOP, COEP, and CORP headers so
   cross-origin isolation works.

Paths 2 and 3 stay. They are how an application with no server of its own
ships its interface, and the owner asked for functionality kept. They are not
free, though:

- **The loopback server checks nothing about who connects.** It reads the
  Host header only to build URLs (`app.go:1455`). Two different readers can
  reach `App.FS` while it is up, and each needs its own fix. A web page can
  reach it through DNS rebinding (INFERRED), and a Host check stops that.
  Another local process sends whatever Host it likes, so it needs an
  unguessable per-server token in the URL the view loads. That keeps out
  other users on the machine. A process running as the same user can read
  `App.FS` anyway, from the binary or from memory, and the docs should say
  so rather than claim more.
- **It shuts down 3 seconds after its last request** (`loopbackIdleTimeout`,
  `app.go:1306`). That is deliberate, "so it serves exactly the page's initial
  load". But the page's origin is that server. Reading the code, a lazy
  `import()` or a `fetch` made after 3 seconds finds no server to answer it.
  Nobody has run that case yet.
- **Two comments say it stops at the first load finishing**
  (`app.go:1273`, `lib_unix.go:1809-1810`). Only `Destroy` and the idle timer
  stop it.

TKT-01M3J59M4EJRPMKHWBS7K4XD3S (Guard tuohi's loopback server and settle its
idle shutdown) adds the Host check and the token. It also tests a late fetch and
settles the lifetime from the result. A consumer that serves its own
interface never starts this server, and should send its own COOP and COEP
headers if it wants cross-origin isolation.

## Desktop services

- **Single instance** moves to `tuohi/instance` as
  `Acquire(id, onMessage)` and `Send`. The Unix and darwin copies are
  near-identical and merge. TKT-01M3HWWRVGMZTXBYJ86FCTH0V8 (Harden the
  single-instance channel against other local users) already lists the
  hardening: no `/tmp` fallback, a 0700 directory, peer credentials, a size
  cap, and a pipe security descriptor. The review adds four defects:
  - `release` unlinks the lock file, which can give two primaries whether it
    unlinks before or after unlocking, so the lock file must stay;
  - Windows drops a launch that arrives while the pipe is busy;
  - the working directory is not forwarded;
  - the Unix read has no deadline.
- **Autostart** moves to `tuohi/autostart`. About 75 to 80 percent of its
  1,130 lines are derived from Wails v3, credited in NOTICE and at the code.
  The SMAppService binding is appkit's own. The review keeps it credited
  rather than rewriting it: MIT with credit costs nothing, and a rewrite would
  come out much the same. What it does rewrite is the `.desktop` `Exec`
  quoting. That quoting was copied from Wails, and it neither doubles `%` nor
  quotes the reserved characters: TKT-01M3J59M535T9QH2RS1ZY6PSJM.
- **Clipboard** moves to `tuohi/clipboard` and drops atotto for native calls:
  GTK3, GTK4, `NSPasteboard`, and Win32. TKT-01M3J59M2BR91XQBDT1TPPM4G5. On
  Linux, atotto runs `wl-copy`, `xclip`, or `xsel` and has no clipboard
  without one of them. GTK is already in the process. The one awkward piece
  is GTK4 paste, which is asynchronous.
- **Notify, tray, and dialog** are already subpackages and stay as they are.
  Only the root's imports of them go. Two limits to know about:
  - The Linux tray is a StatusNotifierItem over D-Bus. It needs a watcher,
    which GNOME ships only as an extension. INFERRED.
  - The Linux dialog uses `GtkFileChooserNative`, so it goes through the
    desktop portal where GTK chooses to.
- **The icon** stays in the root. Under GTK3 on Wayland, starting an
  application today writes icons and a hidden `.desktop` file into
  `~/.local/share` and starts `kbuildsycoca`, without being asked. That
  becomes an explicit opt-in: TKT-01M3HWWRYD7GNZEZA2JCGWGDJS.

## The FFI layer: upstream purego

`pure/` is purego v0.11.0 with its package renamed, Android and iOS removed,
and panic messages prefixed. A diff against upstream v0.11.1 shows nothing
else. Engine code uses the standard entry points:

| Entry point | Calls outside `pure/` |
|---|---|
| `RegisterLibFunc` | 155 |
| `SyscallN` | 73, all Windows COM calls |
| `NewCallback` | 23 |
| `RegisterFunc` | 21 |
| `Dlopen` | 16 |
| `Dlsym` | 14 |

The darwin code also uses `pure/objc`, whose API matches upstream's `objc`
package. TKT-01M3HWWRW7YGCHHZY4BFEX3A6W (Replace pure/ with upstream
ebitengine/purego) switches to the dependency. That brings in upstream's
v0.11.1 fixes, hands the per-architecture assembly back to the Ebitengine
project, and closes the Apache-2.0 notice gap in the copy.

One behaviour to change on the way: `RegisterLibFunc` panics on a missing
symbol, and on Unix it runs inside `ensureInit`'s `sync.Once`, so a missing
symbol escapes as a panic instead of the error `ensureInit` returns.
TKT-01M3J59M6JQZJWB23HPXXQZVZR makes it an error naming the library and the
symbol.

Something in a GTK4 process installs a SIGSEGV handler without
`SA_ONSTACK`, and JavaScriptCore is the likely source. While that handler is
installed, a nil dereference anywhere in the program, the consumer's code
included, is a fatal error with no panic and no stack. The GTK4 test crash
fixed in `ab07b8a` showed it. TKT-01M3J59M7A0XK9NFE0RG72ZBFA finds the source
and decides whether tuohi works around it.

## Support tiers, CI, and the Go minimum

The review settles the tiers and the Go minimum. TKT-01M3HWWRXMN56AG2GNC3M92GWZ
(Decide tuohi's support tiers and make CI match them) keeps the CI questions:
testing macOS and Windows before a merge rather than after, the inherited
GitHub workflow, action pinning, and GUI scenarios on Forgejo.

- **Tier 1 is Linux, macOS, and Windows.** Each is built and tested on its
  real engine on every change to `main`: WebKitGTK 4.1 and 6.0 on amd64 and
  arm64, WKWebView, and WebView2. That keeps appkit's claim, and CI now
  matches it.
- **Tier 2 is FreeBSD and NetBSD.** They must cross-build, and nothing runs
  them. Dropping them lost, because cross-building costs one CI step and the
  owner asked for functionality kept. Promoting them lost, because no runner
  exists.

- **Go 1.26 is the minimum.** The owner chose it on 2026-09-27. The code needs
  about Go 1.23: `reflect.TypeFor`, range over int, and
  `structs.HostLayout`. `golang.org/x/sys` v0.48.0 declares 1.26, which sets
  the real floor. The inherited `go 1.27` is not needed. git-ticket-canvas
  moves to 1.26 when it adopts tuohi. Pinning an old `x/sys` to stay at 1.25
  lost: it holds a security-relevant dependency back to save one consumer a
  one-time move.
- **Linux GUI scenarios now run** under `just test-gui` locally and on
  GitHub's Ubuntu runners, on both WebKitGTK stacks. They do not run on
  Forgejo CI, whose Alpine container has no WebKitGTK.
- **GitHub's Linux jobs build with cgo on** and run without `-v`, so they
  exercise a different path from what ships.

## What this review did not settle

- **Whether a page served by `App.HTTP` breaks after the idle timeout.** It is
  inferred from the code and needs the scenario in
  TKT-01M3J59M4EJRPMKHWBS7K4XD3S.
- **Whether WebKitGTK denies an unhandled permission request.** Its
  documentation says so. TKT-01M3HWWRTVWVYSEDPRKSDPE783 makes the question
  moot by handling every request.
- **How far the Linux isolated script world carries the origin check into
  frames.** TKT-01M3HWWRT7X1RZZYY6KFEP0ERE has to show it with a
  cross-origin iframe.
- **The macOS and Windows behaviours this page calls INFERRED.** The review
  read them from code and documentation, and made no macOS or Windows run.
