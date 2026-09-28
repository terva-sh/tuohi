---
schema: 4
id: TKT-01M3N0PTS10TG629DVNEFSNVSZ
title: Check that a Close from Go reaches App.Wait on macOS
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
assignees: []
milestone: null
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T22:03:32Z
updated_at: 2026-09-28T22:03:32Z
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

On macOS, a window closed by `View.Close` may never be reported to the App scope. `destroyOnUI` in lib_darwin.go clears the window delegate before sending `close`, and `appWindowClosed` is called only from `onWindowWillClose`. If that is right, `App.Wait` with `Exit` set never returns after the last window is closed from Go.

`destroyOnUI` also calls `w.onWindowDestroyed(true)` after `close`, which may already report it. Nobody has checked which path runs.

### Found by

Found by reading, during the inventory for TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine). It has not been reproduced.

### Things to settle

- A macOS GUI scenario: `App.Exit` true, one view, `View.Close` from Go, and `App.Wait` must return.

## Acceptance criteria

- [ ] Whether a Destroy-driven close reaches appWindowClosed on macOS is established by a scenario
- [ ] App.Wait with Exit set returns after the last window is closed from Go
