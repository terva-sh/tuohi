---
schema: 4
id: TKT-01M3MV9EBAGG435E6V7JFQHYA2
title: Capture problems tuohi's consumers find
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
  - ref: ticket:TKT-01M3J1H8CPMZX9EJX8R2CQRA6P
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T20:28:50Z
updated_at: 2026-09-28T20:29:09Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/c04aed4f
  name: ""
extensions: {}
---

## Description

### What

One place for problems that tuohi's consumers find by building on it: git-ticket-canvas first, and terva when it adopts tuohi. Each problem is filed here as its own ticket under this epic, with the consumer ticket that found it named as a reference.

### Why

tuohi is developed alongside its first consumer on purpose, because a library nobody uses is designed in a vacuum. That only works if what the consumer runs into comes back here, where the code is, rather than staying in the consumer's store as a workaround. Its alpha handoff asks for exactly that: report problems as tickets in tuohi's store.

### How to use it

- File each finding as a child of this epic, as a bug or task, with a `ticket:<repo>/<ID>` reference to the consumer ticket where it came up.
- If the finding is already an open ticket, do not file it again. Add a note to that ticket saying which consumer hit it and how, and list it below.
- Milestones and priority are the owner's call at promotion, like any other draft. Being under this epic says where a ticket came from, not how urgent it is.

### Already tracked elsewhere

- TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine). git-ticket-canvas met it outside a binding: its GUI test had to call `Eval` from `View.Ready` rather than from a goroutine.
- The inherited `appkit` prose, including the `Package appkit` comment, is owned by the rename ticket, per AGENTS.md.

## Acceptance criteria

- [ ] Every finding from a consumer is a child of this epic or a note on the open ticket that already covers it
