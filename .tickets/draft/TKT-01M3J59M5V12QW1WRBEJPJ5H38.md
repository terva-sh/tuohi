---
schema: 4
id: TKT-01M3J59M5V12QW1WRBEJPJ5H38
title: Make the macOS main-thread rule explicit and enforced
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-27T19:25:58Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
extensions: {}
---

## Description

### What

Make the macOS rule that UI runs on the process's main thread explicit, and fail clearly when a program breaks it.

On macOS, AppKit must run on the main thread. tuohi locks the thread that first calls `Show`, and records whether it is main (`lib_darwin.go:535-542`, `:1739`). Nothing makes the program call `Show` and `Wait` from the main goroutine: no `init` locks it, and no documentation says so. A consumer that starts its HTTP server on the main goroutine and opens the window from another gets AppKit on a secondary thread, and the result is inferred to be a crash or a silent failure.

### Shape

- An `init` in the darwin engine that calls `runtime.LockOSThread`, which is the usual Go pattern for macOS UI libraries, so the main goroutine stays on the main thread.
- `Show` and `Wait` return an error when they are called off the main thread and no loop owner exists.
- The package doc and a consumer example show the loopback shape: the server in a goroutine, `Show` and `Wait` on main.

## Acceptance criteria

- [ ] The darwin engine keeps the main goroutine on the main thread
- [ ] Show and Wait called off the main thread with no loop owner return an error instead of misbehaving
- [ ] The package docs show a loopback consumer with its server in a goroutine and the window on main
