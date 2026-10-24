---
schema: 4
id: TKT-01M5NSFRBXS3MA76QWQB4GJ95D
title: Let a caller cancel a clipboard read
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/api
  - area/engine-linux
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

Let a caller cancel a clipboard read. `Paste` and `PasteImage` wait for the
clipboard owner with no timeout of their own, and GTK does not bound every
transfer, so an owner that never answers keeps the call waiting. v0.3.0
documents this.

### Why

Kuiske calls `PasteImage` from a goroutine with its own 10 s deadline, and on
a timeout abandons a goroutine that may never return. It suggested a
context-aware variant (`PasteImageContext(ctx, limits)` or similar) and said
it does not block the release.

### Things to settle

- GTK 4 can be cancelled: pass a GCancellable to `gdk_clipboard_read_async`
  and `g_input_stream_read_bytes_async`, and `g_cancellable_cancel` it (thread
  safe) when the context ends.
- GTK 3's `gtk_clipboard_wait_for_*` cannot be cancelled. The async
  `gtk_clipboard_request_*` calls with a nested loop could stop waiting, but
  GTK keeps the transfer and calls back later.
- Whether text `Paste` gets the same variant.
