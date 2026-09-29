---
schema: 4
id: TKT-01M3NSA8HE52CHVQZ2A6T8H5NS
title: Decide whether APPKIT_DEBUG may open dev tools in a release build
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - security
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

Decide whether the `APPKIT_DEBUG=1` environment variable may turn a view's dev tools on in a release build.

### Current behaviour

`App.Debug` ORs in `APPKIT_DEBUG=1` (`app.go`), and `View.Debug` ORs in `App.Debug`. So whoever launches the program can open the web inspector on its pages, and nothing the program sets can prevent it.

### Direction

The architecture review leaned no. `View.Debug` is the application's decision, and an environment variable belongs to whoever launched the program. Options:
- drop the variable;
- honour it only when the program opts in, for example through an `App` field;
- keep it and document it.

Split out of TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the app allows them), whose acceptance criteria do not cover it.
