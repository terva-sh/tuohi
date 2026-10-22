---
schema: 4
id: TKT-01M5JKX2K8C4CAKPTP1VZ28D6G
title: Add interactive notification handles and permission management
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/notify
  - area/api
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:kuiske/TKT-01M43TRAJW4ZWVXPK79YXX9R0Q
    path: null
  - ref: t3-thread://v1/564d5d21-1210-473a-aa2b-918183ce9764
    path: null
  - ref: https://git.local.sothr.com/terva-sh/tuohi/pulls/82
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T20:12:39Z
updated_at: 2026-10-22T22:51:54Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M43TRAJW4ZWVXPK79YXX9R0Q (Show content-free desktop notifications) needs notification activation to open the right account and room and dismissal when a room is read. Add a public managed notification abstraction with opaque application correlation IDs, activation, replacement, dismissal, capabilities, permissions and cancellation/shutdown. Investigate and implement modern macOS UserNotifications with checks before touching a center in an unbundled process. Preserve Show/ShowOpts defaults. Keep message content and credentials out of routing payloads and logs; composition/privacy policy stays in the consumer. Inline replies are outside scope.

## Acceptance criteria

- [x] A managed notification returns a correlation handle and supports activation, replacement and dismissal where available, with explicit capability/errors elsewhere.
- [x] Permission status/request and shutdown/cancellation are explicit; modern macOS notification access checks app bundle identity before native calls.
- [x] Routing carries only opaque IDs, with lifecycle/concurrency and privacy behavior documented and exercised by meaningful tests and a public-API example.
- [x] CGO_ENABLED=0 checks and all prescribed cross builds pass; native runtime evidence and unavailable platform validation are recorded separately.

## Implementation plan

Preserve managed APIs and defaults. Pin notificationPool using runtime.LockOSThread before creating the pool, defer unlock before deferred drain so drain runs first. Add Darwin notification-path thread-affinity regression using NSThread currentThread across repeated Gosched calls. Run cgo-free tests/cross checks, explicitly compile Darwin tests, post disposition and request a fresh full #89 review.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T20:29:04Z

Implemented the opt-in notify.Client/Handle API with serial worker activation, Go-only application correlation, replacement, dismissal, completion/errors, explicit permissions and bounded best-effort shutdown. Lifetime cancellation also interrupts an in-flight permission wait before acquiring the operation lock; queued activation is coalesced and suppressed by dismissal/close. Existing Show/ShowOpts/Alert/Beep behavior is preserved. Linux uses a dedicated bus connection bound to the unique daemon owner and fails existing handles on service loss, avoiding reused-ID aliases. macOS uses modern UserNotifications after APPL bundle/identity/executable checks, retains its delegate, refuses another delegate, and uses Objective-C blocks without cgo. Managed macOS attachments and Windows balloons report ErrUnsupported; the limitations are documented rather than silently falling back.

Validation: final CGO_ENABLED=0 just ci passed in both modules, including all eight prescribed target cross builds. just test-gui passed on both native Linux WebKitGTK stacks, including showcase/loopback. Shared client tests cover identity, duplicate IDs, errors, replacement, dismissal, queued activation, callback-initiated close, cancellation interrupting permission waits, cleanup and service loss. Private D-Bus integration tests exercised actual GetCapabilities/Notify/actions/replacement/CloseNotification wire behavior, spoofed/unknown IDs, daemon ownership changes and redaction of service error bodies. These are protocol tests, not proof a real notification banner appeared. The public managednotify example demonstrates desktop-adapter activation/focus, replacement and room-read dismissal. A native macOS test is provided for unbundled safety, but this Linux host cannot execute it or validate bundled permission/delivery behavior; macOS/Windows/BSD compilation is not native runtime evidence. No version was published.

Primary API reference consulted: Apple's UNUserNotificationCenter documentation says the shared center is safe from any application thread and requires the delegate to be installed before interacting with it: https://developer.apple.com/documentation/usernotifications/unusernotificationcenter.

**agent:codex/t3-b903096c** at 2026-10-22T20:33:48Z

PR #82 review 2722 (run #549) reviewed exact head 48a07f9ee66cc0c3c3daaffbf3ecf98391c45c2e against base 4f8d5e638ed4ff802ff8e9af9ee6e9673c09c099 and contains two valid medium findings. Finding 1: the state mutex remains held during native operations, so a queued caller cannot observe its own cancellation. Replace admission with a context-aware gate and keep state locking short, buffering native events only while publishing a mutation's resulting correlation. Regress deadline expiration for Permission, RequestPermission, Post, Replace and Dismiss behind an independently blocked permission request, plus early native event correlation. Finding 2: GetNameOwner does not activate an idle installed notification daemon. StartServiceByName only when the name has no owner, then resolve/bind the unique owner while retaining owner-change and stale-ID protections. Add a genuinely activatable service on a private bus, initially without an owner. Rerun required cgo-free checks, post individual dispositions and request a fresh exact-head/base review. Ownership remains this session alone; Kuiske inspected read-only.

**agent:codex/t3-b903096c** at 2026-10-22T20:37:09Z

Disposition for review 2722, finding 1 (cancellation): fixed. All native operations now acquire a context-aware channel gate before briefly taking the state mutex; native permission/post/replace/dismiss waits no longer hold that mutex. Shutdown cancels the active operation before acquiring the gate and retains serialized cleanup. Native events arriving during a mutation are buffered until its resulting native ID is published, and queued callbacks resume afterwards, preserving early-event correlation and successful-dismiss suppression. TestManagedQueuedOperationsRespectDeadline checks Permission, RequestPermission, Post, Replace and Dismiss return DeadlineExceeded behind an independently blocked permission request, do not cancel that request, do not reach the backend and do not leak admission. TestManagedNativeEventsBeforePostReturns regresses activation/close arriving before a post reply.

Disposition for review 2722, finding 2 (D-Bus activation): fixed. A NameHasNoOwner result now triggers context-bound StartServiceByName, followed by unique-owner resolution. Operations/signals remain bound to that unique owner, never the activatable name. The resolution reply's sequence distinguishes earlier startup transitions from later owner loss; existing owner-loss/reused-ID/spoofed-signal protections remain. TestManagedDBusActivatesIdleService installs a private-bus .service entry launching the test executable, first verifies NameHasNoOwner, then proves activation, capability discovery, post, replacement and dismissal. This exercises actual D-Bus activation rather than pre-owning the name.

Validation: CGO_ENABLED=0 notify suite passed 20 consecutive runs. Full just ci passed in the library and examples, including all eight prescribed cross targets and ticket validation. just test-gui passed on both native Linux WebKitGTK stacks, including showcase/loopback and the private-bus regressions. These remain protocol/runtime Linux evidence only; native macOS/Windows/BSD validation is unavailable and compilation does not establish it. Public cancellation and startup documentation updated. A fresh exact-head/base model review will be requested after push; no merge or release is authorized.

**agent:codex/t3-b903096c** at 2026-10-22T20:42:05Z

Fresh review 2724 (Actions #554, run e64a5bd0-d477-44ea-a2ac-509c8fe94db0) reviewed 490233c1747c41f40cb384d645d4cef643b3a5ce against 4f8d5e638ed4ff802ff8e9af9ee6e9673c09c099. It explicitly resolved both review 2722 findings and found a valid new medium issue: late cancellation cleanup for macOS replacement A can remove a later successful replacement B because both use the same native request ID. Forgejo CI #551 passed for 490233c.

Fix: keep native replacement IDs, but associate asynchronous submissions with per-ID generations. Cleanup checks its generation and invokes native removal under the same lock used to admit the next generation, preventing a check/remove race. Reference counts retain generation history until both each waiter and native completion finish; idle IDs are then removed, so long-lived clients do not retain every historical notification ID. The actual shared asynchronous wait/cleanup path is exercised on Linux using a controlled native-center adapter: cancel A, return its waiter, successfully complete B under the same ID, then deliver A's late completion. A second regression proves the latest cancelled request is still cleaned up if accepted after its waiter returns. This tests ordering logic, not native macOS delivery. New identifiers for every replacement were rejected because they would abandon native replacement and could create duplicate banners. Full validation and another exact-head/base review follow.

**agent:codex/t3-b903096c** at 2026-10-22T20:43:37Z

Disposition for review 2724, finding 1 (late cancellation cleanup): fixed with generation-guarded asynchronous submission cleanup. The controlled delayed-completion regression and latest-cancelled cleanup regression pass; the full cgo-free notify suite passed 20 consecutive runs. Full CGO_ENABLED=0 just ci passed in both modules with all eight prescribed cross builds and strict ticket checks; just test-gui passed on both native Linux WebKitGTK stacks. Native macOS delivery/completion behavior remains unexecuted here; the regression runs the actual shared asynchronous control path used by macBackend.post, not a native center. Requesting another exact-head/base review after push; no merge or release performed.

**agent:codex/t3-b903096c** at 2026-10-22T20:46:45Z

Review 2725 (Actions #557, run 6792e8a4-c997-412a-aa3d-f127dc20fbc4) reviewed cbdb9487e33b27fbdcba2b62103f16d3c37472d3 against 4f8d5e638ed4ff802ff8e9af9ee6e9673c09c099 and explicitly resolved review 2724's stale cleanup finding. Forgejo CI #555 passed. It found one valid medium issue in the shared asynchronous path: a native completion could clean up after cancellation but send nil to the waiter, whose select could then report success because both cancellation and result were ready.

Disposition: fixed. A completion that observes cancellation now publishes the context error after cleanup, and a waiter taking the result branch rechecks cancellation and cleans up before returning that error. This also covers cancellation after a successful completion enqueues its result but before the waiter selects. TestCancelledCompletionCannotReportSuccess controls both orderings synchronously inside start, preventing the waiter from selecting until both channels are ready; 100 iterations per ordering exercise either selection branch and require both a cancellation error and cleanup. A result from deliberately removed content can no longer publish a live handle or successful replacement. The existing delayed A/B completion regressions still cover generation safety. This is shared ordering evidence, not native macOS runtime evidence.

**agent:codex/t3-b903096c** at 2026-10-22T20:47:05Z

Validation for review 2725's result-race fix: targeted CGO_ENABLED=0 notify tests passed, including 100 controlled iterations of each cancellation/completion ordering; full just ci passed in the library/examples, including all eight prescribed cross builds and strict ticket checks. The prior cbdb948 GUI suites passed locally on both Linux stacks and its Forgejo CI #555 passed; this follow-up changes only asynchronous result coordination, so relevant headless/control-path and full compile checks were rerun. Native macOS/Windows/BSD runtime validation remains unavailable. A fresh review follows this push.

**agent:codex/t3-b903096c** at 2026-10-22T20:50:06Z

Follow-up audit while implementing tray availability found the same D-Bus disconnect selection hazard in managed notifications: godbus closes both its signal channel and connection context on disconnect, and selecting the closed signal channel returned without invalidating outstanding handles. Fixed that terminal branch to atomically mark the backend unavailable and emit the same handle-ending event as the context branch. TestManagedDBusConnectionLossEndsHandles closes real dedicated connections across 30 clients on a private bus, requiring every live handle to end with ErrUnavailable and later Post calls to fail. Explicit Client.Close remains suppressed because backend.close marks alive false first. Targeted cgo-free notify tests pass. This is part of the requested notification service/lifetime guarantees; full checks and another exact-head/base review follow.

**agent:codex/t3-b903096c** at 2026-10-22T20:50:28Z

Clean full review for 8798ad126239e84d4cb926b803109c5403467753 against 4f8d5e638ed4ff802ff8e9af9ee6e9673c09c099 is recorded in PR comment 33574: run 53745c14-9362-4a67-b43b-ac5cbc85f880, Actions #560. It explicitly resolves review 2725's result-race finding and reports no additional findings. Forgejo CI #558 passed. The subsequent self-audited disconnect fix passes targeted private-bus notification tests and full CGO_ENABLED=0 just ci, including all eight cross targets, both modules and strict ticket checks. It will receive its own new exact-head/base review; the clean review at 8798ad1 is not claimed for later source. No merge/release performed.

**agent:codex/t3-b903096c** at 2026-10-22T20:54:16Z

PR #82 is ready for owner review/merge at exact source head 9ffc04f7a1b66f2fb45b53da3f9a18b981ca7cdc against base 4f8d5e638ed4ff802ff8e9af9ee6e9673c09c099. Clean full review Actions #564 (27048), run 1d45de26-f37a-47fb-8622-5dcbddc4308a, reports no additional findings and confirms prior resolutions; PR clean-status comment 33574 records the named revision. Forgejo CI #562 passed its check and both Linux GUI stacks. Current head has all original review 2722 fixes, follow-up 2724/2725 fixes, individual dispositions, and the audited bus-disconnect fix. Kuiske received tested source and exact check/review evidence. Native bundled macOS delivery/permission/delegate validation remains unavailable; cross builds and control-path regressions are not that evidence. This note is recorded on the subsequent tray work branch to preserve the clean reviewed notification head. No merge or release is authorized/performed.

**agent:codex/t3-b903096c** at 2026-10-22T22:48:11Z

done to in-progress: Integration PR #89 review 2747 confirms notification autorelease pools lack OS-thread pinning.

**agent:codex/t3-b903096c** at 2026-10-22T22:48:11Z

Accepted high finding in PR #89 review 2747 at 3d01112: notificationPool creates/drains a thread-local NSAutoreleasePool on arbitrary unpinned Go workers. Mirror the existing tray OS-thread pinning, and add a native Darwin notification-pool regression that yields and compares native thread identities. Native execution is unavailable locally; compile the regression without cgo and require mirror CI evidence.

**agent:codex/t3-b903096c** at 2026-10-22T22:48:31Z

notificationPool now locks its worker to one OS thread before pool allocation and defers unlock before drain, ensuring drain completes while pinned. Added a notification-specific Darwin worker regression with 1,000 autoreleased NSNumber allocations/yields and pthread_self identity checks, following the already-reviewed tray native test. The Darwin/arm64 notification test binary compiles with CGO_ENABLED=0; native execution is unavailable here. Linux notify/tray/updater regressions pass. NSThread in the plan was replaced by pthread_self to match the existing tray fixture and directly compare native OS-thread identity.

**agent:codex/t3-b903096c** at 2026-10-22T22:51:53Z

Notification pool affinity finding in PR #89 reviews 2747 and 2748 is resolved by 13b3530. Fresh full review Actions #637 / run de8ca529-80d3-4dbc-8148-452d797bab75 is clean at 13b3530 / 9196aaf and explicitly confirms resolution. Full combined CGO_ENABLED=0 just ci/all eight builds pass after the fix; Linux package regressions execute, and Darwin/arm64 notification test binary compiles. Native macOS execution and packaged delivery/permissions remain unavailable locally; mirror CI must supply actual runtime evidence. Integration to main remains tracked separately.

## Summary

Notification pool affinity finding in PR #89 reviews 2747 and 2748 is resolved by 13b3530. Fresh full review Actions #637 / run de8ca529-80d3-4dbc-8148-452d797bab75 is clean at 13b3530 / 9196aaf and explicitly confirms resolution. Full combined CGO_ENABLED=0 just ci/all eight builds pass after the fix; Linux package regressions execute, and Darwin/arm64 notification test binary compiles. Native macOS execution and packaged delivery/permissions remain unavailable locally; mirror CI must supply actual runtime evidence. Integration to main remains tracked separately.
