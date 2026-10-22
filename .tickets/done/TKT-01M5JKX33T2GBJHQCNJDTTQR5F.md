---
schema: 4
id: TKT-01M5JKX33T2GBJHQCNJDTTQR5F
title: Expose tray host registration state and availability changes
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/tray
  - area/engine-linux
  - area/api
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

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M44GTHZY4ZAASWMSBG1VEFHJ (Show a tray icon and optionally keep running when closed) must not hide its only reachable window when a tray host is absent. Linux currently reports Set success even when watcher registration fails. Expose registration/availability state and changes on watcher/host disappearance and return, including actionable failures. Registration is not a guarantee an icon is visible. Background-running and close-to-tray policy remain in Kuiske; document GNOME, WSLg and nested-session limits.

## Acceptance criteria

- [x] A consumer can inspect tray host/registration state and observe absence, registration failure, disappearance and recovery.
- [x] State/callback lifetime is concurrency-safe, stops on removal, and does not imply registration proves visibility.
- [x] Linux mock-session-bus tests cover missing, failing, disappearing and returning hosts; docs/examples show conservative close-to-tray policy.
- [x] CGO_ENABLED=0 checks and prescribed cross builds pass; platform guarantees and runtime validation limits are recorded.

## Implementation plan

### Approach

Add tray.CurrentAvailability() returning Running, Registered, HostPresent, Err and a tray-lifetime Generation, plus Config.OnAvailability for serial worker callbacks. Coalesce rapid changes to the latest state, avoid native/UI locks in user callbacks, and stop queued delivery on removal (an already admitted callback may finish). Consumers should inspect the current generation/state before hiding the only window. Registration/host presence never proves the user can see an icon.

Linux subscribes before its initial watcher registration and reports missing watchers, registration errors, absent hosts, watcher owner loss, host registered/unregistered signals, property changes, bus loss and recovery. Read IsStatusNotifierHostRegistered separately from successful item registration; re-register against a captured unique watcher owner so restart races do not report stale success. Preserve Set's existing nonfatal missing-watcher behavior, now observable through state instead of stderr alone. Native status-item creation/shell registration establishes conservative macOS/Windows state; do not claim visibility. Windows should observe shell taskbar restart and re-register, and report native registration failures.

### Validation and alternatives

Private Linux D-Bus watcher tests cover no watcher, failed registration, watcher with no host, host disappearance/return, watcher replacement and bus loss. Document GNOME extensions, WSLg/nested sessions and consumer-owned background policy, with a public example. Couple this PR to the runtime-update ticket because both alter the same backend lifetime/concurrency boundary, while keeping separate acceptance evidence. Run the prescribed cgo-free checks and cross builds and distinguish native Linux protocol tests from unavailable native macOS/Windows execution. A bool returned from Set was rejected because host availability changes after startup. Treating registration as visibility was rejected because even a registered host may suppress or hide the icon.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T20:55:24Z

Implemented Availability snapshots and serial/coalesced OnAvailability callbacks with tray generations and explicit removal semantics. Linux subscribes before initial registration, separates unique-watcher registration from IsStatusNotifierHostRegistered, verifies ownership before reporting success, observes watcher/host changes and retries failures, and marks bus loss unavailable regardless of which terminal channel wins select. Missing watcher remains a nonfatal Set result for static consumers; consumers now inspect observable state rather than relying on stderr. Watcher restart recovers in-place; a disconnected session bus requires explicit tray recreation. Windows reports actual shell registration return values, observes TaskbarCreated, and uses a two-second UI-loop registration probe to detect disappearance and recover. A failed probe-timer setup remains an explicit availability error. macOS reports status-item creation, with no independent host/visibility guarantee. No state implies the icon is actually visible.

Private-bus tests cover no watcher, rejected registration, watcher without a host, host arrival/loss, watcher disappearance/replacement, recovery within the same tray generation, and session-bus disconnection. A regression exposed the closed-signal-channel selection bug during this work and now passes; the same notification listener bug was fixed/reviewed independently in PR #82. Public docs/example demonstrate the conservative Running/Registered/HostPresent/Err prerequisite and keep background policy with Kuiske. GNOME extensions, WSLg/nested sessions, snapshot staleness, blocked native loops and unavailable native macOS/Windows/BSD validation are named explicitly. Full CGO_ENABLED=0 just ci/all eight cross targets passed; both Linux GUI stacks are being rerun. This is protocol evidence, not proof a real desktop panel displayed an icon.

**agent:codex/t3-b903096c** at 2026-10-22T20:55:49Z

Final validation: CGO_ENABLED=0 just ci passed in the library and examples with all eight prescribed cross targets and strict ticket checks. Both native Linux WebKitGTK stacks passed just test-gui, including all private-bus tray regressions and the showcase/loopback consumers. Five repeated tray-suite runs passed during concurrency/lifetime development. Native macOS/Windows/BSD UI runtime validation remains unavailable; these results do not establish visual tray presence. Publishing the coupled runtime/availability PR against t3/managed-notifications and requesting its own model review. No merge/release.

**agent:codex/t3-b903096c** at 2026-10-22T21:04:35Z

Coupled PR #83 is ready at abc50ac336d37fa3e0aeb3751a529e0fe6a2f48e against base 9ffc04f7a1b66f2fb45b53da3f9a18b981ca7cdc. Full review #569/run d8d3e9ad-7211-459b-b730-f1d09426a4a7 is clean and Forgejo CI #567 passed both jobs. Native runtime evidence is Linux only. Await owner merge; no release was published.

## Summary

Implemented with public API docs/examples, meaningful shared and native Linux regressions, cgo-free cross builds and clean reviewed source. PR #83 was owner-merged into its stack parent; final integration to main is tracked by TKT-01M5JWFSGV9TKY437YPS9G9021 (Land reviewed desktop and updater stacks on main). Native macOS/Windows validation remains a separate hosted/runtime requirement; managed notifications on Windows explicitly return ErrUnsupported.
