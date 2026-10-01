---
schema: 4
id: TKT-01M3R23M9PWBF3VB2QD76SRSHH
title: A bare app:// URL loads its page without the bridge
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/bridge
  - area/api
assignees: []
milestone: v0.1.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-30T02:25:43Z
updated_at: 2026-10-01T05:57:25Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

A view navigated to a bare `app://`, with no host, loads its App.FS page with no bridge on Linux. The page's bindings and events are missing, and nothing reports why. The documented form, `app://app/index.html`, works.

It was found by the showcase, which navigated to `app://#selftest` and whose self test had silently stopped reaching Go. Nothing ran that self test until TKT-01M3R1V2 ("Move the demos into examples/ as tuohi's reference implementation"). Over `-http` the same page passed 23/23, and with `app://app/index.html` it passes on both WebKitGTK stacks.

### Cause, as far as it is known

`originOf("app://")` is `app:`, because the URL has no host. The page the engine then shows reports a URL whose origin is not `app:`, so the trust check withholds the bridge. It most likely started with the bridge trust work (TKT-01M3HWWRT7X1RZZYY6KFEP0ERE), since the demo's URL predates it. Which URL WebKitGTK reports for the page has not been checked, and neither have macOS (which rewrites `app://` onto the loopback base) or Windows (which maps it onto its https vhost).

### Options

- Normalize a hostless `app://` to `app://app/` wherever `resolveURL` handles the uniform origin, so it is trusted as what it loads.
- Refuse it, with an error from Navigate or a documented rule, since the documentation never offered it.

Either way a GUI scenario should navigate to a bare `app://` on every engine and check that the bridge is there.

## Acceptance criteria

- [x] A view navigated to a bare app://, natively and under App.HTTP, loads the App.FS root with its bindings on every engine
- [x] A query or fragment on a bare app:// URL is kept, so app://#route reaches app://app/#route

## Implementation plan

Add normalizeAppURL in engine.go, next to canonicalNavigateURL. An app:// URL with an empty authority (`app://`, `app://#x`, `app://?q`, `app:///path`) is given the host `app`, and the path `/` when it has none, so `app://#selftest` becomes `app://app/#selftest`. Every other URL, including an opaque `app:x`, is unchanged.

Each engine's Navigate calls it first, before resolveURL and the Windows scheme rewrite. Every load (View.URL, View.Navigate, a new-window redirect) then trusts the origin it really loads. Doing it once in View.Navigate would miss the View.URL path in App.Show and handleNewWindow, which call the engine directly.

Tests:
- a unit table for normalizeAppURL;
- a GUI scenario, `bareAppURLScenario`, registered on all three engines. It shows App.FS views at `app://#bare`, natively and under App.HTTP, and checks that the page calls a binding and keeps its fragment.
- The View.Navigate doc and the App.FS doc say a bare app:// is the root of the `app` host.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: normalize, and it blocks v0.1.0

The owner decided, 2026-10-01: normalize a hostless `app://` to `app://app/` where `resolveURL` handles the uniform origin, so the page is trusted as what it loads. A fragment or query on the bare form carries over, so `app://#selftest` becomes `app://app/#selftest`.

Refusing it with an error lost because the form already loads a page today and the showcase used it, so consumers may use it too. Refusing would turn a silent failure into a breaking one. A GUI scenario navigates to a bare `app://` on every engine and checks the bridge.

The ticket is linked as a dependency of TKT-01M3HWWRZ (Release tuohi v0.1.0).

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:32:11Z

### Implemented on fix/bare-app-url

`normalizeAppURL` in engine.go gives a hostless app:// URL the host `app`. Each engine's Navigate calls it first. It is not called once in View.Navigate, because App.Show's View.URL and handleNewWindow call the engine directly.

`bareAppURLScenario` (bare_app_url_test.go), run on all three engines, shows App.FS views at `app://#bare`, natively and under App.HTTP. Without the fix the native case reported "no call" on Linux, and the HTTP case already passed, because rewriteAppURL drops the host. Writing the scenario exposed TKT-01M3TZ0A (Serve app:// from the requesting view on WebKitGTK, not the first one), which is fixed in the same PR, because without that fix the native case cannot pass.

Verified on Linux, on webkitgtk-6.0 and webkit2gtk-4.1: `just ci` and the GUI suite are green. The criteria are ticked on the Linux evidence and the shared code path. macOS and Windows run the same scenario on GitHub after the merge. If either fails there, reopen this ticket.

## Summary

Landed in PR #58 (merge 6c337b1). Each engine's Navigate reads a hostless app:// URL as the root of the app host, keeping its query and fragment, via normalizeAppURL in engine.go. bareAppURLScenario covers it natively and under App.HTTP. It passed on Linux (both WebKitGTK stacks) and on GitHub run 36820646707 on macOS and Windows.
