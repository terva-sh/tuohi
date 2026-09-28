---
schema: 4
id: TKT-01M3MV9PNVWQ1N2ZCAGCM4AJEK
title: Hand outside links to the browser before requesting them on Linux
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
  - security
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
  - ref: code:lib_unix.go
    path: lib_unix.go
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T20:28:58Z
updated_at: 2026-09-28T21:34:09Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

On Linux, tuohi decides a top-level http or https navigation when its response arrives, not when the navigation starts. So a link that leaves the trusted origins is first requested by the application's own web view, and only once the far end answers is the URL handed to the system browser. A host that does not answer is never handed over at all.

`decidePolicy` in `lib_unix.go` returns early for every scheme in `responseSchemes`, http and https included, and judges the page at `policyResponse`. Its comment gives the reason: WebKitGTK's navigation actions do not say whether they are for the main frame, so a top-level page is judged by its main resource's response. macOS decides at `decidePolicyForNavigationAction`, and Windows at `NavigationStarting`, so both decide before anything is requested.

### Found by

git-ticket-canvas, working TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) against `v0.1.0-alpha.1`. Its GUI test clicked a link to `https://example.invalid/leave` and waited 15 s, and a fake `xdg-open` on `PATH` was never run. Pointing the link at a second loopback server that answers made the same test pass. The code on `main` at 70750bf is unchanged.

### Why it matters

- **A link that does nothing.** A consumer's outside link to a site that is down, blocked, or offline does nothing, and the page gets no signal. What the view shows meanwhile was not checked: it may keep the application's page, or load WebKit's error page in its place.
- **A request the user did not see go out.** The application's web view contacts an origin the view does not trust, with that view's own cookie store and user agent, before the person's browser does. The policy's promise is that such pages are not shown. It does not currently promise that they are not fetched.
- **Engines disagree.** The same click behaves differently on Linux than on the other two engines.

### Things to settle

- Whether WebKitGTK offers any way to tell a main-frame navigation action from a frame's, so that a page can be judged before it is requested. This was not researched. Measure it on both WebKitGTK stacks before relying on it.
- If it cannot, whether cancelling at the action for every non-trusted http(s) URL is acceptable. That would also cancel frames navigating to other origins, which the policy currently leaves alone.
- A GUI scenario for each engine that clicks a link to a host that does not resolve and expects the hand-off.

### Related

TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings) introduced the navigation policy.

## Acceptance criteria

- [ ] On Linux, a link to a non-trusted http or https origin is handed to the system browser without the view requesting it
- [ ] A link to a host that does not resolve still reaches the system browser on every engine
- [ ] A GUI scenario on each engine covers the unresolvable-host case
