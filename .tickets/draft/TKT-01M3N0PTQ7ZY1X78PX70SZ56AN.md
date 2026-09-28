---
schema: 4
id: TKT-01M3N0PTQ7ZY1X78PX70SZ56AN
title: Let App.Show create a window from any goroutine
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-linux
  - area/engine-darwin
  - area/engine-windows
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
updated_at: 2026-09-28T22:16:17Z
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

`App.Show` creates a view's window, web view, bindings and first navigation on the caller's goroutine. The first call pins that goroutine's OS thread as the UI thread. A later call from any other goroutine then builds a window off the UI thread:

- **Unix:** all of `newView`, `installEvents`, `BindBatch` and `Navigate` run on the caller.
- **Windows:** the HWND and WebView2 controller are created on the caller's thread, which then owns them. `BindBatch` and `rebuildScripts` pump `GetMessageW` on the caller's thread, which by reading never sees the script completion off the UI thread and would block.
- **macOS:** marshals only when a loop is already running. Off main with no loop, everything runs inline and `uiIsMain` latches false.

The reveal path (a second `Show` on a live view) now marshals through the dispatcher, as of TKT-01M3J1H8CPMZX9EJX8R2CQRA6P.

### Found by

The inventory for TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine), which covered the View surface and left App to its own tickets.

### Things to settle

- Whether `App.Show` off the UI thread marshals the whole creation through `ui.call` and waits, or returns an error. Waiting fits "every exported method is safe from any goroutine". The first `Show`, before any loop runs, has no UI thread to marshal to, and must stay where it is.

## Acceptance criteria

- [ ] App.Show from a goroutine off the UI thread either creates the window on the UI thread or returns an error, on every engine
- [ ] A GUI scenario covers it on every engine

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-28T22:16:17Z

From review of PR #24: two concurrent first App.Show calls on the same View both see it unshown (View.live() is nil until setup finishes) and both create a window. Settle this together with Show from a goroutine.
