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
updated_at: 2026-10-22T23:04:13Z
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

Run fresh native processes in the CI Debian Go/WebKitGTK image with two CPUs, retaining the full preceding TestMain sequence. Instrument the binding scenario with independent network request/page-script diagnostics and checked binding setup errors, preserving the same off-thread Eval/Focus/Navigate contract and exact success criteria. Do not attribute the missing report to a production cause without evidence. Regress any confirmed harness error, run full cgo-free checks and both stacks, record the exact source/runtime and request fresh review after source changes.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T22:06:48Z

Investigated both failures in PR #87 Actions run #599 at 3f5e402. They concern existing GUI tests unchanged from main, not the bundle installer. clipboardScenario launched its synchronous off-thread call before webview.Run marked a loop active; the dispatcher correctly refused it. goroutineCallsScenario issued Navigate immediately after asynchronous Eval, allowing navigation to discard the evaluated page; its independent report waits also raced its watchdog. Fixed by launching clipboard work from a queued UI turn, chaining Navigate from the evaluation report binding (still off the UI thread), using one observation deadline before the emergency close, and stopping both watchdogs. Existing assertions still require exact Unicode clipboard round trips and page/eval/navigation/off-thread reports. No production threading semantics changed.

Validation: CGO_ENABLED=0 just ci passes both modules and all eight prescribed targets. CGO_ENABLED=0 just test-gui passes both native Linux WebKitGTK stacks and available examples. Native macOS/Windows/BSD execution remains unavailable; compilation is not runtime evidence. Fresh full exact-head/base review is requested with this source fix; remote checks will be tracked before ticket closure.

**agent:codex/t3-b903096c** at 2026-10-22T22:56:50Z

done to in-progress: PR #89 CI #640 GTK4 binding scenario received no page reports; investigate independently of the previous fixed Eval/navigation race.

**agent:codex/t3-b903096c** at 2026-10-22T22:56:51Z

CI #640 at 3e8d588 fails only TestViewMethodsFromBinding on WebKitGTK 6.0 with start=none eval=none navigate=none offUI=false. GTK3 and all subpackages/examples pass. This differs from the fixed Eval/navigation ordering issue: no initial page binding arrived. Existing output cannot establish whether navigation, page script or bridge failed. Preserve strict assertions; reproduce in the CI base runtime and add independent page/network phase diagnostics if necessary.

**agent:codex/t3-b903096c** at 2026-10-22T23:04:12Z

CI #640 disposition: no initial binding report on GTK4, with no established production cause. Four fresh processes of exact baseline 3e8d588 in the Go 1.27 Debian trixie CI image, limited to two CPUs, pass with WebKitGTK 2.54.0-1~deb13u3 and the complete preceding TestMain sequence. The container reports unavailable bubblewrap and uses the supported sandbox fallback. The original failure is not claimed fixed.

Added checked binding-registration errors and independent HTTP request/page-script/native-ready diagnostics, retaining the same 20-second observation deadline, off-thread Eval/Focus/Navigate calls and exact success assertions. A scratch-only controlled page error produced the expected strict failure with request=/a, page stage=error, shown=function, step=function and ready=1, proving the new failure path distinguishes page execution from missing bridge delivery. No intentional fault was added to repository source.

Full CGO_ENABLED=0 just ci passes both modules and all eight cross targets; full just test-gui passes both Linux WebKitGTK stacks and available examples. Pinned golangci-lint v2.13.1 passes both modules on darwin/linux/windows with Go 1.26.0; make lint-bsd passes too. An initial local run used a long task scratch prefix that exceeded the existing instance test's Unix socket path limit; rerunning in the existing shorter task scratch directory passes, without changing production code. Native macOS/Windows evidence from GitHub run 39867971162 applies only to main19811cf, not this unmerged desktop head. Native packaged notification and team-signed bundle acceptance remains unavailable.

TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports) retains the original unconfirmed stall as an unselected draft. These diagnostics and the earlier synchronization fixes are complete; fresh PR #89 review/checks gate integration. No production threading/security behavior was changed or relaxed.

## Summary

Original synchronization fixes remain in 3f67b4d. Added checked binding registration and independent network/page/native-load timeout diagnostics, verified with a controlled strict-failure fixture; full cgo-free CI/all cross builds and both native Linux stacks pass. Four exact-baseline CI-image processes did not reproduce the initial-report stall; TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports) retains that unresolved investigation. PR #89 fresh review/checks gate integration; no production cause or original-stall resolution is claimed.
