---
schema: 4
id: TKT-01M3HWWRRXTAR4T01SK79Z4BSM
title: Linux GUI scenarios crash in view teardown on both WebKitGTK stacks
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-linux
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-92c88910
  branch: fix/sync-github-flags
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-92c88910
  commit: 92c6ca54a2c7cec77437d49005b0010dc597980d
  session: null
  claimed_at: 2026-09-27T18:15:31Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T18:22:05Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
extensions: {}
---

## Description

### What

With the GUI probe fixed, the Linux GUI scenarios crash the test binary on both WebKitGTK stacks, built with `CGO_ENABLED=0`, under `xvfb-run` and `dbus-run-session`, on Debian 13 with WebKitGTK 2.52.6:

- **WebKitGTK 4.1 (GTK3):** SIGSEGV at address 0x18 during a `pure` call in the teardown path, reached from `lib_unix.go:1025` in `(*webview).Destroy`.
- **WebKitGTK 6.0 (GTK4):** SIGABRT. GDK warns `gdk_gl_context_make_current() failed`, then libepoxy asserts "Couldn't find current GLX or EGL context". This one may be Xvfb having no GL rather than tuohi. Check with `LIBGL_ALWAYS_SOFTWARE=1`, `WEBKIT_DISABLE_COMPOSITING_MODE=1`, or `-screen 0 1600x1000x24 +extension GLX`.
- **With cgo on:** an earlier run crashed in `View.Close` → `(*webview).Destroy` at `lib_unix.go:1050`, with a GLib `g_object_unref` assertion. Only the no-cgo build matters, but the trace points at the same teardown.

### Why it matters

A consumer that opens and closes windows during its lifetime hits `Destroy`. The git-ticket-canvas prototypes did not notice, because they exit through `App.Quit` and never close a view directly.

### Depends on

The probe fix, which is what lets these scenarios run at all.

## Acceptance criteria

- [x] The cause of the WebKitGTK 4.1 SIGSEGV in Destroy is found and fixed
- [x] The GTK4 abort is either fixed or shown to be the test display's missing GL, with the harness adjusted
- [x] just test-gui passes on both stacks with CGO_ENABLED=0

## Implementation plan

### Root cause

There were two separate causes, one per symptom.

1. **`Destroy` ran GTK off the UI thread.** `View.Close` is documented safe from any goroutine, and bindings run on `serialQueue` goroutines, off the UI thread. Every scenario closes its view from inside a binding, and the Unix `Destroy` called `gtk_window_close` and `g_object_unref` directly on that goroutine, racing the main loop. That caused the WebKitGTK 4.1 `g_object_unref` abort at line 1050, the SIGSEGV at `gtkWindowClose` (line 1025), and the GTK4 `g_list_store_remove`, `gtk_native_unrealize`, and `object_already_finalized` cascade on GitHub. The macOS engine already runs its teardown via `performOnMain`, and Windows dispatches `destroyOnUI` when off its UI thread. Only Unix did not.
2. **A GTK3-only call in `embedScenario`.** The scenario calls `gtkWindowResize(host, ...)` after `Close`. `gtk_window_resize` does not exist in GTK4, so the Go func var is nil, and calling it faults. A C library in the GTK4 process has installed a SIGSEGV handler without `SA_ONSTACK`, so Go cannot turn the fault into a panic and dies with "non-Go code set up signal handler without SA_ONSTACK flag". This was a test bug, not a teardown bug.

### Approach

- **Record the UI thread.** When `newView` pins the UI thread, it stores GLib's `g_thread_self()` in an `atomic.Uintptr`. `onUIThread()` compares against it and is true before any view exists. `g_thread_self` is in libglib on Linux, FreeBSD, and NetBSD, so no per-OS thread-ID call is needed.
- **Marshal `Destroy`.** `Destroy` releases the loopback server in place, then runs the GTK part (`destroyOnUI`) directly on the UI thread, or through `dispatchMain` from any other thread. This matches the Windows engine: asynchronous, not waiting on the main loop, so a caller on a goroutine cannot deadlock against a loop that has stopped.
- **Fix the scenario.** `embedScenario` uses `gtk_window_set_default_size` on GTK4.

### Alternatives considered

- **Synchronous marshal that waits for the main loop, as macOS's `performOnMain` does:** rejected. On GTK nothing guarantees the loop is still iterating when a goroutine calls `Close`, for example after `Run` returned, and a wait would then hang forever. Windows made the same choice for the same reason.
- **`g_main_context_is_owner` as the UI-thread test:** rejected. The default context is only acquired during an iteration, so it is false on the UI thread between iterations.
- **Per-OS thread IDs (`gettid`, `thr_self`, `_lwp_self`):** rejected. That would be three code paths where GLib offers one.
- **GL workarounds for GTK4** (`LIBGL_ALWAYS_SOFTWARE`, `WEBKIT_DISABLE_COMPOSITING_MODE`, `+extension GLX`, `GSK_RENDERER=cairo`): tried, and none changed the GTK4 crash. The missing GL was not the cause.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T17:24:52Z

### Observations from TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG's verification (2026-09-27, b8b54ad, CGO_ENABLED=0)

With the probe fixed, `just test-gui` reaches the scenarios on both stacks. The first scenario, `bridgeScenario`, crashes on its deferred `View.Close` (lib_unix_test.go:151). The signatures differ from this ticket's description and vary between runs:

- **webkitgtk-6.0 (GTK4):** run 1 gave a SIGSEGV and run 2 a SIGABRT, both after `Gdk-WARNING gdk_gl_context_make_current() failed`, both at `lib_unix.go:1025`, which is `gtkWindowClose(w.window)` in `Destroy`.
- **webkit2gtk-4.1 (GTK3):** `GLib-GObject-CRITICAL g_object_unref: assertion 'G_IS_OBJECT (object)' failed`, then SIGABRT at `lib_unix.go:1050`. That is the same site as the cgo-on trace recorded above, so it is not cgo-specific.

The runs used no `LIBGL_ALWAYS_SOFTWARE` and no `WEBKIT_DISABLE_COMPOSITING_MODE`, both of which GitHub's unix job sets.

**agent:claude-code/t3code-92c88910** at 2026-09-27T18:11:16Z

### GitHub mirror is red on main because of this bug (2026-09-27, 3adf498)

GitHub run 36339527184 on 3adf498 fails all four Linux GUI jobs: ubuntu-latest and ubuntu-24.04-arm, gtk3 and gtk4. macOS, Windows, lint, and every build pass. Bubblewrap is installed on GitHub's runners too, so the scenarios had been skipping there as well until TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG (GUI tests skip wherever bubblewrap is installed) fixed the probe. These are the first real Linux GUI runs anywhere. They run with cgo on and set `LIBGL_ALWAYS_SOFTWARE`, `WEBKIT_DISABLE_DMABUF_RENDERER`, and `WEBKIT_DISABLE_COMPOSITING_MODE`.

- **gtk3, both arches:** `g_object_unref: assertion 'G_IS_OBJECT (object)' failed`, then SIGABRT at `lib_unix.go:1050`, from `Destroy` reached at lib_unix_test.go:152/162 on amd64 and 190/198 on arm64. The same site as the local no-cgo run, so the crash is not about GL or cgo.
- **gtk4, both arches:** repeated `g_list_store_remove: assertion '!g_sequence_iter_is_end (it)' failed`. On arm64 that is followed by `gtk_native_unrealize: priv != NULL`, `gdk_gl_context_make_current: GDK_IS_GL_CONTEXT`, `g_object_unref: '!object_already_finalized'`, and `_gdk_frame_clock_uninhibit_freeze`, which points to a double finalize of the window. The package then FAILs.

The `release` job needs `unix`, so no tag can publish until this lands.

**agent:claude-code/t3code-92c88910** at 2026-09-27T18:20:14Z

### Verified (2026-09-27, Debian 13, WebKitGTK 2.52.6)

- **`just test-gui`:** passed 3 of 3 runs, both stacks, CGO_ENABLED=0, all five scenarios running (TUOHI_REQUIRE_GUI=1).
- **With cgo on,** as GitHub builds: both stacks pass.
- **Without the `Destroy` change** (the test fix alone): 4.1 SIGABRT, GTK4 SIGSEGV. The marshalling is required, not incidental.
- **Cross builds:** `GOOS=freebsd` and `GOOS=netbsd` build. `just ci` passes.

### Same bug class, left out of this change

On Unix, `webview.Navigate`, `webview.Eval`, and `webview.Focus` call WebKitGTK directly on the caller's goroutine. `View.Focus`'s doc says "the backends marshal to the UI thread when called from a background goroutine", which is false on Unix. `Show`, `Hide`, `Maximize`, and the other window-state methods do marshal. `View.Close` also reads and clears `v.w` without synchronization, so two concurrent `Close` calls race at the View level. None of this caused the teardown crash, so it is filed separately rather than folded in.

### SA_ONSTACK

Some C library in the GTK4 process installs a SIGSEGV handler without `SA_ONSTACK`. While it is installed, any nil dereference in Go code, a consumer's included, is a fatal error with no panic and no stack instead of a recoverable panic. Not investigated further here. The architecture review should decide whether it matters.

**agent:claude-code/t3code-92c88910** at 2026-09-27T18:22:05Z

### Review disposition, PR #3, terva-review run dc571f2f on 74e2143

- **medium: Background Close can leave GTK teardown pending after the main loop stops. Documented in 5d14eb5; the behaviour is kept, and the lifecycle rule goes to the architecture review.**
  - **What happens:** the queued idle source stays on GLib's default context and runs when the UI thread next iterates it: the next `Run`, `App.Wait`, or a UI-thread `Destroy`'s drain loop. It is lost only if the UI thread never iterates again, which in practice means the process is exiting.
  - **Why this is not new:** `Terminate`, `Show`, `Hide`, and the window-state methods on Unix rely on the loop the same way, as does the Windows engine's `Destroy`.
  - **Why the alternatives lose:** the only other ways to "complete teardown" are to run GTK off the UI thread, which is the crash this PR fixes, or to wait for the loop, which hangs when the loop has stopped.
  - **Follow-up:** a lifecycle rule for calls made after the loop stops belongs to TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape). A note there records it.
