---
schema: 4
id: TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG
title: GUI tests skip wherever bubblewrap is installed
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-linux
  - area/ci
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T16:59:08Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
extensions: {}
---

## Description

### What

`lib_unix_test.go`'s `webkitRunnable` decides whether WebKitGTK can start its helpers by running `bwrap --unshare-user -- /bin/true`. That call gives bubblewrap an empty root, so `/bin/true` does not exist, bwrap exits 1, and the probe reports that WebKit cannot run. On any machine with bubblewrap installed, every Linux GUI scenario silently skips. That includes Debian 13, where WebKitGTK depends on bubblewrap.

### Evidence (2026-09-27, Debian 13, WebKitGTK 2.52.6)

- `bwrap --unshare-user -- /bin/true` prints `execvp /bin/true: No such file or directory` and exits 1.
- `bwrap --unshare-user --ro-bind / / -- /bin/true` exits 0.
- Under `xvfb-run` and `dbus-run-session`, `go test -v .` reports five skips: `TestBridge`, `TestErrorAndUnbind`, `TestRichBindingTypes`, `TestEmbedExternalWindow`, and `TestWaitReturnsAfterLastWindowCloses`. The package still reports `ok`.
- With the probe given `--ro-bind / /`, the scenarios run and the test binary crashes. That is the bug filed alongside this one.

### Fix

Probe with a real root, for example `--ro-bind / /`. Also make a skip visible: when a display is present but a scenario skips, `just test-gui` should fail, because a green run that tested nothing is how this went unnoticed.

## Acceptance criteria

- [ ] The availability probe succeeds where bubblewrap can create a user namespace
- [ ] just test-gui fails when a display is present and a GUI scenario skips
- [ ] The five Linux GUI scenarios run under just test-gui on Debian 13
