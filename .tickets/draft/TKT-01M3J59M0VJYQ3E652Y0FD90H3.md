---
schema: 4
id: TKT-01M3J59M0VJYQ3E652Y0FD90H3
title: Declare the engine interface and share the bridge core
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/api
  - area/bridge
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

Declare the platform boundary as a Go interface and move the logic the three engines duplicate into shared code. The design is in `docs/architecture.md`, under "One engine interface, one bridge core".

### Why

`View.w` is a concrete `*webview` defined three times, in `lib_unix.go`, `lib_darwin.go`, and `lib_windows.go`. No interface states what an engine must provide. The shared code calls about sixteen methods on each and also writes six fields directly (`eventsGlobal`, `onReady`, `onReadyFired`, `events`, `transient`, `contentBase`). Each engine re-declares those fields by hand.

The contract nothing states has already drifted. `View.Focus`'s doc promises marshalling that the Unix and Windows engines do not do. Six pieces of logic are near-duplicates across the three files:

- the message envelope parse and dispatch tail of `onMessage`;
- `resolve`;
- `BindBatch` and `Unbind`;
- the app:// URL rewrite;
- the default size and position in geometry;
- the engine ID registry.

### Shape

- An unexported `engine` interface that the shared code depends on. The per-engine files implement it.
- A shared `bridge` type that owns the bindings, the message envelope, the reply path, the events, and the internal-message switch. Engines feed it raw message bodies and the sender information they have, and run the JavaScript it returns.
- Per-view state the shared code needs moves into a struct that the shared code owns, rather than fields each engine must declare.

Do this before the security tickets change the bridge, so each one lands once rather than three times.

## Acceptance criteria

- [ ] An unexported engine interface lists every method the shared code calls, and each platform's webview satisfies it at compile time
- [ ] Message parsing, reply, bind and unbind, and internal-message dispatch live in shared code, not in each engine
- [ ] just ci and just test-gui pass, and GitHub CI passes on macOS and Windows
