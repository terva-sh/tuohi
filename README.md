# tuohi

tuohi puts a Go program's web interface in a native desktop window, using the
web engine the operating system already has: WebKitGTK on Linux, WKWebView on
macOS, and WebView2 on Windows. It needs no cgo, so a program that embeds it
still cross-compiles to every desktop from one machine with `CGO_ENABLED=0`,
and it ships no browser engine of its own.

*Tuohi* is Finnish for birch bark: the thin, light outer layer of the tree, and
the traditional material for light containers. terva and other terva-sh tools
embed tuohi to offer a desktop window over the web interface they already
serve, instead of an Electron package.

```sh
go get github.com/terva-sh/tuohi
```

tuohi is a fork of `github.com/malivvan/appkit` v0.1.0, whose repository no
longer exists. [docs/provenance.md](docs/provenance.md) records where the
source came from and what the review of it found, and [NOTICE](NOTICE) credits
the projects it draws on. The fork is being reviewed and reworked before its
first release, so expect the API to change.
[docs/architecture.md](docs/architecture.md) records the shape tuohi is moving
to, and why.

---

tuohi is a pure-Go foundation for building web-based desktop applications. It
drives the web engine each operating system already ships - WKWebView on
macOS, WebKitGTK on Linux, WebView2 on Windows - behind a single Go API, and
adds what a window needs around it: windows and app windows, drag regions,
custom URL schemes, URL/file opening and native file dialogs. Notifications,
the clipboard, single instance, autostart and the tray live in subpackages
beside it (see [Desktop services](#desktop-services)). Everything is cgo-free.

## Why no cgo

Most native-view bindings reach for cgo, which quietly takes back the things
that make Go pleasant to ship: cross-compiling needs a matching **C**
cross-compiler for every target (MinGW for Windows, a sysroot for Linux),
builds stop being reproducible, and `go get`/`go install` only works for
people who already have that toolchain set up.

tuohi keeps cgo out entirely. Through [purego](https://github.com/ebitengine/purego)
it loads the OS view at runtime (`dlopen` / `LoadLibrary`), so no C compiler
is in the loop:

- **Cross-compile to every desktop from one machine** - no C cross-toolchain,
  just `GOOS`/`GOARCH`:

  ```sh
  GOOS=windows GOARCH=amd64 go build   # from a Mac, from Linux, from anywhere (see "xdemo" Makefile target)
  GOOS=linux   GOARCH=arm64 go build
  GOOS=darwin  GOARCH=arm64 go build
  ```

- **`CGO_ENABLED=0` builds** - reproducible output, and a `go get` /
  `go install` that just works with no compiler to install first.

One caveat, so "self-contained" is not misread: tuohi does **not** bundle a
browser engine - it is not Electron. The binary ships no native library and
stays small, but it uses the *system* view at runtime, so the target machine
needs that present: WebView2 on Windows (preinstalled on current Windows
10/11), WebKitGTK on Linux (a package), WKWebView on macOS (built in).

## What's in the box

- No cgo
- Windows, macOS and Linux
- Zero bundled native libraries - binds the OS view directly (WKWebView /
  WebKitGTK / WebView2)
- JavaScript ↔ Go binding
- A single `*App` scope for the app: `App` is both the configuration (like an
  `http.Server`) and the runtime context; the app-scoped operations are
  methods on it (`Show`, `Wait`, `Quit`, `Open`, `Reveal`, `Backend`) and its
  settings are committed the first time any method is called
- Native file dialogs - a single `View.Dialog(opts dialog.Options)` method and
  the standalone `dialog.Open`, with the panel kind chosen through
  `dialog.Options.Type` - plus opening URLs / revealing files
  (`App.Open` / `App.Reveal`)
- A declarative system tray with a menu - PNG icons (light/dark/macOS-template
  variants), checkboxes, submenus, separators (the `tray/` package, set up
  from `App.Start`), desktop notifications (the `notify/` package), and a
  best-effort runtime application icon (`App.Icon`; a no-op on Windows, which
  reads the icon from the executable's own resources, and shown by a GTK3
  window on Wayland only when `App.DesktopEntry` lets tuohi install a desktop
  entry for it)
- The system clipboard (the `clipboard/` package) and single instance (the
  `instance/` package: one process per application, and a later launch hands
  its arguments to the running process)
- A Go↔JS `Events` bridge built into every view: `w.On` / `w.Off` / `w.Emit`
  (Go side) with `window.events` on the page (the events global's name is
  configurable through `App.Events`)
- App-scoped UI serving: set `App.FS` once (an `io/fs.FS`) and every view
  serves your UI from it at the uniform `app://` origin. Serving is
  scheme-first on Windows and Linux (WebView2's https vhost carries the
  isolation headers; Linux's registered custom `app://` scheme cannot -
  WebKitGTK can't attach the headers to scheme responses - but the JSC
  option still enables SharedArrayBuffer). macOS always serves over a
  per-view loopback `http://localhost` server (WKWebView cannot make a
  custom scheme a secure context and a long-standing WebKit bug keeps
  SharedArrayBuffer off plain WKWebView pages), and `App.HTTP` opts Linux
  and Windows into that same loopback origin - the loopback and vhost
  responses carry the cross-origin-isolation headers (COOP/COEP + CORP), so
  **SharedArrayBuffer is available on every platform**. Nothing is ever
  exposed beyond the loopback interface. The URL an app uses never changes
  per platform.
- Launch at login from Go: `autostart.New(id)` returns an `*Autostart` -
  `Enable(args...)` registers the running executable to start with the user
  session under `id`, `Enabled`/`Path`/
  `Backend` report the current registration and `Disable` removes it. The
  backend is per platform: XDG autostart `.desktop` file, `HKCU\…\Run`
  registry value, macOS LaunchAgent plist (or `SMAppService` for bundled
  macOS 13+ apps)
- Window state from Go, per `View`: `Show`, `Hide` (remove the window from
  the screen AND the taskbar/window list - the hide-to-tray pair),
  `Maximize`/`Minimize` and their inverses `Unmaximize`/`Unminimize` all work
  at runtime and are safe to call from any goroutine. `Focus`
  (keyboard focus into the web content) and
  `Raise` (front the window and activate the app) round out the runtime
  controls. Geometry, however, does **not** change at runtime - sizing and
  moving a window after creation is a defined non-feature of tuohi (the
  platforms cannot agree on it: Wayland compositors do not let a client move
  or resize its own toplevel, and GTK4 has no move API at all), so a unified
  runtime setter would silently fail on part of the target matrix. Set the
  initial geometry on the View itself - `Left`/`Top`/`Width`/`Height` plus
  the resize `State` - and if you
  really need to move or resize the
  window afterwards, take its native handle from `View.Window` (filled by
  `App.Show`) and use that platform's own API
- Frameless windows (the default - `View.Frame` is false) are fully
  transparent: no OS decoration of any kind and the desktop shows through
  everywhere the page does not paint. Give the page's `html`/`body` an
  explicit background when one is wanted; `View.Frame` true gives the
  ordinary OS-framed, opaque window
- Windows are declarative: define a `View` (geometry, options, bindings and
  the first-page `URL`), then `App.Show(&view)` creates it and navigates to
  `URL`. `view.Debug` enables the inspector, and `view.FirstMouse` opts macOS
  into first-click passthrough. For script that must run when the page comes
  up, call `view.Eval` from the `view.Ready` callback: Ready fires when the
  first page finished loading, so the DOM and the page's own scripts are
  already in place and one `Eval` reaches them reliably across all three
  engines. `view.window` (unexported) lets in-package embedders supply a host
  native window before Show.
- Plays nicely with `go.work` multi-module setups

## Desktop services

The root package `github.com/terva-sh/tuohi` is the window: `App`, `View`,
bindings, events, serving (`App.FS`, `App.HTTP`), the runtime icon
(`App.Icon`), and opening URLs or revealing files (`App.Open` /
`App.Reveal`). `App.Backend()` reports which web engine the app runs on -
`webkitgtk-6.0`/`webkit2gtk-4.1` on Linux, `WKWebView` on macOS,
`WebView2` on Windows.

The desktop services live in subpackages on the same cgo-free foundation.
None of them imports the root, and the root imports none of them except
`dialog`, so a program that only opens a window links none of their
dependencies: neither godbus (the Linux tray and notifications) nor the
clipboard's code. Where a platform cannot support something
cleanly, the API returns a clear `ErrUnsupported` instead of shipping
something flaky.

Each service that used to hang off `App` has moved:

| Was | Is now |
|---|---|
| `App.ID` and `App.Exec` (Single Instance Mode) | [`instance/`](instance/): `instance.Acquire(id, onMessage)`, `Lock.Release`, `instance.Send(id, args)` |
| `App.Autostart()` | [`autostart/`](autostart/): `autostart.New(id)`, with the same `Enable`/`Disable`/`Enabled`/`Path`/`Backend` |
| `App.Copy` / `App.Paste` | [`clipboard/`](clipboard/): `clipboard.Copy(text)` / `clipboard.Paste()`, on strings rather than bytes |
| `App.Notify` | [`notify/`](notify/): `notify.Show(app.Name, title, message)` |
| `App.Tray` | [`tray/`](tray/): `tray.Set(cfg)` from `App.Start`, `tray.Remove()` after `Wait` |

`View.Dialog` stays on the view, because a dialog needs its parent window;
the panels themselves are the [`dialog/`](dialog/) package.

**Single instance.** The first process to `Acquire` an id holds the lock;
a later one gets `instance.ErrAlreadyRunning`, hands its arguments over and
exits. The package does not exit for you, and the old `--new-instance`
override went with `App.Exec`: check your own arguments before `Acquire` if
you want one. On Unix the lock and socket live in a directory only the user
can use, never in `/tmp`, and on Windows the pipe is open to the user alone.
Either way the running instance takes messages only from the same user; the
package documentation says what that does and does not guarantee.

```go
const id = "com.example.app"
lock, err := instance.Acquire(id, func(m instance.Message) {
	// Runs on its own goroutine. m.Args and m.Dir, the later launch's
	// working directory, are untrusted input.
})
if errors.Is(err, instance.ErrAlreadyRunning) {
	if err := instance.Send(id, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	return
}
if err != nil {
	log.Fatal(err)
}
defer lock.Release()
```

**Autostart.** `autostart.New(id)` stores the registration under exactly
`id`, which must be 1-200 characters from `A-Za-z0-9._-`. It no longer falls
back to a name derived from `App.Name` or the executable. `Enable` replaces
an entry an older build registered under another name, because it removes
any entry that points at the same executable. The exception is a bundled
`.app` on macOS 13 and later: it registers itself through SMAppService as
its own login item, named by its bundle identifier, so `id` and the
arguments to `Enable` are not used there.

**Clipboard.** `clipboard.Copy` and `clipboard.Paste` run no external
program. On Linux, FreeBSD and NetBSD they call the GTK tuohi's window
loaded, so no `xclip`, `xsel` or `wl-copy` is needed, and on macOS
`NSPasteboard`. There they work once a tuohi `App` has shown its first
window, and until then return `clipboard.ErrNoApp`. On Wayland, and on X11
without a clipboard manager, copied text lasts only as long as the process.
On Windows they use the Win32 clipboard, which needs no tuohi window.

**Tray.** `App.Start` runs on the UI thread when `Wait` starts, before its
loop dispatches any event, which is where `tray.Set` must be called. An
error from it ends `Wait`. The tray icon is no longer derived from
`App.Icon`: pass your own `tray.Config.Icon`.

```go
cfg := tray.Config{
	Icon:    trayPNG,
	Tooltip: "my app",
	Items: []tray.Item{
		{Label: "Quit", OnClick: app.Quit},
	},
}
app.Start = func() error { return tray.Set(cfg) }
err := app.Wait()
tray.Remove()
```

## Install

```bash
go get github.com/terva-sh/tuohi@latest
```

## Requirements

tuohi binds the view the operating system already provides; there is nothing
to bundle, but that runtime must be present:

- **Linux**, **FreeBSD** and **NetBSD** - a system WebKitGTK with GTK4 or 
  GTK3; tuohi detects which at runtime, or you can pin one with the 
  `TUOHI_BACKEND` environment variable ([Choosing a stack](#choosing-a-stack-tuohi_backend) below).
  The exact libraries and how to install or debug them are in 
  [Linux shared libraries](#linux-shared-libraries) below.
- **Windows** - the Microsoft Edge WebView2 Runtime (preinstalled on current
  Windows 10/11; otherwise install the Evergreen Runtime). It is located via
  the registry, and `App.Show` returns an error if it is missing. To bundle
  zero native DLLs, tuohi calls the runtime's internal environment-creation
  export directly instead of shipping `WebView2Loader.dll`; that export is
  undocumented and could change in a future Edge runtime (in which case
  `App.Show` returns a clear error). See the note on `createEnvironment` in
  [lib_windows.go](lib_windows.go).
- **macOS** - nothing extra. The Cocoa/WebKit frameworks ship with the OS.

### Support tiers

- **Tier 1: Linux, macOS and Windows.** Every change to `main` is built and
  tested on the real engine: WebKitGTK 6.0 and 4.1 on amd64 and arm64 Linux,
  WKWebView on macOS, and WebView2 on Windows. The Linux GUI scenarios also
  gate every internal pull request before it merges; macOS and Windows are
  tested after the merge.
- **Tier 2: FreeBSD and NetBSD.** They must cross-build on every change, and
  nothing runs them.
- **Go 1.26 or newer,** except linux/s390x, which needs Go 1.27: purego
  reaches it without cgo only from 1.27 on.
- **glibc on Linux.** Even with `CGO_ENABLED=0`, a binary that reaches purego
  asks for glibc's loader, `/lib64/ld-linux-x86-64.so.2` on amd64, so it runs
  on glibc desktops. On musl, such as Alpine, it needs `gcompat`.

The other architectures in the table below compile, and nothing tests them.

### Supported platforms (build targets)

tuohi binds the OS web engine through [purego](https://github.com/ebitengine/purego)
(runtime `dlopen`, no cgo), so compilation tracks purego's supported platforms:

| GOOS    | GOARCH                                                                           |
|---------|----------------------------------------------------------------------------------|
| Linux   | amd64, arm64*, 386, armv7*, armv6*, armv5*, loong64*, ppc64le*, riscv64*, s390x* |
| FreeBSD | amd64, arm64*                                                                    |
| NetBSD  | amd64, arm64*                                                                    |
| Windows | amd64, arm64*, 386                                                               |
| Darwin  | amd64, arm64*                                                                    |

> linux/s390x needs Go 1.27 or newer; every other target builds with Go 1.26.
>
> Architectures marked with a `*` have only been tested to compile, not to run. If somebody has
> a machine of that architecture and can verify the runtime, please open an issue.



- **FreeBSD needs one build flag.** With `CGO_ENABLED=0`, purego's
  `internal/fakecgo` exports `environ` and `__progname` with a directive the
  compiler accepts only in cgo-generated code, so a FreeBSD build without cgo
  must compile that package with `-std`:

  ```sh
  CGO_ENABLED=0 GOOS=freebsd go build -gcflags=github.com/ebitengine/purego/internal/fakecgo=-std ./...
  ```

  NetBSD needs no flag.
- Runtime on the BSDs is **not** verified and depends on what the port
  provides: a desktop GTK/WebKitGTK with the sonames tuohi probes
  ([shared libraries](#linux-shared-libraries) - a BSD port may name them
  differently), a session D-Bus for the tray/notify/dialog backends, and
  working `flock`/Unix sockets for the `instance` package. Some helpers are
  Linux-specific at runtime (e.g. the `xdg-open` opener and the console-bell
  fallback in `notify`) and degrade or report unsupported elsewhere. Other
  GOOSes (OpenBSD, DragonFly, Solaris, AIX, Plan 9, js) have no lib-family
  backend and do not compile.

### Linux shared libraries

Linux is the hard case: every distro packages WebKitGTK a little differently,
but what tuohi needs is concrete. These are the exact sonames it tries to
`dlopen` at startup. They must be loadable by the dynamic linker (on the
default search path, in the `ldconfig` cache, or in `LD_LIBRARY_PATH`) and
match the **architecture of your binary** - a 64-bit Go build needs 64-bit
libraries.

Always loaded:

- `libglib-2.0.so.0`
- `libgobject-2.0.so.0`

`libwebkitgtk-6.0.so.4` decides the stack: if it loads, tuohi uses GTK4;
otherwise GTK3. It never loads both - most desktops have GTK3 and GTK4
installed side by side, and pulling both into one process corrupts GTK's type
system and crashes `gtk_init`.

- GTK4: `libgtk-4.so.1`, `libwebkitgtk-6.0.so.4`, `libjavascriptcoregtk-6.0.so.1`
- GTK3: `libgtk-3.so.0`, `libwebkit2gtk-4.1.so.0` (or `libwebkit2gtk-4.0.so.37`), `libjavascriptcoregtk-4.1.so.0` (or `libjavascriptcoregtk-4.0.so.18`)

Either stack needs WebKitGTK 2.40 or newer. The GTK4 stack needs GTK 4.12 or
newer, and the GTK3 stack GTK 3.20 or newer. An older library fails
`App.Show` with an error that names every function it lacks.

### Choosing a stack (`TUOHI_BACKEND`)

The `TUOHI_BACKEND` environment variable pins one of the two stacks before
the probe above runs - useful when both are installed and you want to force
one, or to reproduce a bug against a specific WebKitGTK:

- `TUOHI_BACKEND=webkitgtk-6.0` - the GTK4 stack
- `TUOHI_BACKEND=webkit2gtk-4.1` - the GTK3 stack (still falls back to the
  `-4.0` sonames inside that stack when `-4.1` is absent)

If the pinned backend's libraries cannot be loaded - or the value is anything
other than the two above - tuohi prints a warning to stderr and continues
with the auto-detected stack that works. macOS and Windows always use their
single built-in backend (WKWebView / WebView2) and ignore the variable.
`App.Backend()` reports the stack that was actually loaded (the showcase logs it
on every start).

The `TUOHI_*` environment variables were named `APPKIT_*` in v0.1.0-alpha.1,
and the old names are no longer read.

On the GTK4 stack, the file dialogs additionally load `libgio-2.0.so.0` the
first time a dialog opens (it ships with GLib, so it is present wherever the
libraries above are).

Installing the WebKitGTK package pulls GTK and GLib in as dependencies:

- Debian / Ubuntu: `apt install libwebkit2gtk-4.1-0` (GTK3) or `libwebkitgtk-6.0-4` (GTK4)
- Fedora: `dnf install webkit2gtk4.1` or `webkitgtk6.0`
- Arch: `pacman -S webkit2gtk-4.1` or `webkitgtk-6.0`
- Nix / NixOS: these libraries are not on the default loader path, so a bare
  `go run` outside a shell that provides them fails to load. Add
  `webkitgtk_4_1` (or `webkitgtk_6_0`) to your `buildInputs` / dev shell, or
  expose them through `LD_LIBRARY_PATH` or `nix-ld`.

If `App.Show` reports that none of the libraries could be loaded, the linker
cannot find the soname. See what is actually visible to it:

```bash
ldconfig -p | grep -E 'libwebkit(2)?gtk|libjavascriptcoregtk|libgtk-[34]'
```

`wrong ELF class: ELFCLASS32` means the library was found but in the wrong
architecture - a 64-bit binary was pointed at 32-bit libraries (check your
`LD_LIBRARY_PATH`).

The test suite reflects this: the GUI tests skip themselves when none of
these libraries can load, so `go test ./...` stays green on a box without
WebKitGTK instead of failing.

## Hello world

```go
package main

import (
	"log"

	"github.com/terva-sh/tuohi"
)

func main() {
	app := &tuohi.App{}
	view := &tuohi.View{
		Debug:  true, // inspector on (App.Debug turns it on for every view)
		Width:  800,
		Height: 600,
		URL:    "data:text/html,%3Ch1%3EHello%20from%20tuohi%3C%2Fh1%3E",
	}
	view.Ready = func() { /* the first page finished loading */ }
	if err := app.Show(view); err != nil {
		log.Fatal(err)
	}
	defer view.Close()

	if err := app.Wait(); err != nil {
		log.Fatal(err)
	}
}
```

Call `Show` and `Wait` from `main` and run everything else, such as an HTTP
server, in goroutines. macOS requires it: AppKit runs only on the process's
main thread, tuohi keeps the main goroutine there, and off it `Show` and
`Wait` return `ErrNotMainThread` unless a run loop already runs on the main
thread. Linux needs it too: WebKitGTK 2.54 aborts the process when it is first
used off the main thread, and tuohi keeps the main goroutine there as on
macOS. On Windows the goroutine that creates the first window is pinned to
its OS thread and becomes the UI thread. View methods such as
`Eval`, `Navigate` and `Close` are safe from any goroutine, and
`Window(func(unsafe.Pointer))` re-enters the UI thread from background work
(it hands you the native window handle).

## Desktop helpers

### Bind

Bindings expose Go values to the page as `window.*` JavaScript. They are
declarative maps - one on the app, one per view - and are bound automatically
when a window spawns, deterministically: the `App.Bind` entries first, then
the `View.Bind` entries, each map iterated in alphabetical key order, so the
outcome never depends on Go's map iteration order.

The entry's **key is a dotted path** that nests variables on the page: dots
separate levels under `window`, so a value bound at `app.someAPI.call` lives
at `window.app.someAPI.call`. What the entry's value becomes is decided by
its kind alone - one value always binds under exactly one name:

- A **Go function** becomes a JS function the page calls:
  `view.Bind["sum"] = fn` appears as `window.sum(...)`. The function's arity
  decides whether it ALSO works as a variable:
  - a **zero-argument function is a callable getter**: call it
    (`window.now()`) or read it as a value (`await window.now`, which calls
    the Go function with no arguments and resolves to its result) -
    `view.Bind["api.now"] = func() (time.Time, error) { return time.Now(), nil }`;
  - a **one-argument function is a callable setter**: call it
    (`window.log(msg)`) or assign to it (`window.log = msg`, which runs the
    Go function with the assigned value; the assignment expression yields
    `msg` itself - await the call form for the result) -
    `view.Bind["api.log"] = func(s string) error { ... }`.
  Functions with other arities are plain callables.
- A **length-2 array of two functions** - a getter and a setter
  (`[2]any{getter, setter}`) - becomes a property that is readable AND
  writable while staying wired to Go:
  `view.Bind["app.size"] = [2]any{getSize, setSize}` makes `window.app.size`
  a value the page can read (`const size = await window.app.size`, which runs
  `getSize` over the bridge) and write (`window.app.size = "9px"`, which runs
  `setSize` with the assigned value). The getter takes no arguments, the
  setter exactly one. To expose only one side, bind a lone function instead:
  a zero-argument function is a read-only getter, a one-argument function a
  write-only setter (see above) - `view.Bind["app.readOnly"] = getSize` reads
  only, `view.Bind["app.writeOnly"] = setSize` writes only.
- **Any other value** - a bool, number, string, or any JSON-encodable value
  such as a struct, map or slice - becomes an immutable JS **constant**
  bound wholesale under its name:
  `view.Bind["app.meta"] = Meta{Version: "1.2.0"}` appears as
  `window.app.meta` with `window.app.meta.version === "1.2.0"`.

Nothing is derived from the Go type: structs and maps are never walked, there
is no method expansion and no `bind:"…"` struct tag. A struct or map you want
to expose must spell out the names itself - either bind each function at its
own dotted key, or bind the whole value as one constant.

A page's calls are dispatched to Go **in the order it makes them**, so a read
issued after a write observes the write (`window.count = 1; await
window.count`). The calls still run off the UI thread, so a blocking binding
delays only that view's later calls.

Once the bindings of a page are installed, every object the binding process
created is **frozen**: the namespace containers, the function wrappers and
the constant trees are sealed (`Object.freeze`), so the page cannot mutate
the functions or constants it was given. The page's own `window` is left
alone.

Binding names are checked when the window is created, and a bad one fails
`App.Show` loudly instead of producing odd page objects: a dotted name must
have non-empty, whitespace-free segments (`"api.call"` is fine; `"a..b"`,
`".x"` and `"x.y z"` are not); a top-level name must not be one of the
common `window.*` built-ins (`close`, `open`, `name`, `fetch`, `document`,
…), tuohi's own internals (`__webview__`, anything starting `__tuohi`) or
the page's events global (`window.events` by default, whatever `App.Events`
renames it to); and a leaf and its namespace cannot both be bound (`"api"`
together with `"api.id"` is refused, because one would silently destroy the
other). A `View.Bind` entry under a name `App.Bind` already uses
**replaces** that binding (the view wins); a nil entry removes it.

Only **zero-argument functions are awaitable** (`await window.now`): bound
functions of any other arity are plain callables, so an accidental
`await window.fn` can never fire a no-argument Go call the function would
reject. Assigning to a setter or writing a variable (`window.log = msg`,
`window.app.theme = v`) runs the Go side, but an ECMAScript assignment
expression yields the ASSIGNED VALUE - the Go result is not observable
through the expression - so `await` the CALL form (`window.log(msg)`)
when the result matters. Errors are still visible: if the Go side fails
and nothing awaits the call, the rejection is rethrown to the page's
console, so a failure is never invisible.

Every bound function follows the same signature rules: no return value, a
value, an error, or value and error (the page gets a Promise either way).

`App.Bind` covers every window the app spawns; `View.Bind` is per-window and
its names win over the app-wide ones. A **nil** entry in `View.Bind` unbinds
that name for the view (dropping an app-wide binding the window does not
want); a nil entry in `App.Bind` binds nothing.

```go
app.Bind = map[string]any{
	"app.meta": map[string]any{"version": "1.2.0", "features": []string{"tray", "autostart"}}, // frozen constant
}
view.Bind = map[string]any{
	"sum": func(a, b int) int { return a + b }, // window.sum(...)
	"cfg.theme": "#203040",                     // window.cfg.theme
}

// Accessors: state stays in Go, the page reads and writes through the name.
theme := "ocean"
readTheme := func() (string, error) { return theme, nil }
writeTheme := func(s string) error { theme = s; return nil }
view.Bind["app.theme"] = [2]any{readTheme, writeTheme} // read + write
view.Bind["app.themeRO"] = readTheme                   // zero-arg func: read only
view.Bind["app.themeWO"] = writeTheme                  // one-arg func: write only

// app.Show(view) installs window.app.meta (constant), window.sum,
// window.cfg.theme (constant), the app.theme* accessors and freezes the
// namespace.
```

### Application lifecycle

There is no automatically created app window: the `App` only provides the
lifecycle around the windows you spawn. Windows are declarative - define a
`View` (geometry, settings, bindings all live on the struct), hand it to
`App.Show`, and keep the same pointer as the window's handle afterwards.
Then block with `App.Wait`, which runs the platform UI loop:

- The first `App.Show` (or `App.Wait`) performs the one-time app
  initialization: platform init and the best-effort runtime icon
  (`App.Icon`). Content serving starts per window, later: each view is
  served from `App.FS` through the platform's `app` scheme.
- `App.Start`, when set, runs on the UI thread as `Wait` starts, before the
  loop dispatches any event. Set up services that need the UI thread there,
  such as the tray; an error from it ends `Wait`.
- `Wait` returns when `App.Quit` is called, or when the last window spawned
  with `App.Show` closes and `App.Exit` is true. `Exit` defaults to false, so
  an app keeps running after its windows are gone (tray/menu-bar
  applications, background helpers) until it calls `App.Quit`.
- `App.Quit` ends a running application from any goroutine (`Wait` then
  returns). Calling it before `Wait` is harmless.
- The `App` is both the configuration and the scope of the application; its
  exported settings are committed the first time an App method is called.

```go
app := &tuohi.App{Name: "My App", Exit: true} // end when the window closes
view := &tuohi.View{
	Width: 1280,
	Height: 800,
	URL:   "https://example.com", // the first page; loaded by App.Show
}
if err := app.Show(view); err != nil {
	log.Fatal(err)
}
defer view.Close()

if err := app.Wait(); err != nil { // returns when the window closes or Quit is called
	log.Fatal(err)
}
```

The per-window knobs tuohi reads at window creation live directly on the
`App` and the `View` - there is no nested settings struct:

- `App.Debug` and `View.Debug` (default **false**) - the dev-tools /
  inspector switch. `View.Debug` opens one window's inspector; `App.Debug`
  applies app-wide; the two OR together. The `TUOHI_DEBUG=1` environment
  variable turns the tools on for every view only when the program sets
  `App.AllowEnvDebug`, so a release build is not opened to the inspector
  by whoever launches it.
  Backend mapping: WebView2 `DevTools`, WebKitGTK
  `enable-developer-extras`, `WKPreferences.developerExtrasEnabled`.
- `View.FirstMouse` (default **false**) - macOS first-click passthrough
  (see below).
- `App.Events` (default `""` → `"events"`) - the page-side JS global of the
  events bridge, `window.events` with `on`/`off`/`emit`.

```go
view := &tuohi.View{
	Debug: true, // inspector on for this window only
}
```

Page JavaScript is always enabled on every backend - there is no disable
knob, so the engines' JS switches stay at their on defaults. Each backend
keeps a few tuned defaults of its own (Linux media-stream + JS clipboard
access, macOS fullscreen, WebView2's hidden status bar), applied inline when
the view is created; they are not common knobs because the engines do not
agree on them.

Window content is served by your app through ONE app-scoped filesystem
(`App.FS`), and every view loads it from the same uniform **`app://`**
origin as a **secure, cross-origin-isolated context**:

- **Windows** serves the filesystem through WebView2's https vhost for the
  custom `app://` scheme; the vhost responses carry the isolation headers.
- **Linux/BSD** is scheme-first too: the registered custom `app://` scheme
  serves the filesystem (WebKitGTK cannot attach the isolation headers to
  scheme responses, so a scheme-served Linux page is not
  `crossOriginIsolated`; SharedArrayBuffer still works through the JSC
  option).
- **macOS** always serves over a per-view loopback
  `http://localhost` server - WKWebView cannot make a custom scheme a secure
  context, and a long-standing WebKit bug keeps SharedArrayBuffer off plain
  WKWebView pages - while the loopback origin is a secure, isolated context
  by itself. `App.HTTP` opts Linux and Windows into that same loopback
  origin too. The server lives as long as its view and listens on
  127.0.0.1 only. It answers only requests that name its own origin and
  carry its per-server token, which the view's first navigation exchanges
  for a cookie, so neither a web page reaching the port through DNS
  rebinding nor another user's process can read `App.FS` through it. A
  process running as the same user can read `App.FS` from the binary or
  from memory anyway, so keep secrets out of it.

Every response - loopback and vhost alike - carries the cross-origin-isolation
headers (`Cross-Origin-Opener-Policy: same-origin`,
`Cross-Origin-Embedder-Policy: require-corp`,
`Cross-Origin-Resource-Policy: same-origin`), so every app page is
**cross-origin isolated and `SharedArrayBuffer` is available** on every
platform (on Linux the engine additionally enables the JSC
`useSharedArrayBuffer` option, which some WebKitGTK builds gate behind
regardless of isolation). Because COEP is `require-corp`, cross-origin
subresources must carry a CORP header; cross-origin `fetch` follows the
remote's CORS headers exactly like any browser page.

### Frameless windows and drag regions

`View.Frame` is **false by default**, so windows are frameless: no OS
frame of any kind (no title bar, no system buttons) and a fully transparent
background - the desktop shows through everywhere the page does not paint.
Set `Frame: true` on the View for the ordinary OS-framed, opaque window. On
a frameless window your page is the chrome. Mark the movable boxes with the
custom CSS attribute `-app-region`:

```html
<style>
  .titlebar {
    -user-select: none;   /* the drag swallows clicks; no text selection */
    -app-region: drag;
  }
  .titlebar-button {
    -app-region: no-drag;
  }
</style>
<div class="titlebar">
  My App
  <button class="titlebar-button">×</button>   <!-- still clickable -->
</div>
```

- `-app-region: drag` - the box moves the window when dragged; clicks
  inside it are swallowed (like a real title bar). `no-drag` always wins, so
  a button marked `no-drag` inside a draggable bar stays interactive.
- Double-clicking a `drag` box toggles maximize/unmaximize (like a native
  title bar) on every platform - macOS, Linux (GTK3/GTK4) and Windows.
- The legacy `-webkit-app-region` (Electron) and `-webview-app-region`
  spellings are accepted as aliases, so existing stylesheets keep working
  unchanged.
- The attribute is tracked at runtime: styles, element positions and window
  size are watched, so regions follow scrolling, resizing and DOM changes.
- A resizable frameless window keeps native edge/corner resizing, including
  the correct `resize` cursor when hovering the edges/corners (`State`
  controls resizability as usual; `StateFixed` turns edge resizing off).

The [showcase](examples/showcase/) application runs frameless by default (fully transparent,
custom chrome) with a complete runnable title bar (drag anywhere on it, click
the dot to close) - the same chrome on every platform.

### Serving your UI (App.FS)

Set `App.FS` before the app scope opens (it is committed once, like every
`App` setting) and tuohi serves your content to every view from the uniform
`app://` origin:

```go
//go:embed ui
var uiFS embed.FS

app := &tuohi.App{
	Name: "My App",
	FS:   uiFS, // the whole UI: HTML, CSS, JS, assets
}
view := &tuohi.View{Debug: true}
if err := app.Show(view); err != nil {
	log.Fatal(err)
}
view.Navigate("app://index.html") // same uniform URL on every platform
```

The host after `app://` is an arbitrary origin identity (any host works); the
**path** selects the file in the filesystem: navigating to
`app://index.html` serves `uiFS`'s `index.html` with the right
Content-Type, `app://styles/app.css` serves `styles/app.css`, and so on.
A path that is not in the filesystem is answered as "not found".

Why not just `file://` or inline HTML? Because neither is a **secure context**,
and a large part of the web platform is gated behind one:

| Approach                       | Port?                | Secure context?                                                                                                                      |
|--------------------------------|----------------------|--------------------------------------------------------------------------------------------------------------------------------------|
| `file://` / inline HTML        | no port              | **no** - `crypto.subtle` is undefined, `getUserMedia`/geolocation are blocked, `localStorage` is unreliable, routing is hash-only    |
| **`app://` filesystem (this)** | **no external port** | **yes, and cross-origin isolated** - `localStorage`, `crypto.subtle`, `SharedArrayBuffer`, `getUserMedia`, and path routing all work |

Each backend uses its own native mechanism. **Windows** has no per-scheme
secure flag, so there the scheme is
served over a per-scheme `https://<scheme>.localhost` virtual host (an https
origin is a secure context) and `Navigate` rewrites `app://…` to it; the
vhost responses carry the isolation headers. **Linux** serves the same way
through its registered custom `app://` scheme (WebKitGTK cannot add the
isolation headers to scheme responses, so a scheme-served Linux page is not
`crossOriginIsolated`; `SharedArrayBuffer` still works via the JSC option).
**macOS** always serves over a per-view loopback `http://localhost`
server - WKWebView cannot make a custom scheme a secure, isolated context
and a long-standing WebKit bug keeps SharedArrayBuffer off plain pages - and
`App.HTTP` opts Linux and Windows into that same loopback origin, whose
responses carry the COOP/COEP/CORP isolation headers. `SharedArrayBuffer` is
available on every platform. The
[showcase](examples/showcase/) serves its own UI through this one `App.FS` on every
platform.

### The main menu (macOS)

On macOS, tuohi installs a standard main menu when it creates the first
window, so the usual keys work:

- the application menu, with About, Hide (⌘H), Hide Others (⌥⌘H), Show All,
  and Quit (⌘Q), which calls `App.Quit`;
- Edit, with Undo (⌘Z), Redo (⇧⌘Z), Cut (⌘X), Copy (⌘C), Paste (⌘V), and
  Select All (⌘A), which a page's text fields need;
- Window, with Minimize (⌘M) and Close (⌘W), which also work on frameless
  windows.

The application menu takes its title from `App.Name`, or the process name.
The menu cannot be replaced or extended yet. A main menu that something else
installed first, such as an embedding host's, is left as it is.

### First click on an inactive window (macOS)

On macOS a click on a window that does not have focus is spent *activating*
the window: it never reaches the page. For a control panel, a dashboard or a
player - anything the user clicks in passing - that reads as a broken button,
and the user ends up clicking twice.

```go
app := &tuohi.App{}
view := &tuohi.View{FirstMouse: true}
if err := app.Show(view); err != nil {
	log.Fatal(err)
}
```

It is **opt-in**, and deliberately so: the AppKit default is what protects
destructive interfaces. In a drawing tool, an editor, or any window with a
delete button, a click that merely raises the window must not also press
whatever happens to sit under the cursor. Leave it off when a stray first
click could destroy something.

macOS only; ignored on Linux and Windows, where a click on an inactive window
already reaches the content. The mechanism is a `WKWebView` subclass answering
`YES` to `acceptsFirstMouse:` - AppKit asks the *view* under the cursor, so
there is no window-level or runtime switch for it.

**It is not always enough.** AppKit delivers the click to the view, but WebKit
hosts the page in another process and does not always forward that first click
to the DOM while the window is not key. When a program *knows* it took its own
focus away (it launched a window that activates, say), the reliable answer is
to take the focus back:

```go
w.Focus(true) // front the window and activate the app; the next click just works
```

`Raise` is the blunt instrument and should be used sparingly - stealing focus
from someone typing in another application is worse than the second click it
saves. `Focus` is the other half: it moves the caret *inside* the page.

### Events

A lightweight publish/subscribe bridge between Go and JavaScript, layered on
`Bind`/`Init`/`Eval` with no extra native code. Every spawned `View` carries
its own bridge - `App.Show` installs it at creation, so `w.On`/`w.Off`/
`w.Emit` always work and there is no separate handle to create. An event
reaches every listener on both sides exactly once.

```go
view := &tuohi.View{}
if err := app.Show(view); err != nil {
	log.Fatal(err)
}
w := view

// Go subscribes; each argument arrives as raw JSON to decode as you like.
w.On("ui:save", func(args ...json.RawMessage) {
	var name string
	_ = json.Unmarshal(args[0], &name)
	log.Println("save requested for", name)
})

// Go emits to JS - safe to call from any goroutine.
_ = w.Emit("app:ready", map[string]any{"version": 3})
```

```js
// JS subscribes to Go events and emits its own.
events.on("app:ready", (info) => console.log("ready", info.version));
events.emit("ui:save", "untitled.txt");
```

`On` returns a function that cancels that one subscription; `Off(name)` drops
all of them. Go handlers run on the goroutine that emitted (or the binding
goroutine for events coming from JS), so re-enter the UI thread with
`Dispatch` if a handler touches the window. The [showcase](examples/showcase/) Events card
shows both directions live.

### File dialogs

Native open/save/directory dialogs live in the standalone
[`dialog`](dialog/) package
(`github.com/terva-sh/tuohi/dialog`), which shows the panels without any
window. A single entry point, `dialog.Open`, presents whatever panel
`dialog.Options.Type` selects (`TypeOpen`, `TypeOpenMultiple`, `TypeSave` or
`TypeDirectory`) from the program's main thread and returns the chosen paths
(or `nil` when cancelled). The View exposes the same through one
`Dialog` method, which dispatches onto the UI thread for you and blocks the
calling goroutine:

```go
paths, _ := w.Dialog(dialog.Options{
	Type:    dialog.TypeOpen,
	Title:   "Open an image",
	Filters: []dialog.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}},
})
paths, _ = w.Dialog(dialog.Options{Type: dialog.TypeOpenMultiple})     // multi-select
paths, _ = w.Dialog(dialog.Options{Type: dialog.TypeSave, Filename: "untitled.txt"})
paths, _ = w.Dialog(dialog.Options{Type: dialog.TypeDirectory})
```

Backends: `NSOpenPanel`/`NSSavePanel` (macOS), `IFileOpenDialog`/
`IFileSaveDialog` (Windows), `GtkFileChooserNative` (Linux). Each shows the
modal dialog, blocks the calling goroutine, and returns the chosen path(s) or
`nil` on cancel. Call the View method from `Bind` callbacks (a background
goroutine), never from the UI thread. The [showcase](examples/showcase/) Dialogs card drives
all four panel kinds; [`examples/dialog`](examples/dialog/) shows the standalone
package on its own.

## System tray

A tray icon with a menu is the [`tray`](tray/) package's job
(`github.com/terva-sh/tuohi/tray`). Standalone, `tray.Run` owns the
process's UI event loop and blocks until `tray.Stop` (see
[examples/tray](examples/tray/)); macOS, Windows and Linux are implemented. Linux runs
over a D-Bus StatusNotifierItem + `com.canonical.dbusmenu` export, so no
desktop is excluded.

The `Config` is fully declarative and read once by `Run`/`Set` - PNG icon
(plus a `DarkModeIcon` for Windows theme switching and a `TemplateIcon` for
macOS menu-bar recoloring), tooltip, tray-level `OnClick`/`OnDoubleClick`/
`OnRightClick`, and the whole menu tree with `Checkbox`, `Disabled`,
`Separator`, `Submenu` and per-item `Icon` entries:

```go
app := &tuohi.App{
	Name: "my app",
	Exit: true, // end the process when the last window closes
}
cfg := tray.Config{
	Icon:    trayPNG,
	Tooltip: "my app",
	Items: []tray.Item{
		{Label: "Open", OnClick: openUI},
		{Separator: true},
		{Label: "Quit", OnClick: app.Quit},
	},
}
app.Start = func() error { return tray.Set(cfg) } // on the UI thread, before the loop
view := &tuohi.View{
	Width:  1024,
	Height: 768,
}
if err := app.Show(view); err != nil {
	log.Fatal(err)
}
view.Navigate("https://example.com")
err := app.Wait() // runs the loop, which dispatches the tray's menu events
tray.Remove()
if err != nil {
	log.Fatal(err)
}
```

`tray.Set`/`tray.Remove` show and hide the icon without owning the loop:
call `Set` from the UI thread and let your own loop dispatch the menu
events, which is what `App.Start` and `App.Wait` give you. A menu item's
`OnClick` runs on the UI thread; keep it short or hand the work to a
goroutine.

The tray does not take its icon from `App.Icon`: set `tray.Config.Icon`.

Only one tray may be active per process; a second `Set`/`Run` returns
`ErrAlreadyRunning`. `tray.Bounds` reports the icon's on-screen rectangle
where the OS exposes one (Windows). See [tray/README.md](tray/README.md) for
the full API table, threading rules and per-platform behavior, and
[examples/tray](examples/tray/) for a runnable example.

## Desktop notifications

OS-level notifications are the [`notify`](notify/) package's job
(`github.com/terva-sh/tuohi/notify`): title + message, with no window and no
tray icon required. macOS uses `NSUserNotificationCenter`, Windows a
`Shell_NotifyIconW` balloon, Linux `org.freedesktop.Notifications` over D-Bus
(with a `notify-send`/`kdialog` fallback). The standalone package goes beyond
the plain notification: `ShowOpts` attaches a custom icon and an urgency,
`Alert` posts a critical notification with the platform's attention sound, and
`Beep` sounds a tone directly (PC speaker on Linux, kernel beep on Windows,
system beep on macOS).
`App` has no notification method; call the package directly. It names the
source per call, `notify.Show(name, title, message)`, where an empty name
falls back to the executable's name, and it is safe from any goroutine:

```go
app := &tuohi.App{Name: "backup tool"}
if err := notify.Show(app.Name, "Backup finished", "Snapshot complete"); err != nil {
	// errors.Is(err, notify.ErrUnsupported) on unsupported platforms
}
```

Reach for `notify.ShowOpts`/`notify.Alert`/`notify.Beep` when you need icons,
urgency or a sound. See
[notify/README.md](notify/README.md) and [examples/notify](examples/notify/) for a
runnable example.

## Examples

[`examples/`](examples/) holds tuohi's reference programs. They are a Go
module of their own, so they use tuohi the way a consumer does, from outside
and through its public API only; `make check-examples` refuses an example that
reaches into a tuohi internal package. CI builds them for every target and,
wherever a window can open, runs them as programs, so a change that breaks how
a consumer uses tuohi fails in tuohi's own CI. Run them from `examples/`:

```bash
cd examples
go run ./loopback                  # an existing net/http interface in a window
go run ./showcase                  # every feature in one borderless window
go run ./showcase -http            # same, served over a per-view loopback
                                   # http://localhost server (App.HTTP) -
                                   # Linux/Windows opt in; macOS always does
go run ./showcase -tray            # same + a tray menu (Show / Hide / Quit)
go run ./showcase --framed         # same, with the OS window frame
go run ./showcase --selftest       # showcase + automated self test (exit 0/1)
go run ./tray                      # tray icons, menus, checkboxes
go run ./notify                    # notifications
go run ./dialog                    # all four native file panels
```

[`loopback`](examples/loopback/) is the shape terva uses: the program keeps
its `net/http` server on 127.0.0.1, points the window at it, and binds what
the page needs from Go. `go run ./loopback -check` loads the page, checks that
it reached both the server and Go, and exits 0 or 1.

[`showcase`](examples/showcase/) is a single borderless, cross-platform window:
custom chrome whose maximize button toggles into a restore button, its UI
served through the one app-scoped `App.FS`, the JS bridge, events, clipboard,
autostart, native dialogs, notifications, an opt-in tray (`-tray`) that
hides, un-minimizes and shows the window, and open/reveal. The page is loaded
from `app://app/index.html` on every platform - scheme-first on Windows and
Linux (Linux's scheme is not `crossOriginIsolated`, but SharedArrayBuffer
works via the JSC option), macOS via the loopback origin (WKWebView SAB bug),
with SharedArrayBuffer available everywhere.

The tray is opt-in via `-tray`: by default the windowed showcase keeps its
Dock/taskbar icon. Configuring a tray runs the app as a menu-bar "accessory"
app (no Dock icon) on macOS, so pass `-tray` only when you want that
hide/show-from-menu example.

`--selftest` drives a real view and is the project's UI-automation hook: the
page exposes stable ids and a `#selftest` suite whose verdicts are reported
back to Go (it prints `selftest N/N passed` and exits 0/1). The suite covers
the bridge add/echo, every binding form (constant, function, accessor pair),
the events round trip, clipboard, autostart, the drag region, stable ids, the
isolated context (SharedArrayBuffer) and the maximize toggle.

Each example opens real platform UI (WebKitGTK on Linux, WebView2 on Windows,
WKWebView on macOS). The showcase and loopback tests run their program only
when `TUOHI_REQUIRE_GUI=1` says a window can open, as `just test-gui` and the
GUI jobs in CI do, and skip otherwise.

## Testing

```bash
go test ./...
```

This runs the pure-logic unit tests (binding marshalling, single-instance,
events) plus the per-platform GUI smoke tests, which drive a real view
(WKWebView / WebKitGTK / WebView2). Those GUI tests **skip themselves** when
the system view cannot run here - no display, or the libraries are not
installed (WebKitGTK on Linux, the Edge WebView2 Runtime on Windows) - so the
command above stays green on a headless or minimal box instead of failing.

For a fast, headless run, `-short` skips the GUI scenarios on every platform
(each drives a real run loop and can take a few seconds):

```bash
go test -short ./...
```

To actually exercise the GUI tests on Linux, install WebKitGTK and run under
a virtual display:

```bash
xvfb-run -a go test ./...
```

## Building on Windows

Use `windowsgui` to hide the console window:

```bash
go build -ldflags="-H windowsgui" .
```

## Project layout

- `lib_darwin.go` / `lib_unix.go` / `lib_windows.go` - the per-platform
  engine layer: every direct platform-API call (WKWebView and WebKitGTK
  through the purego objc/GTK bindings, WebView2 and Win32/COM), per-OS view
  init (`ensureInit` on macOS/Linux, `ensureWinInit`/`ensureCOMInit` on
  Windows) and the per-backend `bridgePostFn`; the per-OS `newView(v *View, serve)`
  window constructor (which registers the `app` scheme serving the app's
  `App.FS`, starts the per-view loopback server for HTTP-served views and
  applies the window settings inline) lives
  here. Nothing engine-independent lives here
- `view.go` - the view/window API surface: the declarative `View` struct
  (window configuration + post-spawn handle and methods), the `App.Show`
  entry point, geometry + `State`, the `Bind` maps, binding/JS-bridge
  marshalling, the View `Dialog`
  method (over `dialog/`), the internal content request/response types, and
  the CSS drag-region machinery
- `app.go` (+ `app_{darwin,unix,windows}.go`) - the whole
  application scope and app-scoped code: the `App` type (configuration +
  runtime scope with lazy commit and one-time `ensureInit`), `App.Wait`/`Show`
  and the `App.Start` hook, `Open`/`Reveal`, the runtime icon, the
  `serveAppFS` content resolver for `App.FS` and the remaining framework glue
  (the per-view events bridge `On`/`Off`/`Emit`, the app-wide `App.Bind` map)
- `examples/` - the reference programs, a module of their own (see
  [Examples](#examples)): `showcase` (every feature in one window, with a
  `--selftest` UI-automation hook), `loopback` (an existing `net/http`
  interface in a window), and `tray`, `notify` and `dialog`
- `tray/` - the standalone declarative system-tray package; macOS/Windows/
  Linux backends
- `notify/` - the standalone desktop-notification package: plain `Show`,
  `ShowOpts` with icon/urgency, `Alert` and `Beep`
- `instance/` - single instance: `Acquire`, `Lock.Release` and `Send`
- `autostart/` - launch at login: `New(id)` and the per-platform backends
- `clipboard/` - text `Copy` and `Paste`
- `dialog/` - the standalone native file-dialog package

tuohi loads the OS view framework directly and bundles or extracts no native
library, so there is no extracted file to verify or swap.
