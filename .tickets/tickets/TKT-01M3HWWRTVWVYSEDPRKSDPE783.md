---
schema: 4
id: TKT-01M3HWWRTVWVYSEDPRKSDPE783
title: Deny media and clipboard permissions unless the app allows them
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-darwin
  - area/engine-linux
  - area/engine-windows
  - security
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: feat/permissions
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/perm
  commit: e66fc6e7ea97cf892b1682dafa12762704efb30e
  session: null
  claimed_at: 2026-09-29T05:07:44Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-29T05:40:48Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Make camera, microphone, clipboard, and similar permissions an explicit policy the application sets, denied by default on every platform.

### Current behaviour

- **macOS** grants every media-capture request, whatever the origin or frame (`lib_darwin.go:468-472`).
- **Linux** enables MediaStream and JavaScript clipboard access in WebKitGTK settings (`lib_unix.go:1741-1742`) and registers no permission-request handler. That WebKitGTK then denies by default has not been verified.
- **Windows** declares `AddPermissionRequested` (`lib_windows.go:172`) and never registers it.

## Acceptance criteria

- [ ] No engine grants camera, microphone, or clipboard access without an explicit application policy
- [ ] The Linux default is verified by a test rather than assumed

## Implementation plan

### Measured first (Linux, both WebKitGTK stacks, xvfb, mock capture devices)

- **Media.** With `enable_media_stream` on and no `permission-request` handler (today), `getUserMedia` for video and for audio is denied (`NotAllowedError`). WebKitGTK's default denies. A handler sees one `WebKitUserMediaPermissionRequest` per call, and `is_for_audio_device`/`is_for_video_device` say which. Allowing the request grants a stream from the mock devices.
- **Clipboard.** With `javascript_can_access_clipboard` on (today), any page, with no user gesture, can:
  - copy (`execCommand('copy')` returns true);
  - paste: `execCommand('paste')` fires a `paste` event whose `clipboardData` holds the system clipboard. It read back `"probe-secret"` that had been copied.

  With the setting off, both fail. A real X click (XTest), however, still copies through `execCommand('copy')` and `navigator.clipboard.writeText`. So turning the setting off does not break a consumer's copy button, and only gesture-less script access is lost. `navigator.clipboard.readText()` without a gesture is refused either way, and no permission request is raised.

### API (decided autonomously; revisit if a consumer needs more)

```go
type Permission int
const (
	PermissionCamera Permission = iota + 1
	PermissionMicrophone
	PermissionClipboard
)
// View.Permissions []Permission
```

A permission is granted only when the view lists it **and** the requesting page's origin is one the view trusts. Everything else is denied. The alternatives lost for these reasons:
- **A callback, `func(origin, Permission) bool`.** Trust already scopes requests to the application's own origins, and View's define-first fields favour a declared list. A callback can be added later without breaking the list.
- **A per-permission origin list.** The same argument applies.

### Per engine

- **Linux.** `enable_media_stream` stays on, so `navigator.mediaDevices` exists on every engine, and a `permission-request` handler decides:
  - user media: video needs Camera and audio needs Microphone, for the top-level page's URI. WebKitGTK gives no frame origin, but a cross-origin frame needs the trusted page's `allow=` delegation anyway.
  - device info: allowed when Camera or Microphone is.
  - clipboard permission request: Clipboard.
  - geolocation, notification, media key system and website data access: denied.
  - pointer lock and anything unknown: left to WebKit.

  `javascript_can_access_clipboard` is on only when the view lists Clipboard. That setting covers the whole view, so the grant also applies to frames; this is documented.
- **Windows.** Register the `PermissionRequested` handler, which is declared today but unused. Microphone (1), camera (2) and clipboard-read (6) are decided by the policy for the request's `Uri`, which is the requesting frame's origin. Every other kind is set to DENY, so WebView2 shows no prompt of its own.
- **macOS.** `requestMediaCapturePermission` decides camera, microphone, or both, for the request's security origin (the frame's) instead of granting everything. WKWebView has no clipboard permission, and a script read shows the system Paste callout with a user gesture, so Clipboard changes nothing there.

### Out of scope, filed separately

- macOS `fullScreenEnabled`.
- The `APPKIT_DEBUG` dev-tools switch in release builds.

Both came up in the architecture review but are not in this ticket's acceptance criteria.

### Tests

- A headless test of the policy decision.
- A GUI scenario on every engine:
  - the default view denies camera, microphone and gesture-less paste;
  - a view listing Camera gets video and is still denied audio;
  - a view listing Clipboard can paste without a gesture.

  Linux uses WebKitGTK's mock capture devices, and Windows uses Chromium's `--use-fake-device-for-media-stream` through `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`. macOS checks only the denials: granting would make WebKit open a real capture device, which a test binary without camera usage strings is killed for by TCC.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decisions from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

See `docs/architecture.md`, under "Permissions are denied unless the app grants them". The complete surface the review found:

- **Linux:** `enable_media_stream` and `javascript_can_access_clipboard` are turned on (`lib_unix.go:1775-1776`), and there is no `permission-request` handler.
- **macOS:** every media-capture request is granted (`lib_darwin.go:468-472`), and `fullScreenEnabled` is turned on against the native default.
- **Windows:** no `PermissionRequested` handler, so WebView2 shows its own prompts. Downloads also use WebView2's UI.
- **All platforms:** `APPKIT_DEBUG=1` turns dev tools on in any build (`app.go:167`).

Shape: a per-view permission policy that the app sets, denying by default, applied in one handler per engine and connected to every engine. Decide whether dev tools may be turned on by an environment variable in a release build. The review leans no: `View.Debug` is the app's choice, and an environment variable is the user's.

**agent:claude-code/t3code-72958710** at 2026-09-29T05:16:21Z

### Implementation (feat/permissions)

As planned. Details settled while building it:

- **Linux handler.** It resolves each request type with `Dlsym` first, so a WebKitGTK that lacks one (clipboard requests are recent) loads anyway.
- **Screen capture.** A user-media request for neither video nor audio, which is how a screen capture arrives, asks `permits` for nothing and is denied, because `permits` needs at least one permission.
- **Windows.** Every other permission kind is set to DENY, with a log line.
- **macOS.** It builds the origin as `scheme://host[:port]/` from the `WKSecurityOrigin`.
- **Logging.** Each denial is logged, because a page only sees `NotAllowedError`.
- **`allows`** is used only by the Linux device-label case, so it lives in `lib_unix.go`.

### Tests and controls

`TestPermissions` (GUI) shows three views: none, camera, and clipboard. Each tries video, audio, and a gesture-less `execCommand('paste')`, and has a frame on another origin, delegated with `allow="camera; microphone"`, try video.

Expected on Linux, which passes on both stacks:
- none: all denied, paste false, frame denied;
- camera: video ok, audio denied, frame ok (WebKitGTK shares the page's grant with a delegated frame);
- clipboard: paste true.

Windows expects the frame to be denied, since its origin is not trusted, and paste false. macOS checks only the default view.

Controls on GTK4, each of which fails:
- `permits` granting everything, where everything is ok;
- the clipboard setting always on, as before, where paste is true in every view;
- no Linux handler, where the camera view is denied, proving the grant path is ours.

Filed as drafts, out of this ticket's criteria:
- TKT-01M3NSA8HE52CHVQZ2A6T8H5NS (Decide whether APPKIT_DEBUG may open dev tools in a release build);
- TKT-01M3NSA8JEDJJEBA47SVA4EZ7T (Decide whether macOS keeps element fullscreen on by default).

The Windows and macOS expectations are unverified until GitHub CI runs them.

**agent:claude-code/t3code-72958710** at 2026-09-29T05:28:41Z

### What GitHub CI showed on Windows and macOS (runs 36525394924, 36525792930, 36526107654)

This supersedes the "unverified" line in the previous note.

**No capture devices on the runners.**
- WebView2 fails every capture with `NotFoundError` before raising `PermissionRequested`.
- WKWebView fails video with `OverconstrainedError`.

So the camera and microphone grant cannot be seen there.

**No fake devices for WebView2.** Chromium's `--use-fake-device-for-media-stream` cannot reach WebView2: tuohi creates the environment through the runtime's internal `CreateWebViewEnvironmentWithOptionsInternal`, with null options, and `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` is read by the WebView2Loader it skips. Passing browser arguments would need an `ICoreWebView2EnvironmentOptions` COM object, which is not worth building for a test.

**The scenario now reaches every handler.** It also asks for a clipboard read (`navigator.clipboard.readText`) and a notification (`Notification.requestPermission`). A test hook, `permissionDecided`, records every decision `viewCore.permits` makes. Results:

- **Windows**, which passes:
  - none view: read denied, notify denied, and `asked=clipboard:no`;
  - clipboard view: `read=ok` and `asked=clipboard:yes`.

  That is the grant path verified on a real WebView2.
- **macOS**, which passes:
  - `audio=denied` with `asked=microphone:no`, so the delegate is consulted and denies;
  - notifications are denied;
  - grants are not exercised, because of TCC.
- **Linux** is unchanged: both stacks pass, including the camera grant with mock devices.

**Still unverified on a real engine:** the Windows camera and microphone grant, and the macOS grant. They share `permits`, which `TestPermits` covers, and the per-engine kind mapping.

**agent:claude-code/t3code-72958710** at 2026-09-29T05:40:48Z

### Review disposition for PR #31, round 1 (terva-review run on def76da)

GitHub run 36526386848 on def76da passed every job.

1. **High: Linux clipboard permission exposes the clipboard to untrusted frames.** Fixed.
   - **Change:** `javascript_can_access_clipboard` now stays off in every view. It covers the whole view, so it could never honour the trusted-origin rule.
   - **What was measured:** on both stacks, a real XTest click followed by `navigator.clipboard.readText()` raises a `WebKitClipboardPermissionRequest`, and `permissionRequest` already decides it through `permits`. The clicked read returned `"clicked"` for a view that lists the clipboard, and `NotAllowedError` for one that does not.
   - **Meaning:** `PermissionClipboard` is now a per-request read on every engine. No view can paste without a gesture.
   - **Frames:** a frame reaches the request only when the trusted page delegates `clipboard-read` to it with `allow=`, as with the camera.

   The permissions scenario now clicks through XTest on Linux. `libxtst6` is added to the GitHub Linux jobs and to the Debian list in AGENTS.md. On Linux the scenario expects `paste=false` in every view, with `clickread=ok` only in the clipboard view. Controls on GTK4, each of which fails:
   - turning the setting back on for the clipboard view (`paste=true`);
   - never granting the clipboard request (`clickread=denied`).

Checks after the fix: `just ci`, `just test-gui` on both stacks, and golangci-lint on three GOOS.
