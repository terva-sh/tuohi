---
schema: 4
id: TKT-01M5NKRPZ0BJQA5004WPJ34AB8
title: Read clipboard images on Windows
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-windows
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M5NJ6Q60PV4XPBTP9P7ZZ7B0
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-24T00:08:00Z
updated_at: 2026-10-24T00:08:05Z
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

Implement `clipboard.PasteImage` on Windows, which returns `ErrUnsupported`
since TKT-01M5NJ6Q60PV4XPBTP9P7ZZ7B0 (Read an image from the system
clipboard) added it on GTK.

### How

Open the clipboard without an owner, as Paste does. Prefer the registered
"PNG" format (RegisterClipboardFormatW), which Chromium and Firefox put
there, then CF_DIBV5 (17), then CF_DIB (8). Windows synthesizes the DIB
formats from each other and from CF_BITMAP. Check GlobalSize against
MaxBytes before copying the locked block. PNG goes to `encodeImage`, and a
DIB to `decodeDIB` (clipboard/dib.go), which already reads headerless
bitmaps with V5 headers and bitfields, then `encodePNG`. No format is
`ErrNoImage`.

Watch for: a CF_DIBV5 written with BI_BITFIELDS whose masks some programs
repeat after the header, and premultiplied alpha in 32-bit CF_DIB.

### Tests

A windows GUI scenario that sets PNG only, then CF_DIB only, with
SetClipboardData and reads both back. It runs on GitHub's Windows runner
after merge, so land it as its own small pull request.

## Acceptance criteria

- [ ] PasteImage reads PNG and the platform's native image format, within ImageLimits
- [ ] A GUI scenario on the platform's runner puts each format on the real clipboard and reads it back
