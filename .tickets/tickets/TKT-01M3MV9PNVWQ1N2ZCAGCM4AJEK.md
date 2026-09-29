---
schema: 4
id: TKT-01M3MV9PNVWQ1N2ZCAGCM4AJEK
title: Hand outside links to the browser before requesting them on Linux
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/linux-outside-links
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-72958710
  commit: 5d9a4882234c7ee023483714d75736f71278de22
  session: null
  claimed_at: 2026-09-28T23:57:39Z
  expires_at: null
archive: null
created_at: 2026-09-28T20:28:58Z
updated_at: 2026-09-29T02:28:19Z
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

## Implementation plan

### Approach (the owner's choice, 2026-09-29: page intercept plus load-failed)

1. **Page intercept** (`initOutsideLinks` in bind_gen.go), appended to the bridge only where `interceptOutsideLinks` is true. That is WebKitGTK only: WebView2 and WKWebView already decide before any request. It runs only in a document the bridge was installed in, a trusted top-level one. It catches three kinds of navigation that would leave the trusted origins, cancels each, and posts `__appkitOpenExternal`:
   - a plain left click on `a[href]` or `area[href]` that opens in the same window and is not a download;
   - a GET form submit;
   - the Navigation API's `navigate` event, for push and replace navigations, not hash changes or form posts.

   Its listeners are on `window` in the bubble phase, so a page that handled the click itself keeps it.
2. **Go** (`webview.openOutside`) does not take the page's word for the URL. It opens only an http or https URL for which the navigation policy returns `navExternal`.
3. **`load-failed`** (`webview.loadFailed`) handles a main-frame load that failed before commit, when the URL is untrusted http(s) and the policy hands it to the system. It hands the URL over and suppresses WebKit's error page. It skips policy errors, including the response-time ignore, and cancelled loads.
4. **Tests:**
   - `TestOutsideLinksNotRequested`, a GUI scenario shared by all engines, runs four steps: a link, a `location.href` assignment, an unresolvable link, and a trusted URL that redirects to an unresolvable host. The outside server must receive no request.
   - `TestOutsideLinksScript` runs the intercept's rules in node.

### Alternatives rejected

- **Cancelling every untrusted http(s) navigation action.** It would also cancel, or hand to the browser, cross-origin iframes, and the action cannot tell them apart (see the measurements note).
- **The Navigation API alone.** On WebKitGTK 6.0 it does not fire for a link click.
- **Stopping the load at `load-changed` STARTED.** The request is already on its way by then.
- **Capture-phase listeners.** They would take clicks that a page's own router handles.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T00:01:27Z

### Measured on both WebKitGTK stacks (2.52.6, Debian 13), 2026-09-29

I ran a throwaway probe, not committed; its source is kept in scratch. A temporary hook in `decidePolicy` logged every decide-policy signal. The page, on loopback origin A, embedded an iframe from origin B, changed that iframe's `src`, clicked a top-level link to B, and assigned `location.href` to B.

**The navigation action cannot tell a frame from the main frame.** Iframe loads arrive as `NAVIGATION_ACTION` with navigation type OTHER (5), the same as the main frame's own `Navigate`. The only difference is that no `RESPONSE` decision follows for a frame. Neither stack exports anything that names the target frame: `webkit_navigation_action_get_frame_name` reports a link's `target` attribute and nothing else. Cancelling every untrusted http(s) action would therefore also cancel cross-origin iframes, or hand them to the browser.

**The bug reproduces as the ticket describes.** On both stacks, the top-level link click (LINK_CLICKED, 0) reached origin B's server (`GET /top`) before the `RESPONSE` decision handed the URL to `openExternal`.

**The Navigation API does not cover it on both stacks.** `navigation` exists on both, and the `NavigationAPI` feature defaults to on for both.

| Navigation | WebKitGTK 4.1 (GTK3) | WebKitGTK 6.0 (GTK4) |
|---|---|---|
| A link click (`a.click()`) | `navigate` fires, `cancelable=true`, and `preventDefault` stops it before any request | `navigate` never fires, 3 runs of 3, and the request is sent |
| A `location.href` assignment | fires, prevented, no request | fires, prevented, no request |

### Options this leaves

1. **Page-side interception in the bridge script,** which runs in the main frame of trusted pages only. A capture-phase `click` listener on `a[href]`, and a `submit` listener, prevent a main-frame navigation to an untrusted http(s) origin and post an internal message asking Go to hand the URL to the browser. The Navigation API's `navigate` event covers scripted navigations where it fires. What stays at the response, as today: server redirects, `<meta http-equiv=refresh>`, and a link click on 6.0 that no listener catches.
2. **A `load-failed` fallback** for the unresolvable-host criterion. `load-failed` is main-frame only. When the failing URI is untrusted http(s), hand it to the browser and suppress WebKit's error page. This fixes "a link that does nothing" on its own, without closing the request leak.
3. **Accept the response-time decision** and document that Linux requests an outside page before handing it over. Macs and Windows do not.

**agent:claude-code/t3code-72958710** at 2026-09-29T02:28:19Z

### Evidence (Linux, both stacks)

- **`TestOutsideLinksNotRequested`** passes on both stacks.
- **Negative controls:**
  - With `interceptOutsideLinks = false` it reports `requested=2`: the link and the assignment reached the outside server.
  - With the `load-failed` handler disconnected it reports `deadredirect=none`.
  - The unresolvable *link* is caught by the page intercept before any request, so it passes either way. The redirect step is the one that needs `load-failed`.
- **`TestOutsideLinksScript`** covers 19 cases:
  - handed to Go: an outside link, `target=_top`, a GET form, and `navigate` push and replace;
  - left alone: trusted links and forms, modifier clicks, middle clicks, `target=_blank`, `download`, links the page already handled, `mailto:`, POST forms, traversals, hash changes, and navigations that cannot be cancelled.

  With the modifier and already-handled checks removed from the script, it fails on exactly those two cases.
- **`TestBridgeGate`** runs the platform's real bridge in node, whose stub window has no `addEventListener`. The intercept now returns early in that case.
- **`just ci`**, **`just test-gui`** on both stacks, and **golangci-lint** on three GOOS all pass.

Not verified here: the new scenario on macOS and Windows. They should hand everything over natively, before any request. Whether WKWebView sees a server redirect that fails before any response is the open question, and GitHub CI will answer it.
