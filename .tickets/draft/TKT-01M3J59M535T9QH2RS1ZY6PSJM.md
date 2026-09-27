---
schema: 4
id: TKT-01M3J59M535T9QH2RS1ZY6PSJM
title: Quote desktop-entry Exec lines to the spec
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/autostart
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
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-27T19:25:58Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-92c88910
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

- [ ] Exec quoting doubles percent signs, quotes every reserved character, and applies the string-level backslash escape
- [ ] Table tests cover each reserved character, percent, backslash, spaces, and non-ASCII
- [ ] NOTICE says the quoting is rewritten
