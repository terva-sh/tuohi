---
schema: 4
id: TKT-01M5JKX2VTMC0QTWVM6CEANMQ0
title: Support safe runtime updates to tray icons and menus
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/tray
  - area/api
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:kuiske/TKT-01M44GTHZY4ZAASWMSBG1VEFHJ
    path: null
  - ref: t3-thread://v1/564d5d21-1210-473a-aa2b-918183ce9764
    path: null
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/prepare-library-changes
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: cf945c1c5152b33b4bf8f76b7cf91c4e1015d1be
  session: null
  claimed_at: 2026-10-22T20:12:40Z
  expires_at: null
archive: null
created_at: 2026-10-22T20:12:40Z
updated_at: 2026-10-22T20:12:40Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M44GTHZY4ZAASWMSBG1VEFHJ (Show a tray icon and optionally keep running when closed) needs unread/highlight icons, tooltips and menu changes at runtime. Add a safe API for icons and light/dark/template variants, tooltip and menu state/items, including checked/disabled state, without removing and recreating the tray. Preserve callbacks, Dock behavior and static consumers; define threading and lifetime semantics.

## Acceptance criteria

- [ ] An existing tray supports runtime icon variants, tooltip and menu/item state updates without replacing the status item or resetting Dock policy.
- [ ] Updates and removal have documented thread/lifetime semantics; existing callbacks and static consumers retain their behavior.
- [ ] Meaningful tests and a public-API example exercise updates; documentation names platform limitations.
- [ ] CGO_ENABLED=0 checks and all prescribed cross builds pass, with native runtime evidence recorded honestly.
