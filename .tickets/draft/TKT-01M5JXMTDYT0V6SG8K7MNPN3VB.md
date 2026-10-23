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
updated_at: 2026-10-23T23:41:52Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
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

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:41:52Z

Historical Linux CI evidence correction/recurrence: the owner supplied the actual PR #100 Actions #706 GUI log for head 52fa5f9be7378035c62a593f88749a63dd7d6b12. The sole failure was WebKitGTK 6.0 root-package TestViewMethodsFromBinding at goroutine_calls_test.go:121:

```
View methods from a binding: got start=none eval=none navigate=none offUI=false request=none page=none ready=0; want start=/a eval=eval:/a navigate=/b offUI=true
```

The existing 20-second deadline observed no HTTP fixture request, no page-script report, and no Ready callback. This establishes the missing phases, not their cause. GTK6 maximize, updater and all examples passed; the GTK4.1 library, maximize and examples passed. GUI step: 1m48s; GTK6 root: 56.956s; GTK4.1 root: 35.369s. Actual runner evidence: https://git.local.sothr.com/terva-sh/tuohi/actions/runs/706/jobs/1 . Accessibility warnings appeared throughout and are not an established cause.

This matches existing unselected draft TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports), including the PR #97/run #683 signature. Append there rather than file a duplicate or promote it. goroutine_calls_test.go and Linux runtime source are unchanged from the base by PR #100. Both CI engines passed on d39030fab606e0fd74eac57c184f3d6123e3938d (Actions #708), but Darwin fixes do not establish a correction for this intermittent Linux scenario. Preserve strict assertions/timeouts; no further Linux source changes or repeats are required for these native macOS tickets.

The earlier local bubblewrap/process-watch limitations arose only in disposable emulation, and permission failures only in separate macOS baseline testing. None was the cause shown in runner #706. This supersedes the earlier note that the historical runner failure was unavailable/unattributed as to test identity; its underlying cause remains unknown. Parent owns final-head gate readiness and merge.

PR evidence: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34695
