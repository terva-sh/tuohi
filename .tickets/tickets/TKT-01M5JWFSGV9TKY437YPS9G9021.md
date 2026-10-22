---
schema: 4
id: TKT-01M5JWFSGV9TKY437YPS9G9021
title: Land reviewed desktop and updater stacks on main
type: chore
status: in-progress
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
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/complete-desktop-stack
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 02b66db1b173ceb595769c109f8bb68f31535ab7
  session: null
  claimed_at: 2026-10-22T22:42:51Z
  expires_at: null
archive: null
created_at: 2026-10-22T22:42:41Z
updated_at: 2026-10-22T23:03:18Z
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
- [ ] Desktop notification, tray and window lifecycle stack lands on main with final-base review and passing checks.
- [ ] Mirror is synchronized and native validation results or remaining gaps are reported accurately.

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
