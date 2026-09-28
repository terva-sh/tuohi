---
schema: 4
id: TKT-01M3HWWRTVWVYSEDPRKSDPE783
title: Deny media and clipboard permissions unless the app allows them
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-28T21:34:09Z
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

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decisions from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

See `docs/architecture.md`, under "Permissions are denied unless the app grants them". The complete surface the review found:

- **Linux:** `enable_media_stream` and `javascript_can_access_clipboard` are turned on (`lib_unix.go:1775-1776`), and there is no `permission-request` handler.
- **macOS:** every media-capture request is granted (`lib_darwin.go:468-472`), and `fullScreenEnabled` is turned on against the native default.
- **Windows:** no `PermissionRequested` handler, so WebView2 shows its own prompts. Downloads also use WebView2's UI.
- **All platforms:** `APPKIT_DEBUG=1` turns dev tools on in any build (`app.go:167`).

Shape: a per-view permission policy that the app sets, denying by default, applied in one handler per engine and connected to every engine. Decide whether dev tools may be turned on by an environment variable in a release build. The review leans no: `View.Debug` is the app's choice, and an environment variable is the user's.
