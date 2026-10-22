---
schema: 4
id: TKT-01M5JYX8KTD3A2ARREB37A4RP7
title: Exercise window lifecycle through the public application loop
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/lifecycle-app-wait-validation
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 07906df27de9fef631f6dc0cbe3f799855faf9c1
  session: null
  claimed_at: 2026-10-22T23:25:11Z
  expires_at: null
archive: null
created_at: 2026-10-22T23:25:00Z
updated_at: 2026-10-22T23:25:11Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

GitHub native Windows run 39870660591 at 4be7f93 passed instance/notify/tray/updater and examples, then the root WebView2 process hit the 210-second outer limit. Its last trace was the new multiwindow lifecycle scenario (Retained view, ordinary close, retained view destruction). The exact blocked operation is not established: TestMain runs scenarios before m.Run, so the normal Go test timeout had not started and no stacks were captured. The existing watchdog closes only the retained view while other views remain.

Exercise the selected lifecycle contract through the supported public application loop: App.Wait with App.Exit=false, explicit App.Quit, retained view cancellation/hide/show, native close/activation/state events, deferred cancel/allow, request coalescing, and quit invalidation of another deferred close. Preserve every behavioral assertion. Start worker operations after a queued UI turn; use an application-wide timeout and stage diagnostics with a hard stack-producing timeout so a native stall fails within the scenario rather than only at an outer process kill. Do not invent a production fix from the incomplete native trace or weaken assertions.

This is necessary native-validation follow-through under the owner-authorized merge task. Native macOS/Windows results follow the normal internal PR and main mirror process; local compilation alone is not execution. Original evidence: https://github.com/terva-sh/tuohi/actions/runs/39870660591/job/120143961424 .

## Acceptance criteria

- [ ] The native lifecycle scenario uses App.Wait and retains the existing close, renderer, state, callback-thread, deferred-decision, coalescing and quit assertions.
- [ ] Worker operations start during an active UI loop and timeout failures identify their stage without waiting for the outer CI kill.
- [ ] Cgo-free CI/cross builds and both Linux stacks pass; native macOS/Windows outcomes are recorded accurately.

## Implementation plan

Replace the internal per-view Run plus second App.Wait phase with one public App.Wait. Queue a UI-turn gate before off-thread operations; continue after deferred Allow only when native terminal state arrives, request a deferred close on the peer under the same loop, then call App.Quit and check invalidation after Wait returns. Failure/watchdog paths quit the whole app and retain stage diagnostics; a later hard timeout yields a Go panic/stack even before m.Run. Keep exact renderer/window identity, callbacks, activation/visibility/terminal state and request-count assertions. Validate full cgo-free checks and both Linux GUI stacks, compile Windows/Darwin tests and request fresh exact-head/base review. No production change or inferred cause of the original native stall.
