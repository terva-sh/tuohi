---
schema: 4
id: TKT-01M5JWFSGV9TKY437YPS9G9021
title: Land reviewed desktop and updater stacks on main
type: chore
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/release
  - area/ci
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
created_at: 2026-10-22T22:42:41Z
updated_at: 2026-10-22T23:12:40Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner requested review and merge of remaining PR work. Several stacked PRs were merged into parent branches after those parents had already merged to main. Preserve all reviewed commits and land the missing desktop and updater features through internal PRs, then fast-forward the public mirror. This authorizes integration merges and normal mirror synchronization, but no tags, releases or feed publication.

## Acceptance criteria

- [x] Updater stack lands on main with final-base review and passing checks.
- [x] Desktop notification, tray and window lifecycle stack lands on main with final-base review and passing checks.
- [x] Mirror is synchronized and native validation results or remaining gaps are reported accurately.

## Implementation plan

Retarget PR #87 to main so the portable installer, bundle support and examples land together; request fresh full review for the cumulative main diff. Merge the reviewed desktop stack into a new main-based branch, inspect conflict resolution and consumer docs, validate cgo-free checks and both GUI stacks, then request its own final-base review. Merge only passing reviewed heads using SHA guards, sync the GitHub mirror normally and report native check status. Preserve separate source commits and all existing review dispositions; avoid rewriting branches or publishing releases.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T22:43:36Z

Integration has no source conflicts; README additions merge automatically. PR #87 now targets main and has fresh full review dispatched. Windows mirror CI previously exercised only the root and instance packages, so it would omit new updater helper/restart and notify/tray regressions. Extend its existing subpackage step to all four test packages; Linux validates shared behavior and Windows compilation locally, while actual Windows execution remains hosted evidence pending mirror sync.

**agent:codex/t3-b903096c** at 2026-10-22T22:51:54Z

PR #87 landed in main as 19811cf after all three checks passed at e2dd83e with clean exact main-base review #632. PR #89 source 13b3530 has clean review #637; both review rounds 2747/2748 report the same notification pool issue, fixed in 13b3530. Merge origin/main into integration without source conflicts and request a fresh full review on the now smaller desktop-only main diff. Full combined local cgo-free CI/all cross builds and both Linux GUI stacks pass; no native macOS/Windows/BSD execution is claimed.

**agent:codex/t3-b903096c** at 2026-10-22T22:59:33Z

Post-merge mirror run 39867971162 on 19811cf passed native macOS, Windows and both Linux stacks, but golangci-lint failed with 14 errcheck findings in updater deferred read-only closes and best-effort cleanup. The write paths already check Sync and successful Close explicitly. Make discarded cleanup errors explicit without changing install outcomes, audit examples and all target linter variants with pinned v2.13.1, and include this mirror regression repair in PR #89. This runtime evidence predates the desktop stack; Windows updater tests were not yet part of its old CI subpackage step.

**agent:codex/t3-b903096c** at 2026-10-22T23:03:18Z

Pinned golangci-lint v2.13.1 now passes both modules for darwin/linux/windows using Go 1.26.0, matching go.mod and the mirror toolchain family. Explicit discards cover best-effort cleanup/read closes, example lock release and progress Emit; successful writer Sync/Close checks remain unchanged. Removed one newly added unused bind-test stub method. Attempting lint with local Go 1.27 exposed a linter export-format incompatibility, resolved by using the prescribed minimum toolchain; the long scratch prefix also exceeded Unix socket test limits, so validation uses the existing shorter task scratch directory. Neither environment issue motivated a production change.

**agent:codex/t3-b903096c** at 2026-10-22T23:07:36Z

PR #89 merged as c7c7681b717aa9401a7ef391dd0e4b41b533a934 after clean full review ca0f984d-1b50-41aa-b16e-c957efb08847 at head c4a6e2bc269face371a4ba9c331df952fc103e9f against base 19811cf05d3e50e66167902040dbb67fa079fbe3. All three exact-head checks passed: review and both CI jobs (#643). PR #87 had already merged as 19811cf. Normal just sync-github --yes fast-forwarded GitHub main to c7c7681; no public version, tag, release or feed was published. All selected implementation source is now on main.

The cgo-free local/hosted Linux GUI, headless and eight-target cross-build checks passed. Latest lint repairs passed pinned lint for both modules on darwin/linux/windows and BSD. Native GitHub post-merge validation for the newly integrated desktop stack is pending; the earlier 19811cf run passed macOS/Windows/Linux runtime and cross-build jobs but failed lint, now repaired. Compilation is not native runtime evidence. Packaged macOS notification permission/delivery and team-signed bundle install/rollback/relaunch acceptance remain unvalidated, as do Windows managed notifications (explicitly unsupported). The original GTK4 no-report stall remains unproven and tracked in TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports); passing reruns and diagnostics are not a claimed production fix.

**agent:codex/t3-b903096c** at 2026-10-22T23:12:40Z

Native desktop merge run 39869664509 completed: macOS, all Linux GUI jobs, lint and all cross-build jobs passed. Windows notify/tray/updater subpackages passed, but TestAcquireSendRoundTrip in instance timed out after successful Send, and subsequent Windows examples/WebView2 steps were skipped. The failure is now tracked by TKT-01M5JY4A132MT85W66ED42B2Q3 (Drain Windows pipe messages sent before the server starts), with a source fix and deterministic native regression in PR #90. This supersedes the earlier pending-native-status note; native Windows validation is not complete. Packaged macOS notification delivery and team-signed bundle acceptance remain unvalidated.

## Summary

Updater PR #87 and desktop integration PR #89 are merged on main, with fresh exact-head/base clean reviews and passing internal checks. Both forges were synchronized at c7c7681b717aa9401a7ef391dd0e4b41b533a934. Local cgo-free tests, both Linux GUI stacks, all prescribed cross builds and lint passed. Native mirror checks for the desktop merge are pending; packaged macOS notification and signed-bundle acceptance remain explicit gaps. No release was published.
