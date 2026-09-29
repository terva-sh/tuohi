---
schema: 4
id: TKT-01M3J59M2BR91XQBDT1TPPM4G5
title: Replace atotto/clipboard with native clipboard access
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T21:13:07Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Replace `github.com/atotto/clipboard` with native clipboard access on each platform.

### Why

- **Linux:** atotto runs external tools: `wl-copy` or `wl-paste`, `xclip`, `xsel`, the termux tools, or `clip.exe`. The choice is fixed by an `init` that scans PATH at process start, and a desktop without one of those tools has no clipboard at all.
- **macOS:** it runs `pbcopy` and `pbpaste`.
- **Windows:** it truncates at the first NUL.

GTK, AppKit, and Win32 are already loaded in the process.

### Shape

- **GTK3:** `gtk_clipboard_get` with `gtk_clipboard_set_text` and `gtk_clipboard_wait_for_text`, on the UI thread.
- **GTK4:** `gdk_display_get_clipboard` with `gdk_clipboard_set_text` and `gdk_clipboard_read_text_async`. Paste needs a callback and a round trip through the UI thread.
- **macOS:** `NSPasteboard` through `objc`.
- **Windows:** Win32 `OpenClipboard` with `CF_UNICODETEXT`.

One difference to document: on Wayland, text copied through GTK lasts only while the process runs. `wl-copy` forks a process that keeps it.

This lands in `tuohi/clipboard` (see TKT-01M3J59M1H9PZ04J2C9JJZ7V13 (Move desktop services out of the root package)).

## Acceptance criteria

- [ ] Copy and paste work on GTK3, GTK4, macOS, and Windows without running an external program
- [ ] github.com/atotto/clipboard is gone from go.mod
- [ ] A GUI scenario round-trips non-ASCII text through the clipboard on both WebKitGTK stacks

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T21:13:06Z

### Owner decision, 2026-09-30: in this run, native, without cgo

The owner asked whether this can be done without cgo. It can, the way every engine already works:

- GTK3 `gtk_clipboard_*` and GTK4 `gdk_clipboard_*` through purego;
- `NSPasteboard` through `objc`;
- user32 `OpenClipboard`, `SetClipboardData`, and `GetClipboardData`, with kernel32 `GlobalAlloc` and `GlobalLock`.

Costs accepted, which the package documentation must state:

- The clipboard works only once tuohi has started its toolkit. A program that never opens a window has no clipboard.
- On Wayland, copied text lasts only as long as the process, and setting the clipboard may need recent input on one of its surfaces.
- GTK4 paste is asynchronous, so `Paste` waits on a round trip through the UI thread.

It lands after the package split (TKT-01M3J59M1H9PZ04J2C9JJZ7V13), in `tuohi/clipboard`.
