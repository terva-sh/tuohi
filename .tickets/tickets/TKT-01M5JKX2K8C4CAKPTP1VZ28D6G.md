---
schema: 4
id: TKT-01M5JKX2K8C4CAKPTP1VZ28D6G
title: Add interactive notification handles and permission management
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/notify
  - area/api
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:kuiske/TKT-01M43TRAJW4ZWVXPK79YXX9R0Q
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
created_at: 2026-10-22T20:12:39Z
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

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M43TRAJW4ZWVXPK79YXX9R0Q (Show content-free desktop notifications) needs notification activation to open the right account and room and dismissal when a room is read. Add a public managed notification abstraction with opaque application correlation IDs, activation, replacement, dismissal, capabilities, permissions and cancellation/shutdown. Investigate and implement modern macOS UserNotifications with checks before touching a center in an unbundled process. Preserve Show/ShowOpts defaults. Keep message content and credentials out of routing payloads and logs; composition/privacy policy stays in the consumer. Inline replies are outside scope.

## Acceptance criteria

- [ ] A managed notification returns a correlation handle and supports activation, replacement and dismissal where available, with explicit capability/errors elsewhere.
- [ ] Permission status/request and shutdown/cancellation are explicit; modern macOS notification access checks app bundle identity before native calls.
- [ ] Routing carries only opaque IDs, with lifecycle/concurrency and privacy behavior documented and exercised by meaningful tests and a public-API example.
- [ ] CGO_ENABLED=0 checks and all prescribed cross builds pass; native runtime evidence and unavailable platform validation are recorded separately.
