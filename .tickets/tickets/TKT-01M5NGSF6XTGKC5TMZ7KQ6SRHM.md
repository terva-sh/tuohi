---
schema: 4
id: TKT-01M5NGSF6XTGKC5TMZ7KQ6SRHM
title: Exit macOS fullscreen before hiding retained windows
type: bug
status: in-progress
status_reason: null
priority: high
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
claim:
  actor: agent:codex/tuohi-native-20261023
  branch: agent/native-macos-menu-fullscreen
  worktree: /Users/drewshort/.local/state/agent-scratch/tuohi-native-20261023/repo
  commit: 728218d48d300fd704c884342ec9009754d05b24
  session: null
  claimed_at: 2026-10-23T23:16:08Z
  expires_at: null
archive: null
created_at: 2026-10-23T23:15:59Z
updated_at: 2026-10-23T23:24:23Z
created_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
extensions: {}
---

## Description

Hiding a fullscreen NSWindow with orderOut leaves a black dedicated Space. View.Hide must wait for AppKit's completed fullscreen exit, retaining the same window and renderer. Track entry/exit, coalesce repeated hides, cancel pending hide on reveal/quit/close, and defer a toggle requested during DidEnter until the callback unwinds. Native visibility notifications during transition must not cancel an accepted hide.

Source: Kuiske PR #50 https://git.local.sothr.com/terva-sh/kuiske/pulls/50, implementation 89d3aba3c5c6047eaeb23e34f3820f08344f3ded; tuohi v0.2.0. Isolated macOS acceptance established exit-before-hide/reopen, repeated close during exit, custom RequestClose during entry and quit during pending exit. The owner selected this generic upstream work after Kuiske PR #51 merged c8226eabca125d91395c2c95712f8059b617f869.

Boundary: fix View.Hide implementation, preserve other platform semantics and pure Go/CGO_ENABLED=0 support. Kuiske retains tray availability/host-loss policy and keep-running decisions; no tray-specific public API. Tests use disposable windows and synthetic content, never installed apps/accounts.

## Acceptance criteria

- [x] Hide waits for completed exit and preserves the same window and renderer on reopen.
- [x] Repeated close during exit and close during entry coalesce safely; DidEnter exit is deferred.
- [x] Reveal, host-loss reveal, quit and close invalidate pending hide; intermediate visible callbacks do not cancel it.

## Definition of done

- [ ] Required checks, isolated native macOS acceptance and exact-head terva review pass; PR records evidence and boundaries.

## Implementation plan

Keep fullscreen transition state private to the Darwin engine and observe its own delegate notifications. Hide waits for DidExit; DidEnter schedules a guarded asynchronous exit. Show/Raise and lifecycle quitting cancel pending hides without responding to intermediate visibility. Exercise retained renderer identity and transition races in an isolated native scenario; preserve all other platforms.

## Notes

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:23:50Z

Implemented private Darwin menu supplementation and fullscreen-aware Hide. No new public types or methods, cgo, Matrix policy, tray registration logic or Kuiske dependency changes. Host menu actions and custom-selector shortcuts are preserved. Frameless menu Close now follows CloseRequested instead of bypassing policy. Fullscreen notifications use a separate observer, preserving an embedding host's window delegate, and observer teardown removes native and Go registry references.

Native synthetic acceptance on darwin/arm64 macOS 26.6.2 (25G83), CGO_ENABLED=0: menu shapes (absent, empty, placeholder, populated, nested existing actions, custom host shortcuts), idempotence, native WKWebView editing, framed/frameless Cancel+Hide, native Quit/Wait with joined worker; fullscreen exit-before-hide, retained window/renderer identity, repeated close during exit, close during entry, reopen/host-loss equivalent Show, Raise during entry, quit during pending exit and close teardown. All new assertions passed in the full suite; a focused run also passed before the final custom-shortcut case was added.

Required non-test checks passed: vet in both modules including Darwin/Windows, gofmt, import restrictions, public-API examples/module tidiness, embedded JavaScript, eight cross-build targets in both modules and strict ticket check. Full just ci reached native tests; its only reported failure was the existing TestPermissions WKWebView error expectations (TypeError versus OverconstrainedError/denied). Baseline reproduction is being checked separately before disposition. A separate pre-existing -short initialization crash reproduces on unchanged main 728218d48d300fd704c884342ec9009754d05b24, filed as draft TKT-01M5NH4HQ50W4KW26WXT4DYB01 (Keep Darwin short reveal tests independent of GUI initialization); it is not selected implementation scope.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:24:23Z

Confirmed identical TestPermissions failure on unchanged main 728218d48d300fd704c884342ec9009754d05b24, native darwin/arm64 macOS 26.6.2, CGO_ENABLED=0. This is a baseline limitation; remaining native suite and examples are being run with that one existing assertion excluded.
