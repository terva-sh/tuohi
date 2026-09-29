---
schema: 4
id: TKT-01M3JYQWZB01CPZ938Y8C24PXX
title: Deliver binding replies and events only to trusted documents
type: task
status: done
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
updated_at: 2026-09-29T05:05:07Z
created_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
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

- [x] A page on an untrusted origin that defines its own window.__webview__ receives no binding result and no event
- [x] The trusted page's calls and events still work across its own reloads

## Implementation plan

### Approach: one per-view reply key, checked by every script Go evaluates to hand the page something

The view makes a second secret alongside the bridge token: the reply key, 32 random bytes in hex. The bridge sets it on `window.__webview__` as `__key`, a property that cannot be changed or removed, and only in trusted top-level documents, because the gate runs first.

`bridgeGuard(key, body)` wraps each script Go evaluates to deliver something:
- a binding's result (`resolve`);
- an event (`Emit`, through `events.guard`);
- a live bind or unbind (`BindBatch`, `Unbind`).

The wrapper is `(function(){'use strict';var w=window.__webview__;if(!w||w.__key!==KEY){return;}BODY})()`.

An untrusted document never holds the key:
- **A getter learns nothing.** A getter it defines for `__key` is called, but the comparison is made by tuohi's script with `!==`, so no coercion or callback reveals the value.
- **The source stays hidden.** The wrapper is strict, so a sloppy page function cannot reach its source through `caller`.

### Found while reading

- **Events reach every document.** The events API is installed by a document-start script that is not gated, so `window.events` exists in every document. Before this change, `Emit` delivered to an untrusted page's listeners even if that page defined nothing.
- **The key check covers it.** The guard checks the bridge's key before calling `window.<events>._dispatch`, so events go to trusted documents only.

### Alternatives considered

- **A per-document id (the ticket's first direction).** Rejected. Go would have to learn each new document's id, from a message it posts, before it could emit to that document. Events emitted between a reload and that message would be lost, which works against the second acceptance criterion. Replies to a call made by a previous document of the same trusted origin are already harmless: `onReply` ignores an unknown id.
- **Reusing the bridge token as the check.** Rejected. The key has to be readable by the scripts Go evaluates, so it is readable by the trusted page's own scripts. Were it the token, a leak would let anyone who got it post as the bridge. As a separate key, a leak only lets a later untrusted page receive.
- **Refusing Eval while the URI is untrusted.** Rejected, as the ticket predicted: it races with navigation, as the old Linux sender check did.
- **Encrypting what is sent,** so that the key never leaves the bridge's closure. Rejected: WebCrypto is asynchronous, which would reorder replies and events, and a synchronous cipher in the bridge is a lot of code for the case where a trusted page leaks its own key.

### Not guarded

- `View.Eval` is the consumer's own script, and stays unguarded.
- The Linux `onAppRegionState` Eval carries only a resizable flag, no data.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T04:53:53Z

### Implementation and tests (fix/reply-trust)

- **`TestBridgeGuardScript` (Node).** It runs the real bridge and events API in a trusted stub document and in an untrusted one. The untrusted document defines a fake `window.__webview__` and a sloppy getter for `__key` that reads `getKey.caller`. The trusted document receives the event, and a pending call settles through the guarded reply. The untrusted document receives nothing, its getter is called twice, and it learns no key. An unguarded control delivers to it. Three controls each fail:
  - removing the key check;
  - dropping `'use strict'`, which leaks the key through `caller`;
  - a bridge without the key.

  The first version of the caller probe used method-syntax getters, which have no legacy `caller`, so the strict-mode control passed. It now uses a plain `function`.
- **`TestRepliesOnlyToTrusted` (GUI, every engine).** A trusted page starts a binding call that Go holds open, and the view leaves for about:blank, confirmed by `pageURL` and by the bridge falling silent. The blank page defines a fake bridge and an events listener. Go releases the call, emits an event, and sends one unguarded reply as a control. The blank page then carries what it received to a trusted `/report` page in the fragment, where a call and an event must work again.

  Want: `blank=control call=5 event=after`. With the guard disabled on GTK4, it got `blank=event secret-event,control,reply "secret-result"`.

Checks passed: `just ci`, `just test-gui` on both stacks, and golangci-lint on three GOOS.

**agent:claude-code/t3code-72958710** at 2026-09-29T05:00:42Z

### Review disposition for PR #29, round 1 (terva-review run on c9ce209)

GitHub run 36523666803 on c9ce209 passed every job, including `TestRepliesOnlyToTrusted` on macOS and Windows.

1. **Medium: wait for the pending reply before leaving the untrusted page.** Fixed. The blank page no longer relies on a fixed sleep. It leaves for the report page only when both of these hold:
   - the unguarded control reply has arrived;
   - the reply and the event have each been tried there.

   Every guarded delivery reads the fake `__key`, and an unguarded one is received, so either counts as tried. After 10 s without both, the page reports `untried` and the step fails.

   Controls on GTK4:
   - With the guard disabled, the page receives both the reply and the event.
   - With the guard disabled and the reply held back 1.5 s after release, the page still receives the late reply (`blank=event secret-event,control,reply "secret-result"`). This is the case the 500 ms sleep missed.
   - With the guard in place and the reply 1.5 s late, the test passes.

Checks after the fix: `just ci` and golangci-lint pass, and the scenario passes on both WebKitGTK stacks.

## Summary

Landed in PR #29 (e66fc6e).

Each view now has a reply key besides its bridge token. The bridge sets it, in trusted top-level documents only, as a `__key` on `window.__webview__` that cannot be changed or removed.

Every script Go evaluates to deliver something is wrapped by `bridgeGuard` in a strict check of that key. That covers a binding's result (`resolve`), an event (`Emit`), and a live bind or unbind. So:
- An untrusted document receives nothing, even when it defines its own `window.__webview__`. A getter it defines for `__key` learns nothing, and strict mode keeps it from reading the script's source through `caller`.
- `Emit` no longer reaches every document. Before, the ungated events API meant it did.

The key is kept apart from the token, so a trusted page that leaks it lets a later page receive, but never call Go. The alternatives rejected (a per-document id, reusing the token, refusing Eval on an untrusted URI, and encryption) are in the plan and in `docs/architecture.md`.

Tests:
- `TestBridgeGuardScript` (Node). Its controls fail without the key check, without strict mode, and with a bridge that lacks the key.
- `TestRepliesOnlyToTrusted`, a GUI scenario on every engine. about:blank receives neither a held call's result nor an event, including a reply arriving 1.5 s late, which the control catches. Back on a trusted page after a reload, calls and events work.

Not guarded: `View.Eval`, which is the consumer's own script, and the Linux `onAppRegionState` flag, which carries no data.
