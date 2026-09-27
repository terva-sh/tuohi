---
schema: 4
id: TKT-01M3HWWRRXTAR4T01SK79Z4BSM
title: Linux GUI scenarios crash in view teardown on both WebKitGTK stacks
type: bug
status: draft
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
claim: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T18:11:16Z
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

- [ ] The cause of the WebKitGTK 4.1 SIGSEGV in Destroy is found and fixed
- [ ] The GTK4 abort is either fixed or shown to be the test display's missing GL, with the harness adjusted
- [ ] just test-gui passes on both stacks with CGO_ENABLED=0

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
