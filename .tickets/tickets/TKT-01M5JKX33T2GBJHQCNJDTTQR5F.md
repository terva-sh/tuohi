---
schema: 4
id: TKT-01M5JKX33T2GBJHQCNJDTTQR5F
title: Expose tray host registration state and availability changes
type: task
status: in-progress
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
updated_at: 2026-10-22T20:30:16Z
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

- [ ] A consumer can inspect tray host/registration state and observe absence, registration failure, disappearance and recovery.
- [ ] State/callback lifetime is concurrency-safe, stops on removal, and does not imply registration proves visibility.
- [ ] Linux mock-session-bus tests cover missing, failing, disappearing and returning hosts; docs/examples show conservative close-to-tray policy.
- [ ] CGO_ENABLED=0 checks and prescribed cross builds pass; platform guarantees and runtime validation limits are recorded.

## Implementation plan

### Approach

Add tray.CurrentAvailability() returning Running, Registered, HostPresent, Err and a tray-lifetime Generation, plus Config.OnAvailability for serial worker callbacks. Coalesce rapid changes to the latest state, avoid native/UI locks in user callbacks, and stop queued delivery on removal (an already admitted callback may finish). Consumers should inspect the current generation/state before hiding the only window. Registration/host presence never proves the user can see an icon.

Linux subscribes before its initial watcher registration and reports missing watchers, registration errors, absent hosts, watcher owner loss, host registered/unregistered signals, property changes, bus loss and recovery. Read IsStatusNotifierHostRegistered separately from successful item registration; re-register against a captured unique watcher owner so restart races do not report stale success. Preserve Set's existing nonfatal missing-watcher behavior, now observable through state instead of stderr alone. Native status-item creation/shell registration establishes conservative macOS/Windows state; do not claim visibility. Windows should observe shell taskbar restart and re-register, and report native registration failures.

### Validation and alternatives

Private Linux D-Bus watcher tests cover no watcher, failed registration, watcher with no host, host disappearance/return, watcher replacement and bus loss. Document GNOME extensions, WSLg/nested sessions and consumer-owned background policy, with a public example. Couple this PR to the runtime-update ticket because both alter the same backend lifetime/concurrency boundary, while keeping separate acceptance evidence. Run the prescribed cgo-free checks and cross builds and distinguish native Linux protocol tests from unavailable native macOS/Windows execution. A bool returned from Set was rejected because host availability changes after startup. Treating registration as visibility was rejected because even a registered host may suppress or hide the icon.
