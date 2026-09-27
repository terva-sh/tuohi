---
schema: 4
id: TKT-01M3HWWRYD7GNZEZA2JCGWGDJS
title: Stop writing desktop files on startup under GTK3 Wayland
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-27T19:26:49Z
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

Under GTK3 on Wayland, starting an application writes icons and a hidden `.desktop` file under `~/.local/share` and may launch `kbuildsycoca` detached (`app_unix.go:185-300`), without the application asking. It exists so the compositor can show the application's icon.

A library embedded in someone else's program should not write into the user's home directory as a side effect of opening a window. Make it opt-in, or remove it in favour of the application installing its own desktop entry, which git-ticket-canvas plans to do anyway.

## Acceptance criteria

- [ ] Opening a window writes nothing under the user's home directory unless the application asks

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

Make it opt-in, through an explicit call or field the app sets, and never on by default. It still runs only under GTK3 on Wayland. Other findings:

- **Name collisions.** `id` comes from `App.Name`, so two apps with the same name overwrite each other's files, and a user's own `.desktop` of that name is overwritten.
- **Zombie process.** `kbuildsycoca` is started and never waited on, so a zombie remains until exit.
- **No cleanup.** Nothing removes the files later.
- **Weak quoting.** `desktopQuote` escapes only `"`. Quoting is TKT-01M3J59M535T9QH2RS1ZY6PSJM (Quote desktop-entry Exec lines to the spec).

It lands with the package split, TKT-01M3J59M1H9PZ04J2C9JJZ7V13, which is why it depends on that ticket.
