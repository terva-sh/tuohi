---
schema: 4
id: TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG
title: GUI tests skip wherever bubblewrap is installed
type: bug
status: done
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
updated_at: 2026-09-27T18:06:49Z
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

- [x] The availability probe succeeds where bubblewrap can create a user namespace
- [x] just test-gui fails when a display is present and a GUI scenario skips
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

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T17:24:52Z

### Verified at b8b54ad (2026-09-27, Debian 13, CGO_ENABLED=0)

- **Probe:** `bwrap --unshare-user --ro-bind / / -- $(command -v true)` exits 0, so the probe now reports WebKit runnable.
- **Headless:** with no display, the scenarios still skip, and the message now gives the reason ("no display: ...").
- **Strict mode:** with a display, a forced read-only `XDG_RUNTIME_DIR`, and `TUOHI_REQUIRE_GUI=1`, `TestBridge` FAILs with the reason. Without the variable it SKIPs.
- **`just test-gui`:** it now runs both stacks and exits 1, naming both.
- **`just ci`:** passes.

### Acceptance criterion 3 is left unticked

The scenarios now run, but the first one, `bridgeScenario`, crashes the test binary in `View.Close` → `(*webview).Destroy`, so the other four never start. "The five scenarios run" becomes true only when TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks) lands, and its "just test-gui passes on both stacks" criterion covers it.

### A change not in the plan

`just test-gui` used to stop at the first failing stack, because the recipe runs under `bash -e`. With the probe fixed, that meant the 4.1 stack never ran. The recipe now collects failures and exits at the end.

**agent:claude-code/t3code-92c88910** at 2026-09-27T17:27:43Z

### Review disposition, PR #2, terva-review run e79f04b7 on da538f2

- **medium: Keep GUI probing behind the -short check. Fixed in cb1ef0f.** The finding was correct: `TestMain` called `guiAvailable()` before checking `testing.Short()`, so `-short` with a display ran bwrap and `ensureInit()`. It now checks `-short` first. Verified with a fake `bwrap` on PATH that records its calls: it is not called under `-short`, and it is called without it. `just ci` passes.

## Summary

Landed through Forgejo PR #2 (terva-sh/tuohi), branch `fix/gui-probe-bubblewrap`.

- **Probe:** `webkitRunnable` runs `bwrap --unshare-user --ro-bind / / -- $(command -v true)`, so it succeeds wherever bubblewrap can create a user namespace.
- **Skip reasons:** each GUI skip names its cause: no display, the bwrap failure and its output, a read-only `XDG_RUNTIME_DIR`, a library load error, or `-short`. `-short` never probes.
- **Strict mode:** `TUOHI_REQUIRE_GUI=1` turns a GUI skip into a failure. `just test-gui` sets it, and runs both WebKitGTK stacks before reporting which failed.
- **Review:** terva-review found one medium issue: `-short` ran the probe. It was fixed in cb1ef0f, and the re-review on 91eff44 is clean.

Criterion 3 stays unticked. The scenarios now run, but the first crashes the test binary in `Destroy`, so the other four never start. TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks) owns that, and its "just test-gui passes on both stacks" criterion covers it. The owner chose to close this ticket on that basis on 2026-09-27. Until that ticket lands, `just test-gui` is red.
