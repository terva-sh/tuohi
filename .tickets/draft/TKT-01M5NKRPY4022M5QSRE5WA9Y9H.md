---
schema: 4
id: TKT-01M5NKRPY4022M5QSRE5WA9Y9H
title: Read clipboard images on macOS
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-darwin
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

Implement `clipboard.PasteImage` on macOS, which returns `ErrUnsupported`
since TKT-01M5NJ6Q60PV4XPBTP9P7ZZ7B0 (Read an image from the system
clipboard) added it on GTK.

### How

On the main thread, as the text functions do: ask the general NSPasteboard
for `availableTypeFromArray:` of NSPasteboardTypePNG and NSPasteboardTypeTIFF
(resolved from AppKit like NSPasteboardTypeString). Check `[data length]`
against MaxBytes before copying. PNG goes to `encodeImage` as image/png.
TIFF goes through `NSBitmapImageRep imageRepWithData:`: check
`pixelsWide` × `pixelsHigh` against MaxPixels, then
`representationUsingType:NSBitmapImageFileTypePNG properties:` and
`encodeImage`. No type is `ErrNoImage`.

### Tests

A darwin GUI scenario that puts PNG only, then TIFF only, on the general
pasteboard with `setData:forType:` and reads both back. It runs on GitHub's
macOS runner after merge, so land it as its own small pull request.

## Acceptance criteria

- [ ] PasteImage reads PNG and the platform's native image format, within ImageLimits
- [ ] A GUI scenario on the platform's runner puts each format on the real clipboard and reads it back
