---
schema: 4
id: TKT-01M3K439622PRJ1KHC91X4GCMQ
title: Fail GitHub CI when the Linux GUI scenarios do not run
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
assignees: []
milestone: v0.1.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T04:24:16Z
updated_at: 2026-09-28T04:24:16Z
created_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
updated_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
extensions: {}
---

## Description

### What

GitHub CI must fail when the Linux GUI scenarios do not run, as `just test-gui` does.

### Current behaviour

- **Linux.** The "GUI tests under Xvfb" step in `.github/workflows/ci.yml` runs `xvfb-run -a go test -count=1 -timeout 150s ./...`, with no `TUOHI_REQUIRE_GUI=1` and no `-v`.
  - If the GUI probe in `lib_unix_test.go` fails on a runner, every GUI scenario skips and the job still passes.
  - The log names no individual test, so a reader cannot tell whether a scenario ran.
- **How it was noticed.** After PR #12, the only evidence that `TestNavigationPolicy` ran on GitHub's Linux jobs was the step duration: 21 to 22 seconds, against 3 to 6 for the headless step. See the note on TKT-01M3HWWRT7X1RZZYY6KFEP0ERE.
- **macOS** always runs its scenarios unless `-short` is set, and has no probe to fail.
- **Windows** skips its scenarios when WebView2 is not available. Its log is verbose, so a skip is visible.

### Direction

- Set `TUOHI_REQUIRE_GUI=1` on the Linux Xvfb step, so a probe failure fails the job.
- Consider `-v`, or `go test -json` with a summary, so the log names each GUI test. At least print the probe's skip reason.
- Decide whether Windows should also fail when WebView2 is unavailable on a hosted runner, where it should always be present.

## Acceptance criteria

- [ ] The Linux Xvfb step fails when the GUI probe fails
- [ ] The GitHub log shows whether each GUI scenario ran
