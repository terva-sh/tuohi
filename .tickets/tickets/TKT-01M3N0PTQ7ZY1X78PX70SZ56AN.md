---
schema: 4
id: TKT-01M3N0PTQ7ZY1X78PX70SZ56AN
title: Let App.Show create a window from any goroutine
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-linux
  - area/engine-darwin
  - area/engine-windows
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-6bca1629
  branch: fix/show-any-goroutine
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-6bca1629
  commit: a0bacde0bf38e23f503cbfefdc927b4b9ba0c0d0
  session: null
  claimed_at: 2026-10-01T05:47:20Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:03:31Z
updated_at: 2026-10-01T05:47:20Z
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

- [x] App.Show from a goroutine off the UI thread either creates the window on the UI thread or returns an error, on every engine
- [ ] A GUI scenario covers it on every engine

## Implementation plan

App.Show checks uiThreadErr first, as before. A live View is revealed through the existing queued onUIWith path, which does not wait. Otherwise, on the UI thread, `show` runs in place. Off it, the whole of `show` (window, events, bindings, first Navigate) goes through `ui.call` and Show waits. When no loop is running, ui.call returns errUILoopStopped, which Show wraps as "tuohi: Show off the UI thread: ...", and nothing is created. Before the UI thread is pinned, onUIThread reports true on Unix and Windows, so the first Show still runs in place and pins it.

Double creation: View gets a `showing` flag under v.mu. `show` sets it before creating and clears it after. A second Show that finds it set returns nil. That covers two goroutines racing before the UI thread is pinned, and a queued Show that WebView2's message pumping runs on the same thread during the first creation, where waiting would deadlock.

Rejected: returning an error for any Show off the UI thread, which the owner decided against on 2026-10-01. Also rejected: making a reveal off the UI thread wait, which turned a queued reveal into an error with no loop running, and which TestViewShowAgainReveals caught.

Test: showGoroutineScenario (show_goroutine_test.go), registered on all three engines. While a first view's loop runs, two goroutines Show the same second View. Both must return nil, exactly one window's page calls its binding, and Ready fires once. After the loop stops, a Show from a goroutine must refuse and create nothing.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-28T22:16:17Z

From review of PR #24: two concurrent first App.Show calls on the same View both see it unshown (View.live() is nil until setup finishes) and both create a window. Settle this together with Show from a goroutine.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: hand the creation to the UI thread and wait

The owner decided, 2026-10-01. `App.Show` off the UI thread runs the whole creation (`newView`, `installEvents`, `BindBatch`, the first `Navigate`) on the UI thread through the dispatcher and waits for it. This keeps the rule that every exported method is safe from any goroutine. The first `Show`, before any loop runs, stays on its caller, which becomes the UI thread.

The same change guards against two concurrent first `Show` calls on one View creating two windows (the PR #24 review note).

Returning an error lost because it breaks the any-goroutine rule and moves the marshalling onto every consumer.

It blocks v0.1.0.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:47:20Z

On Linux the scenario passes on both WebKitGTK stacks. With the hand-off disabled, the Linux suite aborts with SIGABRT, because GTK is called off its thread, so the scenario catches the bug. macOS and Windows are tested on GitHub after the merge, so criterion 1 is ticked on the Linux evidence and the shared code, and criterion 2 waits for that run.
