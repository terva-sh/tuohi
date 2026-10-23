---
schema: 4
id: TKT-01M5NH4HQ50W4KW26WXT4DYB01
title: Keep Darwin short reveal tests independent of GUI initialization
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-23T23:22:02Z
updated_at: 2026-10-23T23:22:02Z
created_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
extensions: {}
---

## Description

On native darwin/arm64, CGO_ENABLED=0 go test -short -run '^TestViewShowAgainReveals$' . panics at dispatchMain because TestMain skips native initialization and TestViewShowAgainReveals reveals a fake live engine, entering the uninitialized platform dispatcher. Reproduced unchanged at main 728218d48d300fd704c884342ec9009754d05b24 in an isolated detached baseline worktree as well as during the native menu/fullscreen upstream work.

This is a test-harness follow-up. Ordinary full native tests initialize AppKit first; do not infer that this failure comes from fullscreen or menu implementation. Preserve the headless test's intent without relying on a GUI scenario having initialized global state.

## Acceptance criteria

- [ ] The native CGO_ENABLED=0 short suite passes without GUI initialization.
