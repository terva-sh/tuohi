---
schema: 4
id: TKT-01M3J59M1H9PZ04J2C9JJZ7V13
title: Move desktop services out of the root package
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T21:13:07Z
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

- [ ] A program that imports only tuohi and opens one window links neither godbus nor atotto, shown with go list -deps
- [ ] Single instance, autostart, and clipboard live in their own subpackages, and notify and tray are no longer imported by the root
- [ ] Every feature the root offered before is still reachable, and the README says where each moved
