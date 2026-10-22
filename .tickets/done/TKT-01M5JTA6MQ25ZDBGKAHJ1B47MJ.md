---
schema: 4
id: TKT-01M5JTA6MQ25ZDBGKAHJ1B47MJ
title: Synchronize GUI clipboard and binding lifecycle scenarios
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
assignees: []
milestone: null
parent: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T22:04:41Z
updated_at: 2026-10-22T22:10:38Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

CI run #599 on PR #87 exposed two existing GUI scenario races, in root tests unchanged by the updater. clipboardScenario starts off-thread synchronous clipboard work before webview.Run enters the UI dispatcher loop; it can correctly receive errUILoopStopped. goroutineCallsScenario assumes asynchronous Eval completes before a following Navigate; navigation can replace the page before its report arrives, and its three independent 10-second waits race the 30-second watchdog. Gate clipboard work on a dispatched UI turn, chain navigation after the evaluation report, use a single shorter observation deadline, and cancel watchdogs after each scenario. Preserve actual off-UI API calls and exact expected reports. Authorized as necessary validation work under the selected updater epic; no production API behavior change.

## Acceptance criteria

- [x] Off-thread clipboard work starts after a UI loop turn; binding navigation waits for the evaluated page report, with bounded deterministic results.
- [x] Both Linux WebKitGTK stacks, cgo-free CI and cross builds pass; review dispositions and validation evidence are recorded.

## Implementation plan

Use a queued native dispatch turn as clipboard startup barrier; navigate from the evaluation-report binding; share one observation deadline and cancel watchdog timers. Run both native Linux stacks, required ci/cross builds, then fresh exact-head/base review. Archive failure and fix in PR #87 and ticket.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T22:06:48Z

Investigated both failures in PR #87 Actions run #599 at 3f5e402. They concern existing GUI tests unchanged from main, not the bundle installer. clipboardScenario launched its synchronous off-thread call before webview.Run marked a loop active; the dispatcher correctly refused it. goroutineCallsScenario issued Navigate immediately after asynchronous Eval, allowing navigation to discard the evaluated page; its independent report waits also raced its watchdog. Fixed by launching clipboard work from a queued UI turn, chaining Navigate from the evaluation report binding (still off the UI thread), using one observation deadline before the emergency close, and stopping both watchdogs. Existing assertions still require exact Unicode clipboard round trips and page/eval/navigation/off-thread reports. No production threading semantics changed.

Validation: CGO_ENABLED=0 just ci passes both modules and all eight prescribed targets. CGO_ENABLED=0 just test-gui passes both native Linux WebKitGTK stacks and available examples. Native macOS/Windows/BSD execution remains unavailable; compilation is not runtime evidence. Fresh full exact-head/base review is requested with this source fix; remote checks will be tracked before ticket closure.

## Summary

Fixed clipboard startup ordering and asynchronous evaluation/navigation ordering in 3f67b4d (PR #87), merged into PR #88. Both native Linux WebKitGTK stacks, both modules and all eight cgo-free CI/cross targets pass. Review 2741 inspected these changes and raised a separate macOS bundle guard issue; its diagnosis/fix is owned by the reopened bundle ticket, with individual disposition posted in PR #87. No production UI threading behavior changed.
