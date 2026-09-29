---
schema: 4
id: TKT-01M3J59M535T9QH2RS1ZY6PSJM
title: Quote desktop-entry Exec lines to the spec
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/autostart
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/desktop-entries
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-desktop
  commit: ff6e0c9380ada27351b08813c316bbfbf275319e
  session: null
  claimed_at: 2026-09-29T21:53:07Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T21:53:07Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

The XDG autostart writer builds the `.desktop` `Exec` line with `quoteExec` (`app_unix.go:638-660`). That function is copied from Wails and has three errors:

- `%` is never doubled, so an argument such as `50%` or `%u` is read as a field code;
- tokens containing `; & | < > ' ( ) * ? # ~` are left unquoted, although the Desktop Entry spec requires quotes;
- the `.desktop` string-level backslash escape is not applied on top of the `Exec` escape, so a backslash does not survive.

The GTK3 Wayland identity writer's `desktopQuote` (`app_unix.go:422-432`) escapes only `"`.

### Fix

Rewrite `Exec` quoting to the Desktop Entry spec, with table tests covering each reserved character, `%`, backslash, spaces, and non-ASCII. Keep the Wails credit in NOTICE for what remains derived, and say in NOTICE that the quoting is rewritten.

## Acceptance criteria

- [x] Exec quoting doubles percent signs, quotes every reserved character, and applies the string-level backslash escape
- [x] Table tests cover each reserved character, percent, backslash, spaces, and non-ASCII
- [x] NOTICE says the quoting is rewritten

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T21:13:06Z

The owner included this in the 2026-09-30 run. It lands after the package split (TKT-01M3J59M1H9PZ04J2C9JJZ7V13) moves the autostart writer into `tuohi/autostart`, so it follows that ticket.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:53:07Z

### Where it landed, and why there

It landed in `internal/desktopentry` (1c602f8), so both writers of a desktop entry share one implementation: `tuohi/autostart`'s XDG backend and the root's GTK3 Wayland identity. An internal package is importable by the root and by the subpackages without either importing the other. Rejected: a copy in each package, which is how the two drifted apart before (`quoteExec` and `desktopQuote`).

- `Exec(args...)` quotes each argument by the Exec rules, then escapes the value as a string, so one literal backslash is written as four, as the spec says.
- `SplitExec` undoes both. Autostart's `desktopExecPath` now uses it, where it used to stop at the first quote even when that quote was escaped.
- `String` escapes a Name value.

### Verified

- Table tests pin the written form for every reserved character, `%`, `%u`, a backslash, spaces, tab, newline, non-ASCII, and an empty argument, and round-trip all of them through `SplitExec`, one at a time and together.
- `TestBuildDesktopEntryExec` reads a written autostart entry back to the exact executable and arguments.
- Mutation controls: removing the `%` doubling, or the string-level backslash escape, each fails six cases.
- `just ci` and `just test-gui` pass on both stacks. golangci-lint reports 0 issues for linux, darwin, windows, and netbsd.

`validateDesktopExecToken` still refuses control characters in autostart arguments, as a second guard. With the string-level escape in place, a newline could no longer inject a key anyway.
