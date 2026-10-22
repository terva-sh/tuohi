---
schema: 4
id: TKT-01M5JKX3C39YNR5SM64A8EK6YR
title: Add close interception and native window lifecycle events
type: task
status: done
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
  - ref: https://git.local.sothr.com/terva-sh/tuohi/pulls/84
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T20:12:40Z
updated_at: 2026-10-22T22:44:15Z
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

- [x] User and keyboard close can be allowed, cancelled or deferred while preserving the same native view/renderer when hidden.
- [x] Native focus/activation and visibility changes reach supported public callbacks with defined thread and ordering semantics.
- [x] Programmatic teardown and App.Quit do not get trapped by interception; defaults and existing consumers retain their behavior.
- [x] Meaningful state-machine and Linux GUI tests plus a public-API example cover lifecycle behavior; CGO_ENABLED=0 and prescribed cross builds pass, with native evidence distinguished from compilation.

## Implementation plan

### Approach

Add View.CloseRequested func(*CloseRequest), with default allow, explicit Cancel/Allow, and Defer for asynchronous decisions. A per-engine state machine coalesces repeated native requests, admits only the first decision and invalidates pending work on teardown/quit. Deferred Allow marshals to the UI and targets the captured engine lifetime. View.RequestClose serves custom chrome through the same policy; View.Close remains unconditional. GTK delete-event/close-request, NSWindow windowShouldClose and Windows WM_CLOSE feed this state machine. App quit invalidates pending requests and bypasses interception without changing its existing process/run-loop policy.

Add View.WindowChanged func(WindowState) and View.WindowState() for native mapped visibility and top-level activation. Deliver snapshots on the UI thread after engine publication, suppress duplicates, and finish with Closed on destruction. GTK map/unmap/is-active, NSWindow key/minimize/order notifications, and Windows WM_ACTIVATE/WM_SHOWWINDOW/WM_SIZE feed native sampling. Visibility means OS shown/mapped state, not proof of pixels visible through occlusion or another workspace. Embedded internal host windows have no close interception guarantee.

Test the shared decision state machine (defer/cancel/allow/repeated requests, teardown/quit, stale queued allows and reentrancy), add native Linux GUI scenarios preserving JS state and the native handle across hide/show, and update the public examples and API documentation in the same PR. Run CGO_ENABLED=0 just ci and both Linux GUI stacks; distinguish compilation from unavailable native macOS/Windows/BSD runtime validation. Publish a scoped internal PR and request exact-head/base review; do not merge or release.

### Alternatives

A bool-only close callback cannot defer a consumer decision. Blocking the UI for a dialog or worker would stop renderer/native dispatch. Recreating a destroyed view loses renderer state. A package-global callback would mix windows and lifetimes. Keep the lifecycle machine separate from the bridge mutex so consumer callbacks never run under internal locks.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T21:08:23Z

Implemented View.CloseRequested, CloseRequest.Allow/Cancel/Defer/Done, View.RequestClose, View.WindowChanged and View.WindowState. Callbacks are captured per engine, run on the UI thread without internal locks, and can nest after native state changes. The initial snapshot follows engine publication; teardown emits a terminal Closed snapshot. A separate lifecycle mutex serializes decisions, coalesces repeated requests and invalidates stale queued allows on teardown/quit. Deferred native teardown uses the process UI dispatcher plus the captured View/engine identity, avoiding worker reads of mutable native handles. App.Quit invalidates pending requests without changing its existing Wait/process policy. View.Close remains unconditional.

GTK uses delete-event/close-request, map/unmap, is-active, GTK3 window-state-event and GTK4 GdkSurface state notifications. Minimized masks are deliberately toolkit-specific. AppKit uses windowShouldClose, key/minimization/occlusion-change notifications and explicit show/hide sampling. Win32 uses WM_CLOSE, top-level WM_ACTIVATE, WM_SHOWWINDOW and WM_SIZE; it samples IsWindowVisible/IsIconic/GetForegroundWindow. Terminal callbacks are published after native handles are cleared where teardown could be reentered. Existing default close behavior and callback fields remain unchanged when the new fields are nil.

Shared regressions cover implicit/explicit allow, cancel, defer, competing decisions, coalescing, Done invalidation, scope quit, stale queued teardown and callback reentrancy; 20 focused repetitions passed cgo-free. The native scenario covers native synchronous allow, cancel/hide/show retaining HWND/NSWindow/GtkWindow and JavaScript state, native focus changes, deferred cancel/allow and App.Quit invalidation. View.Close's existing native scenario now sets a cancelling handler and verifies it is bypassed. Native Linux validation is running on both WebKitGTK stacks for the final expanded scenario; previous retained-state/focus runs passed. CGO_ENABLED=0 just ci passed including all prescribed target builds; macOS/Windows/BSD execution remains unavailable and is not claimed. Public API documentation and examples/lifecycle are included in this PR.

A separate pre-existing GTK3 maximize-mask issue was filed as draft TKT-01M5JPZ2XZDPQDQYBRYW5R902F (Use the GTK3 maximized bit when toggling window state); no unrelated fix was folded into this work.

**agent:codex/t3-b903096c** at 2026-10-22T21:10:40Z

Final local validation passed: CGO_ENABLED=0 just ci, including vet on Linux/Darwin/Windows, formatting, import and embedded-JS checks, headless tests in both modules, all eight target cross builds and ticket checks. CGO_ENABLED=0 just test-gui passed on native Linux WebKitGTK 6.0 and 4.1, including synchronous native allow/terminal state, cancellation/hide/show preserving the native handle and JavaScript state, native activation/deactivation, deferred cancellation/allow, unconditional View.Close and App.Quit invalidation while Wait pumps. Xvfb has no window manager; the focus test uses real X FocusIn/FocusOut events. This does not establish compositor-managed minimization/workspace behavior. Native macOS/Windows/BSD execution is unavailable. No runtime claims are made for those platforms.

Final API adjustments: RequestClose is harmless before Show/after Close and binds queued work to the captured engine, with a regression for a queued request racing teardown and a replacement view. Shared lifecycle tests passed 20 repetitions. Darwin Active requires both application activation and a key window. The native quit scenario waits for an actual pumping loop so AppKit can marshal and Windows consumes WM_QUIT before the next native scenario. The example retains renderer-owned textarea state, shows cancel/hide/show and deferred allow, and keeps its only window reachable by automatically reopening it. No power-management behavior was added. Ready for internal PR and full exact-head/base review; no merge/release authorization inferred.

**agent:codex/t3-b903096c** at 2026-10-22T21:12:08Z

Internal PR #84 is ready: https://git.local.sothr.com/terva-sh/tuohi/pulls/84. Tested implementation head ed3d734523ead9e11199512e64dee60513877a4d against exact base abc50ac336d37fa3e0aeb3751a529e0fe6a2f48e (PR #83). Full review Actions #572 / run 6af1d51b-40ef-4ecb-8331-248023de24f8, request window-lifecycle-ready-ed3d734, is clean: comment 33615. There are no findings requiring dispositions. The review accurately identifies native macOS/Windows runtime validation and notification ordering/reentrancy there as limits; this Linux session supplies compilation and Linux runtime evidence only. Local cgo-free just ci, all prescribed cross builds, both Linux GUI stacks and 20 shared lifecycle regression repetitions passed. Forgejo CI #571 validates the same implementation head. Kuiske received all five API decisions, tickets, tested source and PR links. No public version has been published.

The following ticket-only commit records this result and PR reference. Request review carry for that head; implementation code remains the reviewed ed3d734 source. Ticket remains in-progress awaiting owner merge. This session does not have merge/release authorization.

## Summary

Implemented with public API docs/examples, meaningful shared and native Linux regressions, cgo-free cross builds and clean reviewed source. PR #84 was owner-merged into its stack parent; final integration to main is tracked by TKT-01M5JWFSGV9TKY437YPS9G9021 (Land reviewed desktop and updater stacks on main). Native macOS/Windows validation remains a separate hosted/runtime requirement; managed notifications on Windows explicitly return ErrUnsupported.
