---
schema: 4
id: TKT-01M3J59M6JQZJWB23HPXXQZVZR
title: Report missing WebKitGTK symbols as an error, not a panic
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
  - area/ffi
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T21:13:08Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

On Unix, a missing required symbol panics inside `ensureInit`'s `sync.Once` (`pure.RegisterLibFunc` panics, `pure/func.go:34-40`, called from `lib_unix.go:326` onward). It should come back as the error `ensureInit` already returns. `sync.Once` treats a panicking call as done, so a caller that recovers carries on with nil func vars. That is how a nil `gtkWindowResize` became a fatal crash in the tests.

### Fix

Resolve each symbol with `Dlsym` and collect the missing ones into one error naming the library and symbols, or recover inside the `Once` and set `initErr`. The error should say which WebKitGTK version is needed.

## Acceptance criteria

- [ ] A missing required symbol makes ensureInit return an error naming the library and symbol
- [ ] No panic escapes ensureInit's sync.Once
