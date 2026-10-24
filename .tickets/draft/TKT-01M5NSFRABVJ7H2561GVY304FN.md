---
schema: 4
id: TKT-01M5NSFRABVJ7H2561GVY304FN
title: Use the documented IID for ICoreWebView2Controller2
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-windows
  - area/ffi
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-24T01:47:58Z
updated_at: 2026-10-24T01:47:58Z
created_by:
  id: agent:claude-code/tuohi-clipboard-image
  name: ""
updated_by:
  id: agent:claude-code/tuohi-clipboard-image
  name: ""
extensions: {}
---

## Description

### What

`iidController2` in lib_windows.go is `F0EC8882-7EC5-4118-A7C8-59540CFF17ED`.
The documented IID of `ICoreWebView2Controller2` is
`C979903E-D4CA-4228-92EB-47EE3FA96EAB`, which Microsoft's reference and
independent bindings (zzl/go-webview2, PureBasic's WebViewGadget) agree on.
So QueryInterface always answers E_NOINTERFACE, and
`applyDefaultBackgroundColor` never sets the background.

### Evidence

Kuiske's native Windows spike on 2026-10-23 (Kuiske ticket
TKT-01M5NCYPZ8SS7DGZEG3MG9XGQAP): QueryInterface returned E_NOINTERFACE
(0x80004002) on WebView2 Runtime 155.0.4283.62, so tuohi logged "background
color unavailable". The runtime is current, so the interface exists; the IID
is what is wrong.

### Fix

Correct the IID, and add a native Windows regression that asserts the
controller answers it, rather than logging and carrying on. Land it as a small
PR: Windows is tested on GitHub after the merge. Check what else the
background colour change makes visible, since it has never applied before.
