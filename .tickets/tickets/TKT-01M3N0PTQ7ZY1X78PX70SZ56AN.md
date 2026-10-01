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
  branch: fix/show-any-goroutine-2
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-6bca1629
  commit: 0480469db98d834d43b466350173e88e45c62995
  session: null
  claimed_at: 2026-10-01T16:29:46Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:03:31Z
updated_at: 2026-10-01T18:11:54Z
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

- [ ] App.Show from a goroutine off the UI thread either creates the window on the UI thread or returns an error, on every engine
- [ ] A GUI scenario covers it on every engine

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-28T22:16:17Z

From review of PR #24: two concurrent first App.Show calls on the same View both see it unshown (View.live() is nil until setup finishes) and both create a window. Settle this together with Show from a goroutine.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: hand the creation to the UI thread and wait

The owner decided, 2026-10-01. `App.Show` off the UI thread runs the whole creation (`newView`, `installEvents`, `BindBatch`, the first `Navigate`) on the UI thread through the dispatcher and waits for it. This keeps the rule that every exported method is safe from any goroutine. The first `Show`, before any loop runs, stays on its caller, which becomes the UI thread.

The same change guards against two concurrent first `Show` calls on one View creating two windows (the PR #24 review note).

Returning an error lost because it breaks the any-goroutine rule and moves the marshalling onto every consumer.

It blocks v0.1.0.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T06:17:55Z

### Landed in PR #61, failed on GitHub, reverted in PR #63

The revert of #61 also reverted this ticket's plan and notes, so this note restores them.

**Approach (#61, merge 4895e32).** App.Show off the UI thread ran the whole creation (newView, installEvents, applyBinds, first Navigate) on the UI thread through `ui.call` and waited for it. With no loop running, it returned "tuohi: Show off the UI thread: ...". Shows made before the UI thread is pinned took turns on `firstShowMu`, with a per-engine `uiThreadPinned()`. A per-View `showing` flag made a second Show of a View still being created return `errShowInProgress`. A reveal of a live View stayed queued. The scenario, showGoroutineScenario, has two goroutines Show one View while a first view's loop runs, then a Show with no loop running.

**Linux:** passed on both WebKitGTK stacks. With the hand-off disabled the suite aborts with SIGABRT, so the scenario catches the bug there.

**GitHub run 36822614924 on main, after the merge:**
- Windows: both concurrent Shows returned an error (`show=neither Show returned nil live=false hit=none ready=0`). The WEBVIEW2_DEBUG trace shows the second view's embed reaching `ready=true`, so creation got past WebView2's controller and failed later in showFirst. The scenario did not print the errors. closeFromGoScenario, which passed in run 36822261007, then hung ("Wait did not return after View.Close from a goroutine"), so the failed creation left the UI thread or the window count in a bad state.
- macOS: showGoroutineScenario passed, but the scenario after it, TestOutsideLinksNotRequested, got "no report": its Run returned before its goroutine reported. The macOS window count was already 2 after closeFromGoScenario in #60's passing run, so that leak predates #61.

**Revert (PR #63, merge 2e2a7c3).** It reverted the hand-off and the scenario, and kept the per-View `showing` guard, which now also re-checks for a live window under v.mu (reviews 1668 and 1669). The pre-pin race between first Shows of different Views is open again: serializing them without the hand-off would still create the second window on the wrong thread.

**Next.** The scenario must report each Show's error text, and log the window count before and after. The Windows failure is in showFirst after embed: installEvents, applyBinds and rebuildScripts pump GetMessageW, nested inside the WM_APP dispatch that runs the ui.call. Only GitHub's runners can run Windows and macOS, and only after a merge. So the next attempt either lands as a diagnostic on main, accepting a red run, or needs a way to run GitHub CI on a branch. That is the owner's choice: docs/pr-reviews.md keeps PR branches off the mirror.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T16:29:46Z

### Second attempt (branch fix/show-any-goroutine-2)

#61's code restored unchanged on top of main. Both #61 failures match bugs fixed since. Windows: both concurrent Shows failed after embed reached ready=true. A WM_QUIT left pending by an earlier View.Close's Terminate (fixed in #67) ends the nested GetMessageW pumps in installEvents, applyBinds and rebuildScripts, which would fail creation exactly there. The closeFromGo hang that followed was the close counted against the wrong scope (fixed in #66). macOS: the next scenario's Run returned before its goroutine reported, which is the queued stop: ending the next loop (fixed in #72).

Diagnostics added: the scenario prints each Show's error text when either fails, waits up to 20 s for its report after Run instead of reading without blocking, and macOS logs the window count before and after it. Linux passes on both WebKitGTK stacks (just test-gui). #63's guard is kept: #61's show() already rechecks view.w and view.showing under view.mu, so the duplicate check #63 added in Show is dropped.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T17:08:07Z

Owner's decision, 2026-10-01: merge #73 once Forgejo is green and accept that main may be red for one GitHub run. If macOS or Windows fails, revert at once and fix forward. Pushing the PR branch to the GitHub mirror was offered and declined.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T18:11:54Z

Merged main (#74: WebKitGTK 2.54 main-thread lock and GTK3 clipboard hold) into #73. Forgejo's GUI failures on 9b74353 and a50e610 were those two Debian 13 regressions, not this change. They also showed that a first Show off the main thread aborts with 2.54, so by the owner's decision #73 now makes Unix refuse it with ErrNotMainThread (see TKT-01M3W9PT). The full Forgejo GUI job passes twice in CI's image.
