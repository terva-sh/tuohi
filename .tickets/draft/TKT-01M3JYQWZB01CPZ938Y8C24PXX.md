---
schema: 4
id: TKT-01M3JYQWZB01CPZ938Y8C24PXX
title: Deliver binding replies and events only to trusted documents
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
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T02:50:41Z
updated_at: 2026-09-28T02:50:41Z
created_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
updated_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
extensions: {}
---

## Description

### What

Binding replies and events from Go must reach only the document that is allowed to receive them.

### Current behaviour

- `webview.resolve` (`engine.go`) sends a binding's result by calling `Eval` with `window.__webview__.onReply(...)`.
- `Emit` sends events the same way.
- `Eval` runs in whatever top-level document is current when it runs.

After a navigation, that can be a page on an untrusted origin. Such a page gets no bridge, but it can define its own `window.__webview__` with an `onReply`, and then it receives the results of calls the trusted page made before it left. It also receives every event Go emits.

The call side is closed by TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings): a page without the bridge token cannot call a binding. This ticket covers what flows the other way.

### Direction

- **Per-document id.** The bridge makes a random id for each document and sends it with every message. The script `resolve` evaluates checks that the current document has that id before it calls `onReply`. A page that never saw the id cannot pass the check with a getter, because it never sees the id's value.
- **Events.** For `Emit`, either check the current URI against the trusted origins at `Eval`, or use the same id check against the last trusted document seen.
- **Alternative to weigh:** have each engine refuse `Eval` of bridge traffic while the view is on an untrusted origin. That is simpler, but it races with navigation the same way the old Linux sender check did.

## Acceptance criteria

- [ ] A page on an untrusted origin that defines its own window.__webview__ receives no binding result and no event
- [ ] The trusted page's calls and events still work across its own reloads
