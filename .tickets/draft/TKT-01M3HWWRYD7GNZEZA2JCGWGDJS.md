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
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-27T16:59:09Z
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

Under GTK3 on Wayland, starting an application writes icons and a hidden `.desktop` file under `~/.local/share` and may launch `kbuildsycoca` detached (`app_unix.go:185-300`), without the application asking. It exists so the compositor can show the application's icon.

A library embedded in someone else's program should not write into the user's home directory as a side effect of opening a window. Make it opt-in, or remove it in favour of the application installing its own desktop entry, which git-ticket-canvas plans to do anyway.

## Acceptance criteria

- [ ] Opening a window writes nothing under the user's home directory unless the application asks
