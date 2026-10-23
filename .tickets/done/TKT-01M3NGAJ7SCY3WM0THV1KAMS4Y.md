---
schema: 4
id: TKT-01M3NGAJ7SCY3WM0THV1KAMS4Y
title: Keep WebView2 from requesting a navigation NavigationStarting cancels
type: bug
status: done
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
updated_at: 2026-10-23T04:39:20Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
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

- [x] The cause of the request is found and recorded
- [x] A server redirect to an untrusted origin is not requested on Windows, or the limit is documented with the reason

## Implementation plan

Resolve the documented-limit branch of acceptance: Microsoft ICoreWebView2NavigationStartingEventArgs.get_Cancel explicitly permits GET requests while the host responds. Verify the interface has no NavigationStarting deferral and assess WebResourceRequested against existing cross-origin iframe/subresource support before proposing a network block. Correct false pre-request comments and public Origins documentation, cite the vendor contract and native historical/current evidence, and retain the existing redirect-retains-page, outside-handoff and trusted-frame regressions. No blanket all-origin resource filter, no weakening bridge origin checks, and no inferred network sandbox guarantee. Re-run cgo-free validation and relevant GUI regressions; native Windows evidence remains clearly identified.

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T04:39:19Z

Resolved the acceptance-criterion documented-limit outcome. Microsoft ICoreWebView2NavigationStartingEventArgs.get_Cancel explicitly states: For performance reasons, GET HTTP requests may happen, while the host is responding. Source: https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2navigationstartingeventargs#get_cancel . The supported interface has no GetDeferral; WebResourceRequested has a deferral but is a different surface. A blanket foreign-origin resource/document filter would alter supported cross-origin frame/asset behavior, and the current filter API has documented iframe limitations. No such network policy is silently added to Origins. The API permits the GET/event overlap independent of loopback spelling or runtime version; no undocumented engine race fix is inferred. Corrected two false pre-request comments in lib_windows.go, documented the network versus document/bridge boundary on View.Origins and in docs/navigation-security.md, and linked architecture guidance. Preserve current bridge and navigation behavior.

**agent:codex/t3-b903096c** at 2026-10-23T04:39:19Z

Validation: CGO_ENABLED=0 just ci passes both modules/every prescribed cross target and strict ticket validation. Existing native Windows regressions at unchanged runtime source 68fc3ac passed in run 39873829324, job 120154764292: TestNavigationPolicy (reachable outside redirect retains trusted page, external handoff, frame still runs), TestOutsideLinksNotRequested (early intercepted links/script requests remain zero; its redirect host is unresolvable), TestOriginGate and TestFrameGate. App.OpenExternal captures handoff without opening a browser. This change is documentation/comments only, so no duplicate tests or new runtime assertion that contradicts the vendor contract was added. Actual zero-traffic guarantee for all refused navigations is not claimed.

## Summary

Documented the vendor-supported GET/event overlap and the limits of native cancellation. View.Origins governs document/bridge trust, not network egress. Corrected inaccurate pre-request comments, added sourced navigation-security guidance and explained why a blanket WebResourceRequested filter/imaginary NavigationStarting deferral was rejected. Existing native Windows redirect, intercept and origin/frame regressions pass at 68fc3ac; full cgo-free CI/cross builds pass. Runtime behavior is unchanged. This satisfies the explicit document-the-limit alternative; no network-request prevention fix is claimed.
