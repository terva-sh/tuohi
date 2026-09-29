---
schema: 4
id: TKT-01M3QQS4NJ9X0E0WGYVMT43A1H
title: Stop make clean from deleting the demo source directory
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
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
created_at: 2026-09-29T23:25:13Z
updated_at: 2026-09-29T23:25:13Z
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

The Makefile's `clean` target ends with `rm -rf ./demo`. `./demo` is the demo application's source directory, tracked in git, not a build output. So `make clean` deletes the demo's source from the working tree. Anything uncommitted there is lost, and the tracked files come back only with `git checkout`.

It is inherited from appkit. It was found while renaming the Makefile's binaries under TKT-01M3HWWRWX3D9XTA5RTW5AC26R (Finish renaming appkit to tuohi in prose, env vars, and names), and left alone as out of scope.

### Fix

Remove the line. The demo's build outputs go to `./build`, which `clean` already removes. Check whether anything else in the Makefile writes under `./demo`.

## Acceptance criteria

- [ ] make clean removes only build outputs and leaves every tracked file in place
