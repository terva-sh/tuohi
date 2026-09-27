---
schema: 4
id: TKT-01M3J1H8CPMZX9EJX8R2CQRA6P
title: Unix Navigate, Eval, and Focus call GTK off the UI thread
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
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T18:20:14Z
updated_at: 2026-09-27T18:20:14Z
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
