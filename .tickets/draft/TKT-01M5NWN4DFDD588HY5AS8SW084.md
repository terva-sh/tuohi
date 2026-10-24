---
schema: 4
id: TKT-01M5NWN4DFDD588HY5AS8SW084
title: Drop context-menu entries a tuohi view cannot honour
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
  - area/api
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
created_at: 2026-10-24T02:43:20Z
updated_at: 2026-10-24T02:43:20Z
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

Remove the built-in context-menu entries that cannot work in a tuohi view,
or let an app filter or turn off the default menu per view, and document
which entries remain.

### Why

Reported by Kuiske on 2026-10-24 (Kuiske ticket
TKT-01M5NWJ78RGJGZBZEN0PDZ8XCT), at the owner's request. On Linux tuohi
leaves WebKitGTK's default context menu untouched. Under WSLg, WebKitGTK
2.52.6, right-clicking an image the page shows from a `blob:` URL offers:

- "Open Image in New Window": does nothing and stays highlighted, because
  tuohi never opens a second view and refuses the navigation.
- "Save Image As": does nothing, because no download handler is set.
- "Copy Image Address": copies an in-memory `blob:` URL, useless outside the
  page.
- "Copy Image": copies the image, but the clipboard also offers the
  `blob:http://127.0.0.1:…` address as text.

Links very likely have the matching "Open Link in New Window" and "Download
Linked File". Every tuohi app that shows local content gets these entries.
Kuiske will show its own menu on its images, so this protects other apps and
Kuiske's other surfaces.

### Where

- WebKitGTK, GTK 3 and GTK 4: the WebKitWebView `context-menu` signal can
  remove items by `WebKitContextMenuAction` (open in new window, download,
  copy link or image address, ...) before the menu shows. No handler is
  connected today.
- WebView2: tuohi sets `AreDefaultContextMenusEnabled(true)`
  (lib_windows.go). `ICoreWebView2_11`'s `ContextMenuRequested` can edit the
  items.
- WKWebView: has no public per-item API. A subclass overriding
  `willOpenMenu:withEvent:` can filter the NSMenu.

### Things to settle

- Remove a fixed set by default, or add a per-view option (off, filtered,
  full), or both. Changing the default is a behaviour change for consumers.
- Whether "Copy Image" should stop also offering the `blob:` address.
