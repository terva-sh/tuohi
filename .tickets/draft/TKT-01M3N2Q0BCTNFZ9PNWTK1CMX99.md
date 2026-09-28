---
schema: 4
id: TKT-01M3N2Q0BCTNFZ9PNWTK1CMX99
title: Let a consumer's test see links handed to the system browser
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-darwin
  - area/engine-windows
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T22:38:34Z
updated_at: 2026-09-28T22:38:34Z
created_by:
  id: agent:claude-code/fe5548cb
  name: ""
updated_by:
  id: agent:claude-code/fe5548cb
  name: ""
extensions: {}
---

## Description

### What

A consumer cannot see, in its own tests, that a view handed an outside link to the system browser on macOS.

### Found by

git-ticket-canvas, working ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT (Run the desktop window's GUI scenarios on macOS). On Linux its GUI scenario puts a fake `xdg-open` on PATH, clicks a link to another origin, and checks what the fake was given. On macOS, `openURL` (app_darwin.go:231) calls `NSWorkspace openURL:`, which has no PATH to stand in on. So the canvas skips that test on macOS, since following the link would open a real browser tab.

tuohi's own tests have a hook for this: the unexported `openExternal` variable (engine.go:704), replaced on the UI thread before a view exists. A consumer cannot reach it.

### Things to settle

- **The shape of the hook.** It could be a field on `App`, such as a function that receives each URL handed outside and replaces the system call when set. Or it could be an exported variable like `openExternal`. A field is scoped to one app and needs no rule about when it may be replaced.
- **Whether it covers `App.Open` as well** as a refused navigation, since both reach `openURL`.
- **Windows.** It has the same problem, so one hook should serve all three engines.

## Acceptance criteria

- [ ] A consumer's test can record every URL a view hands outside, without a real browser opening, on all three engines
- [ ] The hook's shape and when it may be set are documented
