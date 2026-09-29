---
schema: 4
id: TKT-01M3NSA8JEDJJEBA47SVA4EZ7T
title: Decide whether macOS keeps element fullscreen on by default
type: task
status: draft
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
updated_at: 2026-09-29T05:13:34Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
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
