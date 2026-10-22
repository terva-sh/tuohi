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
updated_at: 2026-10-22T22:28:13Z
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
