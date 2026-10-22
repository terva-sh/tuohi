---
schema: 4
id: TKT-01M5JKX3C39YNR5SM64A8EK6YR
title: Add close interception and native window lifecycle events
type: task
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
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:kuiske/TKT-01M44GTHZY4ZAASWMSBG1VEFHJ
    path: null
  - ref: t3-thread://v1/564d5d21-1210-473a-aa2b-918183ce9764
    path: null
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/prepare-library-changes
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: cf945c1c5152b33b4bf8f76b7cf91c4e1015d1be
  session: null
  claimed_at: 2026-10-22T20:12:40Z
  expires_at: null
archive: null
created_at: 2026-10-22T20:12:40Z
updated_at: 2026-10-22T20:59:37Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M44GTHZY4ZAASWMSBG1VEFHJ (Show a tray icon and optionally keep running when closed) needs to intercept a user close, hide the existing view and preserve renderer/UI state, with allow/cancel/defer decisions. Add native focus/activation and visibility events so the desktop adapter can suppress notifications for an active conversation. Specify user/keyboard close, programmatic teardown, app quit and callback threading. Preserve default close and shutdown behavior; unrelated power management is outside scope.

## Acceptance criteria

- [ ] User and keyboard close can be allowed, cancelled or deferred while preserving the same native view/renderer when hidden.
- [ ] Native focus/activation and visibility changes reach supported public callbacks with defined thread and ordering semantics.
- [ ] Programmatic teardown and App.Quit do not get trapped by interception; defaults and existing consumers retain their behavior.
- [ ] Meaningful state-machine and Linux GUI tests plus a public-API example cover lifecycle behavior; CGO_ENABLED=0 and prescribed cross builds pass, with native evidence distinguished from compilation.

## Implementation plan

### Approach

Add View.CloseRequested func(*CloseRequest), with default allow, explicit Cancel/Allow, and Defer for asynchronous decisions. A per-engine state machine coalesces repeated native requests, admits only the first decision and invalidates pending work on teardown/quit. Deferred Allow marshals to the UI and targets the captured engine lifetime. View.RequestClose serves custom chrome through the same policy; View.Close remains unconditional. GTK delete-event/close-request, NSWindow windowShouldClose and Windows WM_CLOSE feed this state machine. App quit invalidates pending requests and bypasses interception without changing its existing process/run-loop policy.

Add View.WindowChanged func(WindowState) and View.WindowState() for native mapped visibility and top-level activation. Deliver snapshots on the UI thread after engine publication, suppress duplicates, and finish with Closed on destruction. GTK map/unmap/is-active, NSWindow key/minimize/order notifications, and Windows WM_ACTIVATE/WM_SHOWWINDOW/WM_SIZE feed native sampling. Visibility means OS shown/mapped state, not proof of pixels visible through occlusion or another workspace. Embedded internal host windows have no close interception guarantee.

Test the shared decision state machine (defer/cancel/allow/repeated requests, teardown/quit, stale queued allows and reentrancy), add native Linux GUI scenarios preserving JS state and the native handle across hide/show, and update the public examples and API documentation in the same PR. Run CGO_ENABLED=0 just ci and both Linux GUI stacks; distinguish compilation from unavailable native macOS/Windows/BSD runtime validation. Publish a scoped internal PR and request exact-head/base review; do not merge or release.

### Alternatives

A bool-only close callback cannot defer a consumer decision. Blocking the UI for a dialog or worker would stop renderer/native dispatch. Recreating a destroyed view loses renderer state. A package-global callback would mix windows and lifetimes. Keep the lifecycle machine separate from the bridge mutex so consumer callbacks never run under internal locks.
