---
schema: 4
id: TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1
title: Investigate intermittent GTK4 permission test stalls
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/engine-linux
assignees: []
milestone: null
parent: null
origin: TKT-01M5JV0W5PXH4ZJMBHRTJ8GSFY
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T22:28:12Z
updated_at: 2026-10-23T05:01:02Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

One historical failure in PR #87 Actions #611 at 643ba16 returned no report in the third (clipboard-granted) permission run on WebKitGTK 6.0. The old harness recorded no browser stage, so its exact native operation and cause remain unknown. Four fresh two-CPU executions in the actual Go 1.27 Debian trixie CI base image with WebKitGTK 2.54.0-1~deb13u3 passed; latest complete hosted jobs #615 and #618 also pass both stacks. TKT-01M5JV0W5PXH4ZJMBHRTJ8GSFY — Bound native permission tests and report stalled operations implemented one shared observation deadline, cancelled watchdogs and browser-stage diagnostics in 0b1c1d9. That fix is reviewed and does not claim to fix the original unconfirmed stall.

If the stall recurs, use its new stage diagnostic and native/version logs to isolate the operation and distinguish harness ordering, WebKit/runtime behavior and production permission handling. Keep strict permission, origin and gesture expectations. This investigation is separate from the selected updater implementation, which has green exact-source review and CI, and from the known GTK3 empty-clipboard crash. Retain as an unselected draft until recurrence or a new reproduction method provides actionable evidence; do not call the original stall resolved solely because reruns pass.

## Acceptance criteria

- [ ] Capture an actionable stalled browser phase and native/version evidence, or document why the original observation remains unreproduced.
- [ ] Any correction preserves strict permission/security expectations and passes both native Linux stacks.

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T05:00:49Z

PR #98 GUI attempt 1 at exact head a181417e38680ad51f811d49780011bc52924eb2 failed TestPermissions on WebKitGTK 6.0: the no-permission and clipboard-granted runs produced their expected exact reports, while the camera-granted run returned "timeout at navigation". This means no progress binding report arrived before the existing shared 40-second deadline; it does not distinguish an absent load from failed initial bridge delivery, nor establish a camera permission decision failure. GTK3 and all other packages/examples passed. Failure log: https://git.local.sothr.com/terva-sh/tuohi/actions/runs/686/jobs/1/attempt/1/logs . This is a recurrence of the existing unselected investigation, separate from TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports); no shared cause is established. Release PR #98 changes docs/tickets only. Keep assertions/timeouts intact; record the limitation, request fresh review for updated release notes, and rerun CI before any merge/tag. No production fix is claimed. The prior full local CGO_ENABLED=0 just test-gui at identical Linux runtime source passed both stacks, both modules and the dedicated WM regression.

**agent:codex/t3-b903096c** at 2026-10-23T05:01:02Z

Runtime context for the PR #98 recurrence: the CI dependency log installs WebKitGTK 6.0 and 4.1 version 2.54.0-1~deb13u3 and GTK4 4.18.6+ds-2 in the existing Go 1.27 Debian trixie container. Local passing runtime coverage uses host WebKitGTK 2.52.6 and is not reproduction under that newer WebKit build.
