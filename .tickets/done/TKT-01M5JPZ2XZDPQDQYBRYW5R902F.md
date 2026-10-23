---
schema: 4
id: TKT-01M5JPZ2XZDPQDQYBRYW5R902F
title: Use the GTK3 maximized bit when toggling window state
type: bug
status: done
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
updated_at: 2026-10-23T04:36:09Z
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

## Acceptance criteria

- [x] GTK3 tests its own documented MAXIMIZED bit and GTK4 preserves its mask.
- [x] Native app-region maximize/restore passes on both Linux GTK stacks under a window manager; the former GTK3 mask fails the regression.
- [x] Cgo-free headless tests, both GTK stacks and prescribed cross builds pass.

## Implementation plan

Use separate documented GdkWindowState GTK3 MAXIMIZED=4 and GdkToplevelState GTK4 MAXIMIZED=2 masks in native toggleMaximize. Regress real maximize/restore through the existing app-region bridge under an X11 window manager on both runtime stacks, with strict activation of that scenario in its test recipe. Exercise known iconified and maximized masks with headless stubs as needed, without changing defaults. Validate CGO_ENABLED=0 just ci/all prescribed targets and both GUI stacks; record native versus compiled evidence. Documentation/API sources: https://docs.gtk.org/gdk3/flags.WindowState.html and https://docs.gtk.org/gdk4/flags.ToplevelState.html .

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T04:36:09Z

Confirmed published GDK enum values: https://docs.gtk.org/gdk3/flags.WindowState.html MAXIMIZED=4, ICONIFIED=2; https://docs.gtk.org/gdk4/flags.ToplevelState.html MAXIMIZED=2. Native regression sends the app-region bridge toggle through a real loaded page and observes actual GDK state under Openbox on isolated Xvfb. Before production fix, GTK3 maximizes once then fails restoring (native maximized state did not become false). After distinct masks, two complete maximize/restore cycles pass on both WebKitGTK stacks. Existing Node tracker regression covers the double-click gesture separately. Added strict test-maximize recipe and both CI workflows; Openbox is a test-only runtime dependency. Local Openbox/dependency packages were extracted under task scratch without host installation. Full CGO_ENABLED=0 just ci passed both modules and all prescribed cross targets; updated just test-gui passed both stacks and consumer examples, including the new WM scenario. No macOS/Windows production code or public API changed; those targets compiled, new native mirror runtime follows merge.

## Summary

Use separate GTK3 MAXIMIZED=4 and GTK4 MAXIMIZED=2 masks in app-region toggleMaximize. A strict isolated Openbox/Xvfb scenario exercises the real bridge and two maximize/restore cycles; old GTK3 code fails restoration, corrected code passes both GTK stacks. CI recipes include the native regression. Full cgo-free CI/cross builds and both local GUI stacks/examples pass. Submitted through the internal PR process; no public API/default changes or release publication.
