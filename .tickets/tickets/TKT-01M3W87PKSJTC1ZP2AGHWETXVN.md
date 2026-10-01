---
schema: 4
id: TKT-01M3W87PKSJTC1ZP2AGHWETXVN
title: Survive WebKitGTK 4.1 2.54 crashing on paste of an empty clipboard
type: bug
status: blocked
status_reason: WebKit bug in webkit2gtk-4.1 2.54.0; waiting for a WebKit fix. The upstream report drafted in the notes needs a bugs.webkit.org account to file. CI holds the clipboard on GTK3 meanwhile (#74).
priority: high
due_on: null
labels:
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
created_at: 2026-10-01T17:29:45Z
updated_at: 2026-10-01T19:24:17Z
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

On WebKitGTK 4.1 2.54.0, which Debian 13 shipped as an update on or before 2026-10-01, a page that calls `navigator.clipboard.readText()` while nothing owns the X11 clipboard crashes the process with SIGSEGV in `libwebkit2gtk-4.1.so.0`. The read must be allowed for the crash to happen: tuohi grants it only to a view that lists `PermissionClipboard`, and only on a user gesture.

Backtrace (gdb, symbols for GTK and GLib from debuginfod.debian.net; WebKit's own frame is unresolved):

```text
#0  ?? () from libwebkit2gtk-4.1.so.0
#1  request_targets_received_func (clipboard=..., selection_data=..., data=...) at gtk/gtkclipboard.c:1332
#2  selection_received (...) at gtk/gtkclipboard.c:960
...
#7  gtk_selection_retrieval_report (info=..., type=0x0, format=0, buffer=0x0, length=-1, ...) at gtk/gtkselection.c:3190
...
    g_main_context_iteration () from libglib-2.0.so.0
```

GTK reports that the target request failed (no owner: `type=0`, `length=-1`), and the WebKit callback dereferences null.

### Evidence

- Forgejo CI's GUI job fails on `main` since the update: PR #71 (ticket-only) and PR #73 failed there. The job installs the newest Debian packages on every run.
- Reproduced in CI's image (`golang:1.27-trixie`, `--cpus 2`) on `main` 0480469: 2 of 2 runs crash in `permissionsScenario`. The crash comes when `realClick` clicks the view that grants the clipboard, whose page then reads it.
- The same binary passes when `xclip -selection clipboard` owns the clipboard first.
- The development host has 2.52.6 and passes. WebKitGTK 6.0 (GTK4) passes in the same container.
- GitHub's Ubuntu runners are not affected yet.

### Things to settle

- Whether tuohi works around it (for example, refusing a clipboard read when the clipboard has no owner on GTK3), or only keeps CI green and reports it upstream.
- Whether it blocks v0.1.0.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T17:34:37Z

Owner's decisions, 2026-10-01: work around it in tuohi (refuse a clipboard read the engine would crash on, so the page gets NotAllowedError), keep the scenario on an empty clipboard to prove it, report it upstream to WebKit, and block v0.1.0 on it.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T17:41:13Z

### Ctrl+V crashes too, and the decision changed

The crash is not limited to `navigator.clipboard.readText()`. In CI's image with WebKitGTK 4.1 2.54.0 and nothing owning the clipboard, pressing Ctrl+V (XTest) in a focused `<input>` crashes at the same WebKit address. The control run, with the field focused and no key press, survives. No permission request is involved, so any GTK3 tuohi app crashes when a person pastes an empty clipboard.

The GTK4 stack (webkitgtk-6.0), which tuohi picks by default when installed, passes the same scenarios. So the exposure is a consumer whose machine has only webkit2gtk-4.1, or one pinned with `TUOHI_BACKEND=webkit2gtk-4.1`.

As of 2026-10-01, 2.54.0 (16 Sep 2026) is WebKitGTK's newest stable release, and no matching upstream report was found.

**Options put to the owner after this finding:**
- **Known issue, report upstream (chosen).** On GTK3 the scenario holds the clipboard, so CI checks what tuohi controls. The release notes tell GTK3 users about 2.54.0.
- **Keep the clipboard owned (rejected).** tuohi would claim an ownerless clipboard with empty text, re-claiming on owner-change. It changes desktop-wide clipboard state, can feed empty entries to clipboard managers, and leaves the PRIMARY selection and middle-click unexamined.
- **Refuse the clipboard permission when empty (rejected).** This was the first decision, made before the Ctrl+V finding. It covers the scripted read and not paste.

**Owner's decision, 2026-10-01:** known issue, not a v0.1.0 blocker. The ticket stays open until WebKit ships a fix, then the GTK3 clipboard hold in `realClick` (lib_unix_test.go) comes out.

### Upstream report (draft, not yet filed)

The report has to be filed by someone with a bugs.webkit.org account. Component WebKitGTK, title: "[GTK3] Crash in clipboard targets callback when pasting with no clipboard owner (2.54.0)". Body:
- Steps: on X11 with no clipboard owner (for example xvfb with no clipboard manager), focus an input in a WebKitWebView (webkit2gtk-4.1 2.54.0, GTK 3.24.49), and press Ctrl+V or call `navigator.clipboard.readText()` on a user gesture.
- Result: SIGSEGV in libwebkit2gtk-4.1 called from gtkclipboard.c:1332 `request_targets_received_func`, after `gtk_selection_retrieval_report(type=0, format=0, buffer=NULL, length=-1)`.
- 2.52.6 and webkitgtk-6.0 2.54.0 do not crash.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T18:58:22Z

in-progress to blocked: WebKit bug in webkit2gtk-4.1 2.54.0; waiting for a WebKit fix. The upstream report drafted in the notes needs a bugs.webkit.org account to file. CI holds the clipboard on GTK3 meanwhile (#74).

**agent:claude-code/t3code-6bca1629** at 2026-10-01T19:24:17Z

Owner, 2026-10-01: pinned until after v0.1.0, including filing the upstream WebKit report.
