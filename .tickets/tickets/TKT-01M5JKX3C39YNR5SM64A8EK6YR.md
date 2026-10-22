---
schema: 4
id: TKT-01M5JKX3C39YNR5SM64A8EK6YR
title: Add close interception and native window lifecycle events
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-linux
  - area/engine-darwin
  - area/engine-windows
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

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M44GTHZY4ZAASWMSBG1VEFHJ (Show a tray icon and optionally keep running when closed) needs to intercept a user close, hide the existing view and preserve renderer/UI state, with allow/cancel/defer decisions. Add native focus/activation and visibility events so the desktop adapter can suppress notifications for an active conversation. Specify user/keyboard close, programmatic teardown, app quit and callback threading. Preserve default close and shutdown behavior; unrelated power management is outside scope.

## Acceptance criteria

- [ ] User and keyboard close can be allowed, cancelled or deferred while preserving the same native view/renderer when hidden.
- [ ] Native focus/activation and visibility changes reach supported public callbacks with defined thread and ordering semantics.
- [ ] Programmatic teardown and App.Quit do not get trapped by interception; defaults and existing consumers retain their behavior.
- [ ] Meaningful state-machine and Linux GUI tests plus a public-API example cover lifecycle behavior; CGO_ENABLED=0 and prescribed cross builds pass, with native evidence distinguished from compilation.
