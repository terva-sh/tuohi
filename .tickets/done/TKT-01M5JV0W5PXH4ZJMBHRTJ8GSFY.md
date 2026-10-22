---
schema: 4
id: TKT-01M5JV0W5PXH4ZJMBHRTJ8GSFY
title: Bound native permission tests and report stalled operations
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/engine-linux
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
created_at: 2026-10-22T22:17:04Z
updated_at: 2026-10-22T22:28:14Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

PR #87 Actions run #611 failed only TestPermissions on Linux WebKitGTK 6.0: the third (clipboard-granted) run returned no report. The previous permission runs, both newly synchronized GUI scenarios, updater tests and headless/cross checks passed; WebKitGTK 4.1 passed. This is separate from the known GTK3 empty-clipboard crash (TKT-01M3W87PKSJTC1ZP2AGHWETXVN — Survive WebKitGTK 4.1 2.54 crashing on paste of an empty clipboard). Investigate the unchanged native permissions harness, retain strict permission/security expectations, and improve stage diagnostics and lifetime deadlines rather than blindly retrying or relaxing expected results. The owner authorizes necessary validation fixes under the selected updater epic. Native macOS/Windows execution is unavailable.

## Acceptance criteria

- [x] Validate any fix on both Linux WebKitGTK stacks, cgo-free CI and cross builds; record review and CI dispositions accurately.
- [x] Timeouts identify the last awaited browser operation; one observation deadline precedes teardown and strict permission assertions are preserved.

## Implementation plan

Inspect native logs and browser operations; add progress/error reporting, align observation/watchdog lifetimes and stop watchdogs; reproduce under a constrained CPU where possible. Keep the GTK3 known-issue workaround and strict native permission verdicts. Validate both Linux stacks/full cgo-free CI, request fresh review and record unresolved runtime evidence honestly.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T22:20:17Z

CI #611 at 643ba16 failed only the clipboard-granted permission run on WebKitGTK 6.0 with no report; no updater failure, process panic or native crash appears in the log. The native log does not establish which browser promise stalled. Confirmed a separate harness defect: its page wait (40 seconds) and click wait (10 seconds) could outlive its 45-second close watchdog. Changed the observation phases to share one 40-second deadline, stopped the watchdog on return, and added browser stage reports (video/audio/read/notify/frame/click) used in timeout errors. Strict expected permission and gesture results remain unchanged. This improves diagnosis and lifetime correctness; it does not yet establish the original stall's root cause.

Full CGO_ENABLED=0 just ci and all eight targets pass. Reproducing with the workflow's documented Go 1.27 Debian trixie image under rootless podman, two CPUs, WebKitGTK 2.54.0-1~deb13u3, a private bus/Xvfb, and required GUI checks; image/tool caches and builds are confined to task scratch/default container storage. Four fresh process runs are planned, followed by both local native stacks. Fresh reviews are required after publishing this source change.

**agent:codex/t3-b903096c** at 2026-10-22T22:23:03Z

CI #611 disposition: the failure was TestPermissions, clipboard-granted run, on WebKitGTK 6.0; all updater tests and the newly synchronized binding/clipboard scenarios passed. No crash or exact stalled browser operation was identified by the old no-report message. Four fresh process executions in the actual CI base image, limited to two CPUs with WebKitGTK 2.54.0-1~deb13u3, passed TestPermissions. The original stall is unconfirmed and is not claimed fixed.

Addressed an independently confirmed harness timing error: page setup and click now share one 40-second observation deadline, rather than allowing a 40+10 second observation to race the 45-second watchdog. Watchdogs are stopped after each scenario. Progress bindings record the last awaited browser operation in timeout/early-loop-exit failures. All expected permission, cross-origin, gesture and pointer-lock verdicts remain strict; no production permission behavior or GTK3 known-issue workaround changed.

Validation: full CGO_ENABLED=0 just ci and all eight cross targets pass. Full just test-gui passes both available native Linux WebKitGTK stacks and examples. Native macOS/Windows/BSD execution remains unavailable. Full fresh exact-head/base reviews and hosted CI will be requested after publishing this test-only source improvement; the diagnostic ticket remains open pending its disposition. The ephemeral reproduction container was removed and the task-pulled base image cleaned up; logs/builds stay in task scratch.

**agent:codex/t3-b903096c** at 2026-10-22T22:28:13Z

Final hosted validation: PR #87 source 0b1c1d9f728bdec67b63bacedba4cdb9458c0880/base 1212b665f036c6edea73a1d63f1530901a2789f8 has clean full review ab81960a-1775-4ec9-91f4-3f0d1ede22dc (Actions #616) and both CI jobs green (#615). Integrated PR #88 source 96ce233d0af58d9884729450ce00c9e85f7964a7/base 0b1c1d9 has clean full review 24f3f522-7764-499a-a4de-ac9419f696e7 (#617) and both CI jobs green (#618). Local full cgo-free ci/all eight targets and both native Linux GUI stacks pass; four fresh CI-image permission runs pass.

Scope clarification after investigation: this ticket delivers bounded observation, stopped watchdogs and stage diagnostics. The original acceptance wording asked to identify the exact stalled native operation, which existing logs cannot establish and four controlled reproductions did not recover. That causal investigation is explicitly preserved, unfulfilled, in TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1 — Investigate intermittent GTK4 permission test stalls, left in draft. The revised acceptance line describes only the observable harness behavior actually implemented and validated. Closing this harness change does not declare the historical stall fixed.

## Summary

Implemented shared permission observation deadline, cancelled watchdogs and stalled-browser-stage diagnostics in 0b1c1d9, PR #87 and integrated #88. Strict expected permission/security results retained. Both local Linux GUI stacks, four CI-image runs, full cgo-free ci/all eight targets and latest hosted jobs #615/#618 pass; exact source reviews #616/#617 clean. Original unconfirmed stall is preserved separately in TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1; no causal fix is claimed.
