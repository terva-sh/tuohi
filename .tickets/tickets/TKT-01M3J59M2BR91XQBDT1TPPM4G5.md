---
schema: 4
id: TKT-01M3J59M2BR91XQBDT1TPPM4G5
title: Replace atotto/clipboard with native clipboard access
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: feat/clipboard-linux
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-clip
  commit: a70d42241decfb5a0ddd6ee569d7c5eb504d159d
  session: null
  claimed_at: 2026-09-29T22:21:49Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T23:05:52Z
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
- [x] A GUI scenario round-trips non-ASCII text through the clipboard on both WebKitGTK stacks

## Implementation plan

Three pull requests, one per engine. Each is small, so a red GitHub `main` after a macOS or Windows merge is quick to find and fix.

### The shared hook: `internal/toolkit`

The clipboard package must not import the root, and the root must not import it. `internal/toolkit` sits between them. When the first window exists, the root publishes a `Toolkit`:

- `Call`: the UI dispatcher's `ui.call`.
- `GTK4`: whether GTK 4 is the loaded stack.
- `GTK`, `GLib`: the handles of the libraries the engine already opened.

`Get` returns nil in a program that never opened a window. Copy and Paste then return the new `clipboard.ErrNoApp`.

### Linux, FreeBSD, NetBSD (`feat/clipboard-linux`)

- **GTK 3.** `gtk_clipboard_get(GDK_SELECTION_CLIPBOARD)`, then `gtk_clipboard_set_text` and `gtk_clipboard_wait_for_text`.
- **GTK 4.** `gdk_display_get_clipboard`, then `gdk_clipboard_set_text` and `gdk_clipboard_read_text_async`. The main context is iterated until the callback fires, the way GTK 3 reads.
- **Binding.** Functions come from the handles the engine loaded, never a second GTK: two GTK versions in one process corrupt GObject. A missing function is an error, not a panic.
- **Empty clipboard on GTK 4.** `G_IO_ERROR_NOT_FOUND` and `NOT_SUPPORTED` read as `""`, which is how GTK 3 reports an empty clipboard.
- **Copy.** Cuts at the first NUL and replaces invalid UTF-8 on every platform.

### macOS (`feat/clipboard-darwin`)

`NSPasteboard generalPasteboard` with `NSPasteboardTypeString`. The constant is read from AppKit at run time. Calls run on the main thread through `Call`, each in its own autorelease pool.

### Windows (`feat/clipboard-windows`)

`OpenClipboard(NULL)`, `EmptyClipboard` and `SetClipboardData(CF_UNICODETEXT)` through `x/sys/windows`. Opening retries for up to 1 s. It needs no window and no hook, so it works in any program. atotto leaves `go.mod` with this PR.

### Alternatives considered

- **Keep atotto on Linux.** Rejected: it needs `xclip`, `xsel` or `wl-copy` on PATH, which is what this ticket removes.
- **Load GTK separately in the clipboard package.** Rejected: GTK 3 and GTK 4 in one process corrupt the type system, and an uninitialised GTK has no display.
- **purego for the Win32 calls.** `x/sys/windows` loads DLLs only from System32 and returns error codes, and the instance backend uses it already.

### Accepted cost, documented in the package

On Wayland, and on X11 without a clipboard manager, copied text lasts only as long as the process.

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

**agent:claude-code/t3code-72958710** at 2026-09-29T22:21:50Z

Linux branch `feat/clipboard-linux`, rebased onto main at 669905a. The work was done by a subagent; I reviewed the diff.

### Checks

- `just ci` passes.
- `just test-gui` passes on webkitgtk-6.0 and webkit2gtk-4.1. That includes `TestClipboardRoundTrip`, where `"tuohi ✓ 日本語 — ünïcödé"` survives Copy and then Paste twice: on the UI thread before the loop starts, and from another goroutine while it runs.
- The subagent's negative control: with the set-text calls removed, the test failed on both stacks.
- `TestNoApp` covers `ErrNoApp` without a window.
- golangci-lint had 0 issues on every target in the subagent's run.

### Programs that open only a window

`go list -deps` shows no godbus, atotto or clipboard package on linux, darwin or windows.

**agent:claude-code/t3code-72958710** at 2026-09-29T23:05:52Z

Linux landed in #43 (merge 166eed5); GitHub run 36642081724 passed every Linux job. macOS landed in #48 (merge d43ded1). The Windows branch feat/clipboard-windows is rebased onto it: it replaces atotto with user32 through x/sys/windows, and go list -m all no longer lists atotto. The Windows test binaries compile for amd64, arm64 and 386, and just ci passes. The Windows round trip runs on GitHub after the merge. If it fails, first check SetClipboardData after OpenClipboard(NULL), which Microsoft documents as able to fail.
