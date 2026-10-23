---
schema: 4
id: TKT-01M5NH8TV3APBN79DC6YN05D6J
title: Reconcile native macOS permission fixture with current WKWebView
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
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
created_at: 2026-10-23T23:24:22Z
updated_at: 2026-10-23T23:24:22Z
created_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
extensions: {}
---

## Description

On darwin/arm64 macOS 26.6.2 (25G83), the full native suite's TestPermissions fails with video/audio/frame TypeError and no microphone policy callback, instead of its expected OverconstrainedError/denied values and microphone:no. Reproduced with CGO_ENABLED=0 go test -count=1 -run '^TestPermissions$' . on unchanged main 728218d48d300fd704c884342ec9009754d05b24 in an isolated baseline worktree. The native menu/fullscreen change reports the identical failure.

Investigate the WKWebView permission fixture's origin/capability assumptions on modern macOS while retaining fail-closed permission policy. Do not change production policy to satisfy expected browser error strings. This is a separately recorded platform test baseline issue, not selected implementation scope for the native menu/fullscreen fixes.

## Acceptance criteria

- [ ] The native permission scenario reports and asserts current WKWebView behavior without weakening permission policy.
