---
schema: 4
id: TKT-01M3TZ0AWR4PHN56MT2H9Z39RE
title: Serve app:// from the requesting view on WebKitGTK, not the first one
type: bug
status: done
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
created_at: 2026-10-01T05:29:12Z
updated_at: 2026-10-01T05:57:25Z
created_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

On Linux, without App.HTTP, every app:// request in the process is answered by the first view that registered the `app` scheme. Once that view is closed, app:// requests from every later view are never finished, so the page stays blank and nothing reports why.

### Cause

`registerSchemes` in lib_unix.go registers the scheme on the view's WebKitWebContext with that view's engine id as user data. Every view made by `webkit_web_view_new` shares the default context, and WebKit keeps only the first registration ("Cannot register URI scheme app more than once"), so the callback always resolves to the first view. When that view is gone, `lookupEngine` returns nil and the callback returns without calling `webkit_uri_scheme_request_finish` or `_finish_error`. Every view also makes a new purego callback, and the number of those is limited for the life of the process.

### Found by

The GUI scenario for TKT-01M3R23M9PWBF3VB2QD76SRSHH (A bare app:// URL loads its page without the bridge). Its App.FS view was answered by the earlier `loopbackLateScenario` view's handler and filesystem. The CRITICAL line appears in every Linux GUI run on main.

### Fix

Register the scheme once per web context, with one callback for the process. The callback finds the view that made the request through `webkit_uri_scheme_request_get_web_view` and answers from that view's own `serve`. A request whose view is gone finishes with an error.

## Acceptance criteria

- [x] A view shown after the first App.FS view closed loads its app:// page natively
- [x] Each app:// request is answered from the filesystem of the view that made it
- [x] The scheme and its callback are registered once per web context

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:29:25Z

Promoted and claimed by the agent running the owner's autonomous v0.1.0 burndown (2026-10-01). The TKT-01M3R23M GUI scenario cannot pass natively without this fix, so both land in one PR. It is linked as a release dependency because it leaves consumers on Linux with a blank window after reopening. The owner can unlink it.

## Summary

Landed in PR #58 (merge 6c337b1). WebKitGTK registers the app scheme once per web context with one callback. The callback answers from the serve of the view that made the request, found through webkit_uri_scheme_request_get_web_view, and finishes with an error when that view is gone. bareAppURLScenario's native view runs after other App.FS views have closed, and passes on both stacks. The 'Cannot register URI scheme app more than once' warning is gone from the GUI runs.
