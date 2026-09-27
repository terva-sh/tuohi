---
schema: 4
id: TKT-01M3J1H8CPMZX9EJX8R2CQRA6P
title: Make every View method safe to call from any goroutine
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-linux
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3J59M0VJYQ3E652Y0FD90H3
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T18:20:14Z
updated_at: 2026-09-27T19:35:06Z
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

On the Unix engine (`lib_unix.go`), `webview.Navigate`, `webview.Eval`, and `webview.Focus` call WebKitGTK and GTK directly on whatever goroutine calls them. GTK is not thread-safe. Bindings run on their own goroutines (see `serialQueue` in bind.go), so a binding that navigates, evaluates script, or focuses its view makes GTK calls off the UI thread. That is the class of bug that crashed `Destroy` in TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks).

`View.Focus`'s doc comment says "the backends marshal to the UI thread when called from a background goroutine", and on Unix they do not. `Show`, `Hide`, `Maximize`, `Minimize`, `Unminimize`, and `Unmaximize` do marshal.

`View.Close` reads and clears `v.w` and `v.app` without synchronization. It is documented safe from any goroutine, but two concurrent calls race. That affects every platform.

### Fix direction

Marshal each through `dispatchMain` when `onUIThread()` is false, as `Destroy` now does. Check the macOS and Windows engines for the same gaps. Add a GUI scenario that calls each method from a binding. Make `View.Close` safe against concurrent calls.

The architecture review, TKT-01M3HWWRSJC4QVVGPW04H5CQBD, may prefer one rule for the whole `webview` surface over per-method fixes, so decide there if it has not landed first.

## Acceptance criteria

- [ ] Navigate, Eval, and Focus on Unix run their GTK calls on the UI thread from any goroutine
- [ ] A GUI scenario calls each from a binding goroutine on both WebKitGTK stacks
- [ ] Concurrent View.Close calls do not race under go test -race

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Scope widened by TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

Retitled from "Unix Navigate, Eval, and Focus call GTK off the UI thread". Windows has the same gaps.

| Method | Unix | macOS | Windows |
|---|---|---|---|
| Navigate, Eval, Focus, Raise | direct | synchronous marshal | direct |
| Internal BindBatch | direct | synchronous marshal | direct; pumps `GetMessageW` on the caller's thread |

**The rule, from `docs/architecture.md` "One threading rule":** every exported `View` and `App` method is safe from any goroutine on every engine. On the UI thread it runs in place. Elsewhere it is marshalled asynchronously, except where the caller needs a result; those wait with a timeout and return an error rather than hang when the loop has stopped. This lands in the shared engine layer from TKT-01M3J59M0VJYQ3E652Y0FD90H3 (Declare the engine interface and share the bridge core), so each method is marshalled once rather than three times.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:35:06Z

### Supersedes the timeout rule in the previous note

The previous note said calls needing a result "wait with a timeout and return an error rather than hang when the loop has stopped". A timeout cannot tell a stopped loop from a busy one. A call could return an error while its operation is still queued, then run when the loop resumes, and a caller that retries a dialog would get two. Found by terva-review on tuohi PR #4.

The rule instead:

- **Each marshalled operation carries a state:** pending, running, or cancelled.
- **The UI thread claims an operation before running it,** moving it from pending to running atomically. It skips a cancelled one.
- **The loop owner marks the loop stopped when its loop exits for good:** the end of `App.Wait` or `Run`. Anything still pending is then cancelled.
- **A caller waiting for a result gives up only when the loop is marked stopped,** never on a timer.
- **A caller that gives up cancels its operation atomically.** If the operation has already started, the caller waits for it to finish. An operation that reported failure therefore never runs.
- **A call made after the loop is marked stopped** returns an error at once, without queuing.
