---
schema: 4
id: TKT-01M3HWWRT7X1RZZYY6KFEP0ERE
title: Let only trusted origins call a view's Go bindings
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/bridge
  - security
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T16:59:08Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
extensions: {}
---

## Description

### What

Only pages the application trusts may call its Go bindings.

### Current behaviour

- **Injection.** The bind script is injected at document start into the top frame of every navigation, with no URL check (`lib_unix.go:1640`, `lib_darwin.go:1576`).
- **No sender check.** None of the three message handlers checks where a message came from (`lib_unix.go:905`, `lib_darwin.go:219-223`, `lib_windows.go:691-699`).
- **No navigation policy.** Nothing stops a view navigating somewhere else.

So a view that ends up on a remote page, through a link, a redirect, or content the application did not write, hands that page every binding.

### Direction

The architecture review settles the shape. The likely pieces are:

- an allowlist of origins per view, defaulting to the origin the view was opened on;
- a sender-origin check in every engine's message handler;
- a navigation policy that keeps top-level navigation on allowed origins, and hands everything else to the system browser through `App.Open`.

A loopback consumer's origin is `http://127.0.0.1:PORT`, so the default must cover it.

## Acceptance criteria

- [ ] A page from an origin the application did not allow cannot call any binding, on all three engines
- [ ] Every engine's message handler checks the sender's origin
- [ ] Top-level navigation away from allowed origins is refused or opened in the system browser
- [ ] A loopback-served interface works with the default policy
