---
schema: 4
id: TKT-01M3J59M1H9PZ04J2C9JJZ7V13
title: Move desktop services out of the root package
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: feat/split-services
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-split
  commit: 03a0219ef0a9a34038a4bbcc6890aba910c31b98
  session: null
  claimed_at: 2026-09-29T21:44:35Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T21:52:11Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Move the desktop services out of the root `tuohi` package so a program that only opens a window links none of them. The owner chose this on 2026-09-27 during the architecture review. Every feature stays. The design is in `docs/architecture.md`, under "The root package is the window".

### Why

The root imports `notify`, `tray`, and `dialog` (`app.go:51-53`, `view.go:10`) and `github.com/atotto/clipboard`, so a window-only consumer links godbus and atotto. atotto's `init` also runs `exec.LookPath` at process start. git-ticket-canvas needs a window, a folder dialog, and single instance, and nothing else.

### Target layout

| Package | Holds | Replaces on `App` |
|---|---|---|
| `tuohi` | `App`, `View`, bindings, events, serving (`App.FS`, `App.HTTP`, app://) | nothing |
| `tuohi/dialog` | file and folder dialogs (exists) | `View.Dialog` stays as a thin helper, because a dialog needs its parent window |
| `tuohi/instance` | single instance: `Acquire(id, onMessage)` and `Send` | `App.ID`, `App.Exec` |
| `tuohi/autostart` | launch at login | `App.Autostart()` |
| `tuohi/clipboard` | native clipboard read and write | `App.Copy`, `App.Paste` |
| `tuohi/notify` | notifications (exists) | `App.Notify` |
| `tuohi/tray` | tray icon and menu (exists) | `App.Tray` |

`App.Icon` stays in the root. It is part of how windows look. The GTK3 Wayland desktop-file writing moves behind an explicit opt-in (TKT-01M3HWWRYD7GNZEZA2JCGWGDJS).

Where a service needs the UI thread (a macOS tray, a GTK clipboard), the subpackage takes what it needs from `tuohi` through an exported hook, rather than `tuohi` importing the subpackage.

## Acceptance criteria

- [x] A program that imports only tuohi and opens one window links neither godbus nor atotto, shown with go list -deps
- [x] Single instance, autostart, and clipboard live in their own subpackages, and notify and tray are no longer imported by the root
- [x] Every feature the root offered before is still reachable, and the README says where each moved

## Implementation plan

The API was chosen before the move, following docs/architecture.md:

- **`tuohi/instance`.**
  - `Acquire(id, func(Message)) (*Lock, error)`, `Lock.Release`, `Send(id, args)`, and `ErrAlreadyRunning`.
  - `Message` is a struct, so TKT-01M3HWWRVGMZTXBYJ86FCTH0V8 can add the working directory without breaking callers. The wire format becomes `{"args":[...]}`.
  - The library no longer calls `os.Exit` or reads `--new-instance`. The consumer writes that pattern, and the README shows it.
- **`tuohi/autostart`.** `New(id)` with the old five methods, and `ErrUnsupported`. The id is exactly what the consumer passes, validated as before. The name, executable and "appkit-app" fallbacks go.
- **`tuohi/clipboard`.** `Copy(string)` and `Paste() (string, error)`, on strings, because the native backends in TKT-01M3J59M2BR91XQBDT1TPPM4G5 are text clipboards. It wraps atotto until that ticket.
- **Notify.** `App.Notify` goes, and consumers call `notify.Show(app.Name, ...)`.
- **Tray.**
  - `App.Tray` goes. The new `App.Start func() error` runs on the UI thread as `Wait` starts, before its loop; an error ends `Wait`.
  - The tray is set up there, and `tray.Remove` runs after `Wait`.
  - The tray icon is no longer derived from `App.Icon`.
- **Why a hook on App.** A tray must be set up on the thread whose loop dispatches its events, which `Wait` pins. The alternatives:
  - A consumer calling `tray.Set` before `Wait` can land on another OS thread on Windows.
  - An `App.Do(func())` method cannot run before a loop exists.

The move itself was done by a subagent under a written brief, then reviewed and re-verified in this session.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T21:44:35Z

### Decisions made during the move

- **Unix and macOS single instance are one file**, `instance/instance_unix.go`. The one real difference: macOS used `XDG_RUNTIME_DIR` without checking that it was writable. It now takes the Linux path. Solaris and AIX lack `syscall.Flock`, so they would not build `instance`, but the root does not build there either.
- **The root's Windows `ensureInit` is a no-op.** It loaded only the named-pipe functions, which moved with single instance. The engine loads its own libraries when it creates the first window.
- **SMAppService on macOS was broken, and is now defensive.** The root looked the class up with `class()`, which panics on a missing class, and nothing loaded ServiceManagement.framework. The package now loads the framework once, and treats a missing class as unavailable, which falls back to the LaunchAgent. It only runs for a bundled app on macOS 13 or later, so nothing here tests it.
- **The XDG autostart entry's `Name=`** is now the executable's base name. That was already the fallback when `App.Name` was empty.
- **`autostart_other.go`** returns `ErrUnsupported` on platforms without a backend.
- **The demo.**
  - It registers autostart as "tuohi-demo". `Enable` removes any older entry for the same executable.
  - It embeds a 32px `tray.png`, made from `app.png` with the removed resizer, so its tray looks the same.
- **Test coverage.** The Linux wait GUI scenario now checks that `App.Start` runs on the UI thread, and that its error ends `Wait`.

### Verified at this head

- `just ci` passes. `just test-gui` passes on both stacks for tuohi, autostart, dialog, instance, notify, and tray.
- golangci-lint reports 0 issues for linux, darwin, windows, netbsd, and freebsd.
- **Criterion 1.** A program that imports only `github.com/terva-sh/tuohi` and shows one View was checked with `GOOS=$os go list -deps . | grep -E 'godbus|atotto|terva-sh'`. For linux, darwin, and windows, it lists only `tuohi` and `tuohi/dialog`. Against the base commit, the same program listed atotto on all three, and godbus on Linux.
- macOS and Windows code only builds, vets, and lints here. It moved rather than changed, apart from SMAppService. GitHub CI tests it after the merge.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:52:11Z

### terva-review disposition, PR #38, first round (243e45d)

**Accepted: "Honor the supplied autostart ID for bundled macOS apps" (medium).** For a bundled .app on macOS 13 or later, Enable takes the SMAppService path. That registers the app as its own login item under its bundle identifier and never uses the id, while the new docs promised the id exactly. The behaviour predates the split: App.Autostart did the same.

It was fixed in the docs, not the code (e33a035). The package doc, New, and the README now name the exception, including that a login item starts without Enable's arguments.

Rejected alternatives:
- Falling back to a LaunchAgent whenever the id differs from the bundle identifier. A login item is the registration macOS expects from a bundled app.
- Refusing the call in that case. Enable would then fail for a correct app over a naming mismatch.
