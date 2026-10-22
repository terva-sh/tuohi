---
schema: 4
id: TKT-01M5JPZ2XZDPQDQYBRYW5R902F
title: Use the GTK3 maximized bit when toggling window state
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-22T21:06:11Z
updated_at: 2026-10-22T21:06:11Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

While implementing TKT-01M5JKX3C39YNR5SM64A8EK6YR (Add close interception and native window lifecycle events), source inspection found lib_unix.go gdkStateMaximized uses 1<<1 for both toolkits. GTK3 GdkWindowState uses GDK_WINDOW_STATE_ICONIFIED=1<<1 and GDK_WINDOW_STATE_MAXIMIZED=1<<2, while GTK4 GdkToplevelState uses MAXIMIZED=1<<1. toggleMaximize consequently tests GTK3's iconified bit when deciding whether to restore a maximized window. Verify the platform constants against the supported runtime API, split the masks and regress the double-click toggle under a window manager. This pre-existing unrelated toggle defect was not changed in the selected lifecycle PR. Filed as draft for owner selection.
