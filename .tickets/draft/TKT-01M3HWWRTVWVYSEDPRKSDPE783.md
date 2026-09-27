---
schema: 4
id: TKT-01M3HWWRTVWVYSEDPRKSDPE783
title: Deny media and clipboard permissions unless the app allows them
type: task
status: draft
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
updated_at: 2026-09-27T16:59:09Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
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
