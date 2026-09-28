---
schema: 4
id: TKT-01M3MV9PMXS3CMR71R186MW9BJ
title: Let a consumer title a view's window
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-linux
  - area/engine-darwin
  - area/engine-windows
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T20:28:58Z
updated_at: 2026-09-28T20:29:09Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/c04aed4f
  name: ""
extensions: {}
---

## Description

### What

A view has no window title that a consumer can set, and on Linux the window does not follow the page's `document.title` either. git-ticket-canvas wants its window titled after the store it has open, and tuohi gives it no way to do that.

### Found by

git-ticket-canvas, working TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) against `v0.1.0-alpha.1`. That ticket asks that "the window title names the open store", and the canvas cannot meet it. Checked again on `main` at 70750bf: `View` has no title field, and no engine sets a window title outside the tray and notify packages.

### Things to settle

- **Go or page.** Either a `View.Title` field set at creation plus a setter that marshals to the UI thread, or a view that follows the page's `document.title`, or both, with Go winning when set. Following the page suits a consumer serving its own UI, as the canvas does, since the page already knows what it shows. A Go field suits a page that is not the consumer's.
- **Per engine.**
  - WebKitGTK exposes `notify::title` on the web view, and GTK sets the title with `gtk_window_set_title`.
  - WebView2 has `DocumentTitleChanged`. Its vtable slots are already declared in `lib_windows.go` and unused.
  - WKWebView's `title` is observable by KVO, and the window takes `setTitle:`.
- **Frameless windows.** There is no title bar, but the title still names the window in the taskbar, the window switcher, and accessibility tools, so set it anyway.
- **Untrusted pages.** A page on an origin the view does not trust, such as `about:blank`, should not rename the application's window.

## Acceptance criteria

- [ ] A consumer can set a view's window title on all three engines
- [ ] Whether the title follows the page's document.title is decided and recorded, and an untrusted page cannot rename the window
