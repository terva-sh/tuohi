---
schema: 4
id: TKT-01M3NSA8JEDJJEBA47SVA4EZ7T
title: Document why macOS keeps element fullscreen on
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area/engine-darwin
  - security
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
created_at: 2026-09-29T05:13:34Z
updated_at: 2026-10-01T05:14:34Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

Decide whether macOS keeps turning on WKWebView's `fullScreenEnabled`, which is off by default.

### Current behaviour

`lib_darwin.go` sets `fullScreenEnabled` to YES as "appkit's tuned default". With it on, a page can take the whole screen through the Fullscreen API with a user gesture. WebKitGTK and WebView2 allow element fullscreen by default, so turning it on makes macOS behave like the other engines.

### Direction

Either:
- keep it and document why, which gives consistency across engines; or
- turn it off unless the application asks, which makes it a permission like those in `View.Permissions`.

The architecture review listed it next to the media grants. It is split out of TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the app allows them), because that ticket's acceptance criteria cover only camera, microphone and clipboard.

## Acceptance criteria

- [ ] The comment in lib_darwin.go gives the cross-engine reason for fullScreenEnabled
- [ ] The View.Permissions documentation says element fullscreen is allowed on every engine and is not a permission

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: keep it on, and document why

The owner decided, 2026-10-01. macOS keeps `fullScreenEnabled` on, so element fullscreen behaves as it does on WebKitGTK and WebView2, which allow it by default. A page still needs a user gesture to enter fullscreen.

What's left is documentation. Replace the "appkit's tuned default" comment in lib_darwin.go with this reason, and state the cross-engine behaviour wherever View.Permissions is documented, so nobody expects it to be a permission. Making it a permission lost because it would have to apply on all three engines to be consistent, which costs more work on Linux and Windows to guard against a gesture-gated risk.

It does not block v0.1.0.
