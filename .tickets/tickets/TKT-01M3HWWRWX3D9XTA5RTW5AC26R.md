---
schema: 4
id: TKT-01M3HWWRWX3D9XTA5RTW5AC26R
title: Finish renaming appkit to tuohi in prose, env vars, and names
type: chore
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
  - TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-29T21:13:08Z
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

Finish renaming appkit to tuohi everywhere a user or a reader sees it.

### Already done

The module path and the package name are `tuohi`, in the scaffolding commit.

### Remaining

- **Comments.** About 260 occurrences of "appkit" in Go comments, plus the README body below the fork preamble.
- **Environment variables.** `APPKIT_BACKEND` and `APPKIT_DEBUG` become `TUOHI_*`. Decide whether to accept the old names for a transition. Nothing has released under them, so probably not.
- **Autostart slug.** `defaultAutostartSlug = "appkit-app"`.
- **Upstream links.** Links to `github.com/malivvan/appkit`, and rename slips such as `github.com/ebiten/pure/pull/1` (`pure/syscall.go:58`). Some of these go away with `pure`.
- **Review labels.** "T5:", "(P1)", "R5", "E4/R1", "P2/E2" and similar, which are residue of the source's generation and mean nothing here.
- **Build names.** The Makefile's `appkit_*` build names, and its duplicate `build` target, which makes `make` warn.

Leave the macOS backend's references to Apple's AppKit framework alone: they are correct.

## Acceptance criteria

- [ ] No appkit name remains outside NOTICE, docs/provenance.md, and references to Apple's AppKit
- [ ] Environment variables are TUOHI_*
- [ ] No review-item labels remain in comments

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### More to fix, found by TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

- The package doc says "Package appkit" (`app.go:1`) and points to an AGENTS.md "Source layout" section that does not exist (`app.go:16`).
- The `App.ID` example is `com.github.malivvan.appkit` (`app.go:209`).
- `App.Wait`'s doc says to "call View.Run" (`app.go:514-516`), and Run is not public. `View.Dialog`'s doc says "Run has been called" (`view.go:1011`).
- A detached comment block repeats Dialog's doc (`view.go:1022-1032`).
- `App.Bind` and `View.Bind` carry near-identical 40-line docs (`app.go:256-297`, `view.go:753-795`). Keep one and link it.
- About 99 string literals say "appkit", mostly error prefixes such as `"appkit: ..."`. They are user-visible, so they belong in this ticket.
- About 40 review tags such as `(P1)`, `(R2/RE2/E4)`, `(R1(a)/RE4)` remain in comments outside `pure/`, 31 of them in non-test files.
