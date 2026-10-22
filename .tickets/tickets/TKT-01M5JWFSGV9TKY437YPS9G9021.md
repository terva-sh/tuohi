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
updated_at: 2026-10-22T22:42:51Z
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

- [ ] Updater stack lands on main with final-base review and passing checks.
- [ ] Desktop notification, tray and window lifecycle stack lands on main with final-base review and passing checks.
- [ ] Mirror is synchronized and native validation results or remaining gaps are reported accurately.

## Implementation plan

Retarget PR #87 to main so the portable installer, bundle support and examples land together; request fresh full review for the cumulative main diff. Merge the reviewed desktop stack into a new main-based branch, inspect conflict resolution and consumer docs, validate cgo-free checks and both GUI stacks, then request its own final-base review. Merge only passing reviewed heads using SHA guards, sync the GitHub mirror normally and report native check status. Preserve separate source commits and all existing review dispositions; avoid rewriting branches or publishing releases.
