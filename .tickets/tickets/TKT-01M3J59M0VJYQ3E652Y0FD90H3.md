---
schema: 4
id: TKT-01M3J59M0VJYQ3E652Y0FD90H3
title: Declare the engine interface and share the bridge core
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-92c88910
  branch: refactor/engine-interface
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-92c88910
  commit: c5ffbcae0b57fbb91d8243274d741646fd29f888
  session: null
  claimed_at: 2026-09-27T19:44:42Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-27T19:46:06Z
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

## Implementation plan

### Approach

Behaviour-preserving throughout: no change to what any engine does, only to where the code lives.

1. **`viewCore`.** A struct in a new shared file, `engine.go`, holds the eleven fields all three engines declare identically:
   - `mu`, `bindings`, `userScriptSrcs`, `events`, `calls`;
   - `eventsGlobal`, `onReady`, `onReadyFired`;
   - `serve`, `contentBase`, `transient`.

   Each platform's `webview` embeds it. Promoted fields keep every `w.bindings`-style reference compiling unchanged.
2. **The `engine` interface.** It lives in `engine.go` and lists every method the shared code calls. `var _ engine = (*webview)(nil)` checks it on each platform. `View.w` becomes `engine`, so shared code can reach an engine only through the interface. Tests that read platform fields assert to `*webview`.
3. **Shared bridge core** in `engine.go`:
   - `onMessage` parses the envelope, handles `internalBindError`, asks the engine's `handleInternal(method, params) bool` for the window messages, and otherwise dispatches the binding on `calls`;
   - `resolve` formats the reply and runs `Eval` through `Dispatch`. macOS's `Eval` already makes its own autorelease pool, so dropping the outer pool its `resolve` had changes nothing;
   - `BindBatch` and `Unbind` share every step except the script rebuild. Each engine implements `updateBindings(mutate)`, which keeps its own lock and thread choice exactly:

     | Engine | Rebuild |
     |---|---|
     | Unix | under `mu` |
     | macOS | on the main thread, under `mu` |
     | Windows | after unlocking, because the rebuild pumps messages |

### Not in this ticket

- **URL rewrite, geometry defaults, and the engine registry.** These are the other duplicates the ticket lists. They differ in more than placement: the macOS rewrite is inlined into `Navigate`, Windows uses a per-engine dispatch map, and geometry is interleaved with native calls. The description names them as examples of duplication, and the acceptance criteria require only message parsing, reply, bind and unbind, and internal-message dispatch to be shared. Leave them for the threading work, which rewrites those paths anyway.
- **Marshalling.** Which methods marshal to the UI thread is TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine).

### Verification

- **Linux:** `just ci`, and `just test-gui` on both stacks.
- **macOS and Windows:** cross-build and `go vet` for each. Their engines run only on GitHub's runners, so they are tested after the merge. That is the gap TKT-01M3HWWRXMN56AG2GNC3M92GWZ owns.
