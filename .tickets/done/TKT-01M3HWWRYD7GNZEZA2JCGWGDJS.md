---
schema: 4
id: TKT-01M3HWWRYD7GNZEZA2JCGWGDJS
title: Stop writing desktop files on startup under GTK3 Wayland
type: task
status: done
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
updated_at: 2026-09-29T23:18:24Z
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

**agent:claude-code/t3code-72958710** at 2026-09-29T22:13:11Z

Disposition for the terva-review findings at ecfccca (run 885f1a2c), fixed in 8cf15ca:

- **Accepted, high: "Check system-installed entries before creating a user entry".** The guard looked only in `XDG_DATA_HOME`. A user entry with the same id shadows a package's entry in `/usr/share`, so opting in would have hidden the package's entry. `installWaylandIdentity` now also checks `XDG_DATA_DIRS`, defaulting to `/usr/local/share:/usr/share` and ignoring relative entries as the Base Directory spec says. When a system entry exists it writes nothing. An entry and icons tuohi wrote before the package arrived are removed, so the package's entry shows through. `TestWaylandIdentitySystemEntry` covers both. Skipping the check leaves 3 files, and skipping the removal leaves 1; either way the test fails.
- **Accepted, medium: "Recognize the generated marker only as its own desktop-entry key".** `isGenerated` now accepts `X-Tuohi-Generated=true` only as a key of the `[Desktop Entry]` group. The same text in a comment, in another key's value, in another group or with `false` does not count (`TestIsGenerated`). The foreign-entry test's entry now carries the marker in a comment. Going back to the substring match makes that test fail.

The `App.DesktopEntry` doc and architecture.md say the guard covers system entries. `just ci` passes.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:16:52Z

Disposition for the terva-review finding at e6d120d (run c028a63a), fixed in 489df08:

- **Accepted, medium: "Find system entries whose desktop ID comes from a subdirectory".** `hasEntry` now walks the whole `applications` tree of a data directory and forms each file's id as the spec does, turning `vendor/app.desktop` into `vendor-app`. The walk covers every `XDG_DATA_DIRS` entry and the user's own directory. A subdirectory entry in the user's directory is someone else's too, because tuohi writes only at the top.
  - `TestWaylandIdentitySubdirEntry` covers both places, and checks that a different id in the same subdirectory does not count.
  - Matching the stem without the hyphen rule makes the test fail, leaving 2 files beside a system `vendor/app.desktop`.

`just ci` passes.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:19:44Z

Disposition for the terva-review finding at 5839755 (run d2daa612), fixed in e240630:

- **Accepted, medium: "Remove a generated entry when a foreign user entry takes its desktop ID".** A user subdirectory entry that shares the id is now handled like a system entry. tuohi's own `applications/<id>.desktop` and its icons are removed, and nothing is written again.
  - `TestWaylandIdentitySubdirEntry` now installs tuohi's entry first, adds the other entry, and installs twice more. The other entry must be untouched, and nothing of tuohi's may remain. It checks this for both the system and the user directory.
  - Going back to the previous order leaves 3 files beside the user's `vendor/app.desktop`, and the test fails.
  - The `App.DesktopEntry` doc now says tuohi's entry is removed once any such entry appears.

## Summary

Landed in #39 (merge aa5a263).

- The GTK3 Wayland desktop entry and icons are written only when App.DesktopEntry is set.
- An entry with the same id that tuohi did not write is left alone. That covers one in the user's directory, a system directory from XDG_DATA_DIRS, or a subdirectory of either (vendor/app.desktop is vendor-app). tuohi's own earlier entry and icons are removed once such an entry appears.
- tuohi recognises its own entries by the X-Tuohi-Generated key in the [Desktop Entry] group.
- kbuildsycoca is reaped when it exits.
- Three review rounds, each fixed and tested with mutations.
