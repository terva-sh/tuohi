---
schema: 4
id: TKT-01M3N0PTR5E1NP8TT9XV07GBTK
title: Check App.Notify, Open, Reveal and Quit off the UI thread
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-darwin
  - area/engine-windows
  - area/notify
assignees: []
milestone: null
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T22:03:31Z
updated_at: 2026-09-28T22:03:31Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Several `App` methods make native calls on the caller's goroutine on macOS and Windows:

- **`App.Notify`:** macOS uses NSUserNotificationCenter on the caller, which is documented as safe from any goroutine. Windows uses `Shell_NotifyIconW`, and its hidden HWND is created on whichever thread first calls it (notify_windows.go).
- **`App.Open` and `App.Reveal`:** macOS uses NSWorkspace, and Windows uses ShellExecuteW, both on the caller. Linux runs `xdg-open` and is unaffected.
- **`App.Quit` on Windows:** `uiThreadID` is captured lazily by whichever thread calls it first. A `Quit` from a goroutine before `Wait` records the wrong thread for good. `uiThreadApp`, which `newView` now records, may replace it.

### Found by

The inventory for TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine).

### Things to settle

- Which of these calls actually requires the UI thread. ShellExecuteW and NSWorkspace are documented as callable from other threads, so the fix may be documentation for some of them rather than marshalling.

## Acceptance criteria

- [ ] Each App method is either marshalled or documented as safe off the UI thread, with the platform documentation cited
- [ ] App.Quit before App.Wait on Windows wakes the right thread
