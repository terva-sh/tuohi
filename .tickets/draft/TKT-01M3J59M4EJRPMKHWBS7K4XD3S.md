---
schema: 4
id: TKT-01M3J59M4EJRPMKHWBS7K4XD3S
title: Check Host on tuohi's loopback server and settle its idle shutdown
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
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

Fix three issues in tuohi's own loopback server (`app.go:1133-1567`). It serves `App.FS` over `http://localhost:PORT` on macOS always, and on Linux and Windows when `App.HTTP` is set.

- **No Host or Origin check.** Any local process, and any web page through DNS rebinding, can read `App.FS` while the server is up. The Host header only builds URLs (`app.go:1455`). Answer only requests whose Host is `localhost:PORT` or `127.0.0.1:PORT`.
- **It closes 3 s after its last request** (`loopbackIdleTimeout`, `app.go:1306`) by design, "so it serves exactly the page's initial load". The page's origin is that server, though. A lazy `import()`, a route change, or a `fetch` of an `App.FS` file after the timeout has no server to answer it. Write a GUI scenario that fetches after 4 s, and decide from the result: keep the server for the view's lifetime, or document the limit.
- **Stale comments.** `app.go:1273` says fireReady stops the server, and `lib_unix.go:1809-1810` says it stops once the first load finishes. Only `releaseLoopback` in `Destroy` and the idle timer stop it.

A consumer serving its own UI never starts this server, so this does not block git-ticket-canvas.

## Acceptance criteria

- [ ] The loopback server refuses a request whose Host is not its own localhost or 127.0.0.1 address and port
- [ ] A GUI scenario shows whether a page can fetch from App.FS after the idle timeout, and the server's lifetime or its docs match the result
- [ ] The comments about when the server stops match the code
