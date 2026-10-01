---
schema: 4
id: TKT-01M3R23M9PWBF3VB2QD76SRSHH
title: A bare app:// URL loads its page without the bridge
type: bug
status: ready
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
updated_at: 2026-10-01T05:14:22Z
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

- [ ] A view navigated to app:// either reaches its bindings or is refused with a documented error, on every engine

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: normalize, and it blocks v0.1.0

The owner decided, 2026-10-01: normalize a hostless `app://` to `app://app/` where `resolveURL` handles the uniform origin, so the page is trusted as what it loads. A fragment or query on the bare form carries over, so `app://#selftest` becomes `app://app/#selftest`.

Refusing it with an error lost because the form already loads a page today and the showcase used it, so consumers may use it too. Refusing would turn a silent failure into a breaking one. A GUI scenario navigates to a bare `app://` on every engine and checks the bridge.

The ticket is linked as a dependency of TKT-01M3HWWRZ (Release tuohi v0.1.0).
