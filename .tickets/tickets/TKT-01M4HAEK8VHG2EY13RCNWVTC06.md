---
schema: 4
id: TKT-01M4HAEK8VHG2EY13RCNWVTC06
title: Let a tray application keep its macOS Dock icon
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/tray
  - area/engine-darwin
  - area/api
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude/vuoro-dock-20261009
  branch: feat/tray-keep-dock
  worktree: /home/sothr/.cache/agent-scratch/tuohi/dock-lVyp/tuohi
  commit: 1b5f983774b956c4e4ea56c75462eac7de0f4f91
  session: null
  claimed_at: 2026-10-09T21:52:37Z
  expires_at: null
archive: null
created_at: 2026-10-09T21:52:32Z
updated_at: 2026-10-09T21:58:09Z
created_by:
  id: agent:claude/vuoro-dock-20261009
  name: ""
updated_by:
  id: agent:claude/vuoro-dock-20261009
  name: ""
extensions: {}
---

## Description

On macOS, `tray.Set` and `tray.Run` switch the application to the accessory activation policy unconditionally (`tray/tray_darwin.go`). A tray application therefore has no Dock icon and no place in the application switcher, even while a window is open. vuoro reported this: once it put its icon in the menu bar, its window vanished from the Dock and from Command-Tab, and the person wants to choose.

### Approach

Add `Dock bool` to `tray.Config`. On macOS, `set` uses the regular policy when it is true and the accessory policy otherwise, so the default is unchanged. Windows and Linux ignore it, because a tray there does not touch the taskbar. A Dock click on a hidden window already restores it through the reopen handler in `lib_darwin.go`.

### Alternatives

- Switching the policy as windows show and hide (Dock icon only while a window is open): needs tuohi to track window visibility against the tray and to change the policy at run time, which macOS applies with visible flicker. Deferred until a consumer asks for it.
- A policy on `tuohi.App` instead of the tray: the tray is what forces the policy today, so the opt-out belongs beside it.

## Acceptance criteria

- [x] tray.Config.Dock keeps the regular activation policy on macOS; the default stays accessory
- [x] The tray README and the showcase (-dock) show the option
- [ ] Checked on a Mac: with Dock, the Dock icon stays while the tray is up

## Implementation plan

Add tray.Config.Dock; tray_darwin.set picks the regular policy when it is set, accessory otherwise. Document it in tray/README.md and add -dock to the showcase. vuoro bumps to the release that carries it.

## Notes

**agent:claude/vuoro-dock-20261009** at 2026-10-09T21:53:14Z

just ci passes on linux, with every target cross-built. Nothing here runs macOS, so criterion 3 waits for the GitHub mirror's darwin job after the merge and for a person on a Mac.

**agent:claude/vuoro-dock-20261009** at 2026-10-09T21:58:09Z

CI run 539: the GUI job failed TestViewMethodsFromBinding on webkitgtk-6.0 with 'got no report' and passed it on webkit2gtk-4.1. The change touches only tray_darwin.go and a Config field, which that scenario never reaches, and the scenario passed 3 of 3 locally under xvfb on webkitgtk-6.0. Re-running CI.
