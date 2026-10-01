---
schema: 4
id: TKT-01M3N0PTS10TG629DVNEFSNVSZ
title: Check that a Close from Go reaches App.Wait on macOS
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T22:03:32Z
updated_at: 2026-10-01T05:14:22Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

On macOS, a window closed by `View.Close` may never be reported to the App scope. `destroyOnUI` in lib_darwin.go clears the window delegate before sending `close`, and `appWindowClosed` is called only from `onWindowWillClose`. If that is right, `App.Wait` with `Exit` set never returns after the last window is closed from Go.

`destroyOnUI` also calls `w.onWindowDestroyed(true)` after `close`, which may already report it. Nobody has checked which path runs.

### Found by

Found by reading, during the inventory for TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine). It has not been reproduced.

### Things to settle

- A macOS GUI scenario: `App.Exit` true, one view, `View.Close` from Go, and `App.Wait` must return.

## Acceptance criteria

- [ ] Whether a Destroy-driven close reaches appWindowClosed on macOS is established by a scenario
- [ ] App.Wait with Exit set returns after the last window is closed from Go

## Notes

**agent:claude-code/fe5548cb** at 2026-09-29T21:06:01Z

### Reproduced: a Close from Go never ends App.Wait

git-ticket-canvas checked this from its desktop window, and from a standalone probe. With `App{Exit: true}` and one framed `View`, a `View.Close` called from a goroutine after `Ready` takes the window off the screen and returns at once. `App.Wait` does not return. The probe's watchdog calls `App.Quit` at 8 s, and only then does `Wait` return, with nil.

Reproduces at `v0.1.0-alpha.1`, at `865f6be`, at `e705b5c` on fix/view-threading, and at `5a23702` on main after PR #24. The threading change does not affect it.

Closing the window with its close button does end `Wait`. That path goes through `onWindowWillClose`, which calls `appWindowClosed()`.

### Where it goes wrong

`destroyOnUI` (lib_darwin.go) clears the window's delegate before sending `close`, so `onWindowWillClose` never runs, and that is the only caller of `appWindowClosed()`. The `onWindowDestroyed(true)` that follows does not make up for it, because it skips the Terminate that the last window closing would otherwise trigger.

### How the consumer works around it

git-ticket-canvas's macOS GUI scenarios close with `App.Quit` instead of `View.Close` (cmd/git-ticket-canvas-desktop/window_gui_darwin_test.go). Its Linux scenarios keep `View.Close`, which works there.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: it blocks v0.1.0

The owner decided, 2026-10-01, that this reproduced bug blocks the first release. A `View.Close` from Go that never ends `App.Wait{Exit: true}` on macOS is a hang that git-ticket-canvas already works around. The cause in the note above (`destroyOnUI` clears the delegate before `close`, so `appWindowClosed` never runs) is the place to start.
