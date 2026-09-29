---
schema: 4
id: TKT-01M3NGAJ7SCY3WM0THV1KAMS4Y
title: Keep WebView2 from requesting a navigation NavigationStarting cancels
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-windows
  - security
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-29T02:36:27Z
updated_at: 2026-09-29T02:36:27Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

On Windows, a top-level navigation that `NavigationStarting` cancels has still reached the server as its ordinary request. A link click and a `location.href` assignment from a trusted loopback page to a second loopback origin were each handed to the system, and the second server still received `GET /link` and `GET /assign`. Neither request carried `Sec-Purpose` or `Purpose`, so they were not prefetches.

This contradicts what the code and the architecture doc assumed until TKT-01M3MV9PNVWQ1N2ZCAGCM4AJEK: that WebView2 decides "before any request is sent".

### Found by

`TestOutsideLinksNotRequested` in GitHub runs 36512765547 and 36513098161, on runtime 153.0.4234.48. The WEBVIEW2_DEBUG trace shows NavigationStarting for each URL, then NavigationCompleted, and no ContentLoading.

### What already covers it

TKT-01M3MV9PNV turned on the bridge's outside-link intercept on Windows too. Link clicks, GET forms, and scripted navigations from a trusted page are therefore handed over before WebView2 starts them. What is still requested before it is handed over: a server redirect to an outside host, a `<meta http-equiv=refresh>`, and any navigation the page starts that the intercept does not see.

### Things to settle

- Whether the request goes out in parallel with the NavigationStarting event, or only for some origins, such as same-site loopback ports, or only on some runtime versions. Try a cross-site target.
- Whether `NavigationStarting`'s deferral, `GetDeferral`, holds the request until the host decides. If it does, the policy could decide under a deferral.
- Whether `ICoreWebView2_2::add_WebResourceRequested` with a document filter could refuse the request itself.

## Acceptance criteria

- [ ] The cause of the request is found and recorded
- [ ] A server redirect to an untrusted origin is not requested on Windows, or the limit is documented with the reason
