---
schema: 4
id: TKT-01M5JV0W5PXH4ZJMBHRTJ8GSFY
title: Diagnose stalled native permission scenarios in Linux CI
type: bug
status: in-progress
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
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/updater-bundle
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 643ba16553867276b3be2ecb6757f18ad4a2b084
  session: null
  claimed_at: 2026-10-22T22:17:04Z
  expires_at: null
archive: null
created_at: 2026-10-22T22:17:04Z
updated_at: 2026-10-22T22:23:03Z
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

- [ ] Identify the failing native operation with bounded stage diagnostics and preserve strict permission assertions.
- [ ] Validate any fix on both Linux WebKitGTK stacks, cgo-free CI and cross builds; record review and CI dispositions accurately.

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
