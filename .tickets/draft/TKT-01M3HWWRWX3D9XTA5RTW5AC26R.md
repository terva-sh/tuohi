---
schema: 4
id: TKT-01M3HWWRWX3D9XTA5RTW5AC26R
title: Finish renaming appkit to tuohi in prose, env vars, and names
type: chore
status: draft
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
dependencies: []
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
