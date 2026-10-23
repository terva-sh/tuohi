---
schema: 4
id: TKT-01M5JXMTDYT0V6SG8K7MNPN3VB
title: Diagnose GTK4 windows missing initial binding reports
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
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T23:02:55Z
updated_at: 2026-10-23T04:55:55Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

PR #89 CI run #640 on 3e8d588 failed TestViewMethodsFromBinding on WebKitGTK 6.0 with start=none eval=none navigate=none offUI=false. The old log cannot distinguish navigation, page execution and bridge delivery; no native crash was reported. GTK3 and all other test packages/examples passed.

Four fresh processes in the actual CI Go 1.27 Debian trixie base image, restricted to two CPUs, passed with WebKitGTK 2.54.0-1~deb13u3 and the complete preceding TestMain scenario sequence. The container reports unavailable bubblewrap and uses the same supported headless sandbox fallback; this is reproduction evidence, not proof that the original failure is fixed. The baseline source is 3e8d588. The earlier Eval/navigation ordering fix remains intact and is not this failure's established cause.

TKT-01M5JTA6MQ25ZDBGKAHJ1B47MJ (Synchronize GUI clipboard and binding lifecycle scenarios) adds checked binding registration and independent HTTP/page-script/native-ready diagnostics while preserving exact off-thread method assertions and the existing timeout. On recurrence, use those diagnostics plus runtime/version/event evidence to isolate the missing phase before proposing a production correction. Keep this separate from TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1 (Investigate intermittent GTK4 permission test stalls); no shared cause is established. This is an unselected draft for a recurrence or new actionable reproduction method.

## Acceptance criteria

- [ ] Capture actionable missing-phase/runtime evidence or establish a repeatable reproduction.
- [ ] Any correction preserves strict bridge, navigation and off-thread method expectations and passes both Linux stacks.

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T04:55:55Z

Recurrence during PR #97 at head 14d2d9de157608208a995dab95919fba4b119020, internal CI run #683 (id 27431), GUI attempt 1: TestViewMethodsFromBinding on webkitgtk-6.0 returned start=none eval=none navigate=none offUI=false request=none page=none ready=0. The independent diagnostics now establish that the local fixture received no request, no page-script diagnostic arrived, and Ready never fired before the existing 20-second deadline. No native crash is shown; GTK3 and other packages/examples passed. PR #97 runtime edits are macOS-only, so no production Linux correction or shared cause is established. Keep strict assertions/timeouts; repeat at unchanged runtime source with a ticket-only follow-up and review carry. Failure log: https://git.local.sothr.com/terva-sh/tuohi/actions/runs/683/jobs/1/attempt/1/logs . This is new missing-phase evidence, not a fix or reproducible cause.
