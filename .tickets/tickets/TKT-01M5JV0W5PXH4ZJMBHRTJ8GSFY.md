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
updated_at: 2026-10-22T22:17:04Z
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
