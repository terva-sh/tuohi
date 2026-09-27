---
schema: 4
id: TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG
title: GUI tests skip wherever bubblewrap is installed
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-92c88910
  branch: t3code/read-bootstrap-handoff
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-92c88910
  commit: 3e4df6b0437c64def647e5b7fc6817b33094e831
  session: null
  claimed_at: 2026-09-27T17:22:08Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T17:23:10Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
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

## Implementation plan

### Reproduction (2026-09-27, this workstation, main at 3e4df6b)

`bwrap --unshare-user -- /bin/true` exits 1 with `execvp /bin/true: No such file or directory`, and with `--ro-bind / /` it exits 0. Under `dbus-run-session` and `xvfb-run`, `go test -v .` skips all five scenarios on both `APPKIT_BACKEND` stacks and still reports `ok`.

### Approach

1. **Probe with a real root.** `webkitRunnable` runs `bwrap --unshare-user --ro-bind / / -- TRUE`, where TRUE is `true` resolved on PATH. Binding the host root makes the command exist inside the sandbox, and resolving it avoids assuming `/bin/true` exists, which fails on NixOS.
2. **Say why the GUI is unavailable.** `guiAvailable` records a reason (no display, bwrap failed, `XDG_RUNTIME_DIR` not writable, or the library init error), and the skip message prints it in place of the generic hint.
3. **Make a skip fail when asked to.** A test-only variable, `TUOHI_REQUIRE_GUI=1`, turns the skip in `requireGUI` into `t.Fatal` with that reason. `just test-gui` sets it.

### Alternatives considered

- **Fail whenever a display is present:** rejected. A developer running `go test ./...` in a desktop session without WebKitGTK would then get failures rather than skips, and the headless-safe default is the contract CI and consumers rely on.
- **Parse `go test -v` output for SKIP lines in the justfile:** rejected. It is fragile, it would also catch unrelated skips such as "node not available", and it keeps the rule outside the tests that own it.
- **Name the variable `APPKIT_REQUIRE_GUI`:** rejected. AGENTS.md forbids new appkit names.

### Expected consequence

With the probe fixed, the scenarios run and crash in `Destroy`. That is TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks). `just test-gui` goes from a false green to an honest red until that ticket lands. Neither `just ci` nor Forgejo CI runs `test-gui`, so the merge gate is unaffected.

### Out of scope, noted

GitHub's unix jobs run the GUI tests without `-v` and with cgo on, because `CGO_ENABLED` is unset, so their logs cannot show whether the scenarios ran. Whether GitHub should set `TUOHI_REQUIRE_GUI` belongs to TKT-01M3HWWRX (Decide tuohi's support tiers and make CI match them).
