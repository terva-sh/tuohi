---
schema: 4
id: TKT-01M5NSFRABVJ7H2561GVY304FN
title: Use the documented IID for ICoreWebView2Controller2
type: bug
status: done
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
updated_at: 2026-10-24T03:15:56Z
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

## Implementation plan

### Findings

Checked every IID in lib_windows.go against Microsoft's WebView2.h (SDK
1.0.4314.35, from the Microsoft.Web.WebView2 NuGet package). All match except
ICoreWebView2Controller2, which is `c979903e-d4ca-4228-92eb-47ee3fa96eab`.

The colour was also wrong, which the wrong IID had hidden. WebView2.h defines
COREWEBVIEW2_COLOR as `{BYTE A; BYTE R; BYTE G; BYTE B;}`, which travels by
value as a little-endian uint32 with A in the low byte. tuohi's "opaque
white" `0x000000FF` is A=0xFF and R=G=B=0, opaque black. Fixing the IID
alone would have given every framed window a black background.

### Change

- Correct `iidController2`.
- `defaultBackgroundColor(frameless)`: `0xFFFFFFFF` for opaque white on a
  framed window, `0` for transparent on a frameless one.
- Add `GetDefaultBackgroundColor`, for the test.
- Tests: a packing unit test, and a native scenario that QueryInterfaces a
  live controller for Controller2 and reads back the colour of a framed and a
  frameless view. Windows runs these on GitHub after the merge.

### Visible effect

The documented behaviour now holds on Windows. A frameless View (the
default) was showing WebView2's white background and is now transparent where
the page does not paint, as on WebKitGTK and WKWebView. A page with no
background of its own will show the desktop through it. The patch notes must
say so.

## Notes

**agent:claude-code/tuohi-clipboard-image** at 2026-10-24T03:15:47Z

Merged in #104 as 2729be2. GitHub native run 40056863061 on 2729be2 passed every job. On the Windows runner, under TUOHI_REQUIRE_GUI=1, TestDefaultBackgroundColorPacking and TestDefaultBackgroundColor passed, and applyDefaultBackgroundColor logged hr=0x0.

## Summary

Fixed in #104 (2729be2): the ICoreWebView2Controller2 IID now matches WebView2.h, and the colour packing is fixed (opaque white is 0xFFFFFFFF; the old 0x000000FF was black). Framed windows get opaque white and frameless windows a transparent background, as View.Frame documents. Verified on GitHub's Windows runner in run 40056863061. Ships in v0.3.1.
