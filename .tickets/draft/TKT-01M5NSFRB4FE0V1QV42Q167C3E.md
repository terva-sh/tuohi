---
schema: 4
id: TKT-01M5NSFRB4FE0V1QV42Q167C3E
title: Let a Windows consumer place WebView2's user data folder
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/engine-windows
  - area/api
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
created_at: 2026-10-24T01:47:58Z
updated_at: 2026-10-24T01:47:58Z
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

WebView2's user data folder is `%APPDATA%\<exe name>` (`userDataFolder` in
lib_windows.go): in the roaming profile, and outside the application's own
data directory. A consumer cannot move it.

### Why

Kuiske's native Windows spike on 2026-10-23 (Kuiske ticket
TKT-01M5NCYPZ8SS7DGZEG3MG9XGQAP) found its WebView2 data under
`%AppData%\Roaming\<exe name>`. Browser caches do not belong in a roaming
profile, and an application wants its web data beside its other data, under
a name it chooses, not its executable's. Windows is post-1.0 for Kuiske, so
nothing waits on this yet.

### Things to settle

- API shape: an App field for the data directory, or a default under
  `%LOCALAPPDATA%` named for App.Name, or both.
- Whether to move existing data, or leave a changed default to the consumer.
- The equivalent on WebKitGTK and WKWebView, so the field means the same thing
  everywhere.
