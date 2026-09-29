---
schema: 4
id: TKT-01M3HWWRYD7GNZEZA2JCGWGDJS
title: Stop writing desktop files on startup under GTK3 Wayland
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/desktop-entries
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-desktop
  commit: ff6e0c9380ada27351b08813c316bbfbf275319e
  session: null
  claimed_at: 2026-09-29T21:53:07Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-29T21:53:08Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Under GTK3 on Wayland, starting an application writes icons and a hidden `.desktop` file under `~/.local/share` and may launch `kbuildsycoca` detached (`app_unix.go:185-300`), without the application asking. It exists so the compositor can show the application's icon.

A library embedded in someone else's program should not write into the user's home directory as a side effect of opening a window. Make it opt-in, or remove it in favour of the application installing its own desktop entry, which git-ticket-canvas plans to do anyway.

## Acceptance criteria

- [x] Opening a window writes nothing under the user's home directory unless the application asks

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

Make it opt-in, through an explicit call or field the app sets, and never on by default. It still runs only under GTK3 on Wayland. Other findings:

- **Name collisions.** `id` comes from `App.Name`, so two apps with the same name overwrite each other's files, and a user's own `.desktop` of that name is overwritten.
- **Zombie process.** `kbuildsycoca` is started and never waited on, so a zombie remains until exit.
- **No cleanup.** Nothing removes the files later.
- **Weak quoting.** `desktopQuote` escapes only `"`. Quoting is TKT-01M3J59M535T9QH2RS1ZY6PSJM (Quote desktop-entry Exec lines to the spec).

It lands with the package split, TKT-01M3J59M1H9PZ04J2C9JJZ7V13, which is why it depends on that ticket.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:53:07Z

### Where it landed

`App.DesktopEntry` landed in ff6e0c9. It is a bool, false by default, and has no effect anywhere but GTK3 on Wayland. Only when it is set does `setAppIcon` call `installWaylandIdentity` (through `waylandIdentity`, the gate).

The other findings from the architecture review:
- **Name collisions and foreign entries.** An existing entry without the `X-Tuohi-Generated=true` key is not tuohi's. It is left alone with its icons, and its id is still advertised, so the window matches it.
- **Zombie.** `kbuildsycoca` is waited on in a goroutine, so it is reaped when it exits.
- **Weak quoting.** Exec and Name go through `internal/desktopentry` (TKT-01M3J59M535T9QH2RS1ZY6PSJM).
- **No cleanup.** Not done. The files stay after the app exits, and the App.DesktopEntry doc says so. Removing them at exit would make the icon vanish from the compositor's cache at every quit.

Rejected: removing the writer outright, in favour of the app installing its own entry. The owner chose opt-in on 2026-09-27.

### Verified

- `TestWaylandIdentityOptIn` sets `WAYLAND_DISPLAY` and a temporary `XDG_DATA_HOME`. It checks three things: nothing is written without the opt-in; the opt-in writes the entry, with the key; and nothing is written outside Wayland even with the opt-in.
- `TestWaylandIdentityLeavesForeignEntry` checks that a foreign entry is byte-identical afterwards, and that no icons are written beside it.
- Mutation controls: dropping the gate fails the opt-in test, and dropping the key check fails the foreign-entry test.
- `just ci` and `just test-gui` pass on both stacks.
