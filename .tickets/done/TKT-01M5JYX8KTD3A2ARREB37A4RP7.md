---
schema: 4
id: TKT-01M5JYX8KTD3A2ARREB37A4RP7
title: Exercise window lifecycle through the public application loop
type: bug
status: done
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
claim: null
archive: null
created_at: 2026-10-22T23:25:00Z
updated_at: 2026-10-22T23:40:58Z
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

- [x] The native lifecycle scenario uses App.Wait and retains the existing close, renderer, state, callback-thread, deferred-decision, coalescing and quit assertions.
- [x] Worker operations start during an active UI loop and timeout failures identify their stage without waiting for the outer CI kill.
- [x] Cgo-free CI/cross builds and both Linux stacks pass; native macOS/Windows outcomes are recorded accurately.

## Implementation plan

Replace the internal per-view Run plus second App.Wait phase with one public App.Wait. Queue a UI-turn gate before off-thread operations; continue after deferred Allow only when native terminal state arrives, request a deferred close on the peer under the same loop, then call App.Quit and check invalidation after Wait returns. Failure/watchdog paths quit the whole app and retain stage diagnostics; a later hard timeout yields a Go panic/stack even before m.Run. Keep exact renderer/window identity, callbacks, activation/visibility/terminal state and request-count assertions. Validate full cgo-free checks and both Linux GUI stacks, compile Windows/Darwin tests and request fresh exact-head/base review. No production change or inferred cause of the original native stall.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T23:28:36Z

Replaced the test-only per-view Run/second-Wait sequence with one public App.Wait and off-thread App.Quit. A queued View.Window turn admits the worker only after the application loop runs. All original behavior checks remain: allowed native close, native activation/deactivation, callback UI-thread assertions, cancellation/hide, same window/renderer state after Show, visibility/terminal snapshots, deferred cancel/allow, coalescing, and App.Quit invalidating the peer request. Terminal state is now explicitly awaited before proceeding while the peer keeps the application alive. Failures quit the whole application; a 30-second soft watchdog and 35-second hard panic with stage and all goroutine stacks bound native stalls even before m.Run, including deferred teardown.

CGO_ENABLED=0 just ci passes both modules/all eight prescribed targets. Full just test-gui passes library/packages/examples on WebKitGTK 6.0 and 4.1. Updated Windows amd64 and Darwin arm64 root test binaries compile; pinned Windows lint passes. A scratch-only intentional renderer-state mutation produces TestNativeWindowLifecycle failure: restoring retained renderer: renderer state lost: lost fixture. The process exits normally with failure in 30.5 seconds including all preceding scenarios, rather than reaching the outer CI kill. The fault was never applied to repository source.

Original native WebView2 155.0.4283.62 timeout at 4be7f93 did not establish the exact blocked call or a production cause, and no production change is claimed. The public-loop harness is validated on Linux; new native macOS/Windows outcomes remain pending after internal review/merge and mirror sync. PR #91 already merged ticket closure for the independently confirmed Windows pipe fix; that native subpackage result remains valid despite the later WebView2 timeout.

**agent:codex/t3-b903096c** at 2026-10-22T23:31:07Z

Review 2749 at exact head 553f9bff107d0d61c4a56e9da75c02e8bd53aa70 / base 07906df27de9fef631f6dc0cbe3f799855faf9c1 has one medium finding: the result channel is sent before deferred App.Quit completes, so Wait/result alone can falsely pass while Quit is still running. Accepted. Close workerDone only after Quit returns, and await it with a five-second bound immediately after App.Wait on both normal and error paths. Keep the hard watchdog armed through that join and deferred cleanup. Validate a scratch-only delayed completion fault fails strictly instead of passing, then rerun full cgo-free CI/all targets, both Linux GUI stacks and updated foreign test compilation/lint. Preserve all lifecycle assertions and make no production change. Request fresh full exact-head/base review.

**agent:codex/t3-b903096c** at 2026-10-22T23:33:00Z

Review 2749 finding-1 accepted and addressed: workerDone closes only after the deferred App.Quit returns. Immediately after App.Wait, every path awaits that completion for up to five seconds before proceeding or returning a Wait error. The hard watchdog remains armed during the join and deferred native cleanup. A result sent before Quit finishes can no longer prove success.

Controlled scratch-only regression: delay worker completion ten seconds after Quit (which has already released App.Wait). With the join, TestNativeWindowLifecycle strictly fails after five seconds with App.Quit waited for deferred close: quitting app with deferred peer close (full process 34.814 seconds including preceding scenarios). Removing just the join from the same fault fixture makes the test pass, demonstrating the previous false-positive window. Neither fault nor removed join was applied to repository source.

Updated CGO_ENABLED=0 just ci passes both modules/all eight prescribed targets; full just test-gui passes both Linux stacks and examples. Windows amd64 and Darwin arm64 root test binaries compile; pinned Windows lint passes. Native macOS/Windows runtime confirmation of this public-loop scenario remains pending after reviewed merge. Request a fresh full exact-head/base review after this synchronization fix. Original Windows 210-second timeout still has no established production cause.

**agent:codex/t3-b903096c** at 2026-10-22T23:40:58Z

PR #92 merged as d7f113cc07a5e53a1a0c5c07f358dfe6d7e87bef after all three internal checks and full exact-head/base clean review c8ba23a4-1b9e-415d-a3eb-8a3c24c26e61 (head af924cca7c1008403c3d7d44f631b0d09d6be615, base 07906df27de9fef631f6dc0cbe3f799855faf9c1). Review 2749 finding-1 is individually disposed as fixed by af924cc: the caller joins workerDone after App.Wait, and workerDone closes only after App.Quit returns.

Native mirror run https://github.com/terva-sh/tuohi/actions/runs/39872943146 at d7f113c now completed both required desktop jobs successfully, with CGO_ENABLED=0 and TUOHI_REQUIRE_GUI=1. Windows job https://github.com/terva-sh/tuohi/actions/runs/39872943146/job/120151980729 passes TestNativeWindowLifecycle, all root WebView2 scenarios, helper packages and consumer examples. macOS job https://github.com/terva-sh/tuohi/actions/runs/39872943146/job/120151980911 also passes TestNativeWindowLifecycle, root native scenarios, helper packages and consumer examples. The scenario executes in TestMain and its result is asserted by the named test; the displayed 0.00-second assertion duration is not the native scenario duration. GTK3 x64 and both ARM GTK jobs also passed; x64 GTK4 was still installing dependencies when these native job logs were captured. Local both-stack GUI tests and all prescribed cross builds already passed.

This completes this ticket's public-loop validation and native follow-through. The original Windows timeout's production cause remains unknown; no production source changed and no inferred root cause is claimed. Packaged notification delivery/permissions, production team-signed bundle updates/notarization, and manual tray usability remain separate acceptance work. No release/version was published.

## Summary

Landed through PR #92 at d7f113cc07a5e53a1a0c5c07f358dfe6d7e87bef. The native lifecycle scenario uses one public App.Wait/App.Quit loop, preserves close/renderer/state/thread/deferred/quit assertions, bounds failures with staged watchdogs and joins Quit completion. Review 2749 finding-1 fixed in af924cc; fresh full exact-head/base review clean. Full cgo-free CI/cross builds and both local Linux GUI stacks pass. Native GitHub run 39872943146 at d7f113c passes Windows and macOS desktop jobs including TestNativeWindowLifecycle, helper packages and consumer examples. Controlled renderer-state and delayed-Quit faults fail as intended. Original Windows timeout cause remains unknown; no production fix is claimed. Packaged-app release acceptance is separate, and no release was published.
