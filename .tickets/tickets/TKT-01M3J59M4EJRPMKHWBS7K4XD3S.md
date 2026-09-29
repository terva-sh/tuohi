---
schema: 4
id: TKT-01M3J59M4EJRPMKHWBS7K4XD3S
title: Guard tuohi's loopback server and settle its idle shutdown
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/loopback-guard
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-loop
  commit: e68b5e29b982e7498129a77e9b529c5482052d50
  session: null
  claimed_at: 2026-09-29T22:09:53Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T22:10:08Z
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

Fix three issues in tuohi's own loopback server (`app.go:1133-1567`). It serves `App.FS` over `http://localhost:PORT` on macOS always, and on Linux and Windows when `App.HTTP` is set.

- **No access control.** The Host header only builds URLs (`app.go:1455`), and nothing else is checked. Two different readers can reach `App.FS` while the server is up, and each needs its own fix:
  - **A web page, through DNS rebinding.** INFERRED. Fix: answer only requests whose Host is `localhost:PORT` or `127.0.0.1:PORT`.
  - **Another local process.** A Host check does nothing here, because the process sends whatever Host it likes. Fix: give each server an unguessable token, put it in the URL the view loads, and refuse requests without it. If relative URLs must stay clean, the server may answer the first token-bearing request with a cookie that carries the token and accept either afterwards. It must never set that cookie on a request that did not present the token, or the first requester, whoever it is, gets access. That keeps out other users on the machine. A process running as the same user can still read `App.FS`, because it can read the binary and its memory. Document that limit rather than claim more.
- **It closes 3 s after its last request** (`loopbackIdleTimeout`, `app.go:1306`) by design, "so it serves exactly the page's initial load". The page's origin is that server, though. A lazy `import()`, a route change, or a `fetch` of an `App.FS` file after the timeout has no server to answer it. Write a GUI scenario that fetches after 4 s, and decide from the result: keep the server for the view's lifetime, or document the limit.
- **Stale comments.** `app.go:1273` says fireReady stops the server, and `lib_unix.go:1809-1810` says it stops once the first load finishes. Only `releaseLoopback` in `Destroy` and the idle timer stop it.

A consumer serving its own UI never starts this server, so this does not block git-ticket-canvas.

## Acceptance criteria

- [x] The loopback server refuses a request whose Host is not its own localhost or 127.0.0.1 address and port
- [x] A GUI scenario shows whether a page can fetch from App.FS after the idle timeout, and the server's lifetime or its docs match the result
- [x] The comments about when the server stops match the code
- [x] The loopback server refuses a request that lacks its per-server token, and the docs say same-user processes can still read App.FS

## Implementation plan

### Approach

- **Token in the base URL.** Each server makes 32 random bytes, base64url, when it starts. The base the view navigates under becomes `http://localhost:PORT/.tuohi/<token>`, so `rewriteAppURL`, `resolveURL`, the origin gate and every engine need no change: the origin is still `http://localhost:PORT`.
- **Token becomes a cookie.** A request for `/.tuohi/<token>/<rest>` is compared in constant time. When it matches, the server answers with a 302 to `/<rest>` (query kept), with `Set-Cookie: tuohi-PORT=<token>; Path=/; HttpOnly; SameSite=Lax` and `Cache-Control: no-store`. A wrong token gets 403 and no cookie, so the cookie is only ever set for a request that presented the token. Every other request needs the cookie, or it gets 403 before the resolver runs.
- **Host check first.** Anything other than `localhost:PORT` (any case) or `127.0.0.1:PORT` gets 421, including a missing Host header.
- **Lifetime.** Remove `loopbackIdleTimeout`, `keepAlive` and the timer. The server lives until `releaseLoopback` in `Destroy`. The Windows test-only `loadHTML` relied on the timer, so it keeps its server in an atomic pointer and stops it on the next `loadHTML` or on `Destroy`.
- **Docs.** `App.FS` and `App.HTTP`, the README, architecture.md, and every comment that said "temporary", "idle timeout" or "stopped once the first load finishes".

### Alternatives considered

- **Token in a query parameter, or in every URL.** A query parameter would need `rewriteAppURL` to add it and every relative URL the page builds would lose it. A path prefix plus a cookie keeps the page's own URLs clean, which `TestLoopbackLateFetch` checks through `location.pathname`.
- **Token only, no cookie.** Absolute paths such as `/data.txt` would fail, so every app would have to use relative URLs.
- **`SameSite=Strict`.** It failed `TestOriginGate`. When the page before the app's came from another site, WebKit treats the redirected request as cross-site and withholds a Strict cookie, so the app page got 403. `Lax` sends the cookie only on a top-level GET navigation from another site. That loads the app into the view and shows the other page nothing. Subresources and fetches started by another site still carry no cookie.
- **Keep the idle timeout and document the limit.** The baseline run of `TestLoopbackLateFetch` failed on current main: `path=/index.html data.txt=error /data.txt=error`. A page's origin disappearing 3 s after load breaks lazy imports and route changes, so the server now lives as long as its view.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T22:10:08Z

Implemented on `fix/loopback-guard`, per the plan.

### Verification

- **Baseline.** `TestLoopbackLateFetch` on main reported `path=/index.html data.txt=error /data.txt=error`: the server was gone 4.5 s after load.
- **After.** `just test-gui` passes on webkitgtk-6.0 and webkit2gtk-4.1 with `TUOHI_REQUIRE_GUI=1`, including `TestLoopbackLateFetch` (`path=/index.html data.txt=200:late data /data.txt=200:late data`) and `TestOriginGate`.
- **`SameSite=Strict` was tried first** and failed `TestOriginGate`. A logged run showed `GET /.tuohi/<token>/page cookies=0` followed by `GET /page cookies=0`: the redirect set the cookie, but WebKit withheld it from the redirected request because the navigation started on another site's page. `Lax` fixed it; the reasoning is in the plan.
- **`TestLoopbackGuard`** covers the redirect and cookie, both spellings of the origin, and ten refusals: no cookie, a wrong cookie, a cookie for another port, a wrong or empty token, a rebound Host with and without the token, another port, no port, and no Host. None of them sets a cookie or reaches the resolver. Three mutations, each checked on its own, make it fail: `ownHost` always true, `hasCookie` accepting any cookie, and `validToken` skipped.
- **`just ci`** passes (vet, gofmt, the import and JS checks, headless tests, cross builds, `git ticket check`). `go vet` passes for GOOS=windows and GOOS=darwin. golangci-lint is not installed locally, so Forgejo CI runs it.

### Not tested here

macOS WKWebView and Windows WebView2 run the new scenario only on GitHub after the merge, as the owner decided. The cookie handling there is standard, but a failure would show up first on GitHub `main`.
