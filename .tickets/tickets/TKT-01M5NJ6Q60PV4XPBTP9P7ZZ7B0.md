---
schema: 4
id: TKT-01M5NJ6Q60PV4XPBTP9P7ZZ7B0
title: Read an image from the system clipboard
type: task
status: in-progress
status_reason: null
priority: high
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
claim:
  actor: agent:claude-code/tuohi-clipboard-image
  branch: t3/clipboard-image
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-clipboard-image
  commit: 728218d48d300fd704c884342ec9009754d05b24
  session: null
  claimed_at: 2026-10-23T23:40:52Z
  expires_at: null
archive: null
created_at: 2026-10-23T23:40:42Z
updated_at: 2026-10-24T00:09:14Z
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

Add image reading to the `clipboard` package, so an app can fetch the
clipboard image natively when the web paste event carries none.

### Why

Asked for by the owner on 2026-10-23 from a Kuiske session. Pasting an image
copied in a Windows browser into Kuiske under WSLg (WSL 2.7.11, WSLg 1.0.73,
Windows build 26200, Ubuntu 24.04, WebKitGTK 2.52.6 on both stacks, Wayland)
does nothing:

- `wl-paste --list-types` reports `text/html image/bmp`.
- The page's paste event gets `types=["text/html"]` and no file item:
  WebKitGTK does not expose the BMP to the page at all.
- On the X11 side WSLg offers only text targets.

Kuiske's paste handler reacts only to an image file in the paste event, and
`clipboard` is documented as text-only, so Kuiske has no native way to read
the image. Kuiske will call the new API when a paste event has no image file,
and its server decodes PNG, JPEG, GIF and WebP, so PNG output fits.

### Constraints

- No cgo and no external programs, as the package already requires.
- Bound the bytes read and the decoded dimensions; never log image contents.
- A distinct error when the clipboard holds no image.
- Native tests that put a real image on a real clipboard on GTK 3 and GTK 4
  under Xvfb and read it back, including a BMP-only offer.
- macOS and Windows: implement if feasible, otherwise a clear unsupported
  error and follow-up tickets.

## Acceptance criteria

- [x] clipboard has an API that returns the clipboard image as PNG bytes, bounded by caller-supplied limits on bytes and pixels
- [x] A clipboard with no image gives a distinct error, and an image over a limit gives another
- [x] GTK 3 and GTK 4 read PNG and BMP-only offers, including WSLg's text/html plus image/bmp
- [x] GUI scenarios put a real image on a real clipboard from another process and read it back on both WebKitGTK stacks
- [x] macOS and Windows either read images or return a clear unsupported error, with follow-up tickets filed
- [ ] Pull request opened on Forgejo with a terva review requested and findings dispositioned

## Implementation plan

### API

`clipboard.PasteImage(ImageLimits) ([]byte, error)` returns the clipboard
image re-encoded as PNG. `ImageLimits{MaxBytes, MaxPixels}`: MaxBytes bounds
the encoded image read from the clipboard and the PNG returned, MaxPixels
bounds width times height; zero means a documented default. `ErrNoImage` when
nothing readable is offered, `ErrImageTooLarge` (wrapped with the sizes, never
the contents) over a limit.

### Approach

Read the raw bytes of the best offered format and decode them in Go, rather
than through `gdk_clipboard_read_texture_async` or
`gtk_clipboard_wait_for_image`:

- BMP goes through gdk-pixbuf's bmp loader, which upstream moved into the
  optional "others" set in 2.42.11; Ubuntu 24.04 (2.42.10) ships it, newer
  distributions need not. Decoding in Go removes that dependency.
- GDK and GdkPixbuf decode the whole image before tuohi sees its size. Reading
  the bytes first lets tuohi stop at MaxBytes (GTK 4 streams) and check the
  header dimensions before any pixel buffer is allocated.

Preference order: image/png, the BMP types (image/bmp, image/x-bmp,
image/x-MS-bmp), image/jpeg, image/gif. A pure-Go DIB decoder in the package
handles BMP (BI_RGB 1/4/8/16/24/32, BI_BITFIELDS, V4/V5 headers, top-down),
and will serve Windows CF_DIB later. Output is always re-encoded PNG.

- GTK 4: `gdk_clipboard_read_async` with the ordered mime list (GDK picks the
  first offered), then `g_input_stream_read_bytes_async` in a loop, iterating
  the main context like the text read, stopping past MaxBytes.
- GTK 3: `gtk_clipboard_wait_for_targets`, pick, then
  `gtk_clipboard_wait_for_contents`. GTK 3 buffers the whole transfer, so the
  byte bound applies after the transfer; documented.
- macOS and Windows return ErrUnsupported in this change, with follow-up
  tickets: AGENTS.md asks that new code for them land in small PRs, tested on
  GitHub after merge.

### Tests

Headless unit tests for the decoder and limits. GUI scenario in the root
package: the test binary re-executes itself as a clipboard owner offering
given mime types (PNG only; text/html + image/bmp like WSLg; text only), and
the parent reads with PasteImage on both stacks under Xvfb.

## Notes

**agent:claude-code/tuohi-clipboard-image** at 2026-10-24T00:08:28Z

### Findings while building it

- gdk-pixbuf's BMP loader is in its optional "others" set (ani, bmp, icns,
  ico, pnm, qtif, tga, xbm, xpm), which upstream stopped building by default
  in 2.42.11. Ubuntu 24.04's 2.42.10 ships it. So
  `gdk_clipboard_read_texture_async` and `gtk_clipboard_wait_for_image`
  read BMP only where the distribution enables it, and both decode the whole
  image before tuohi sees its size. PasteImage reads the raw offered bytes
  and decodes PNG, BMP, JPEG and GIF in Go instead (clipboard/dib.go,
  clipboard/image.go).
- On this WSLg host, GTK opens the default `wayland-0` socket even with
  WAYLAND_DISPLAY unset, so `xvfb-run` alone does not move the GUI
  scenarios to Xvfb. They run on the real compositor and can replace the
  machine's Windows clipboard; the text clipboard scenario already did.
  `env -u WAYLAND_DISPLAY GDK_BACKEND=x11` keeps them on Xvfb. On Wayland
  the image scenario leaves out its owner processes: a window that just
  opened has had no input, so the compositor does not give it the
  clipboard.
- Real WSLg, through a throwaway probe: with `wl-copy -t image/bmp` owning
  the Wayland clipboard, GTK 3 (webkit2gtk-4.1) PasteImage returned the
  64×48 image as PNG. GTK 4 (webkitgtk-6.0) got ErrNoImage because
  `gdk_clipboard_get_formats` was empty and the clipboard still local. GDK
  had received no selection offer, which a Wayland compositor sends only to
  the focused client, and the probe's window was not focused. GTK 4 under
  WSLg is therefore unverified on a focused window, and Kuiske should check
  it there.
- Running the scenarios moved the live clipboard on this host while someone
  was using it, so probing stopped there.
