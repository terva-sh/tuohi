---
schema: 4
id: TKT-01M5NYGV1KCRNJYJ3G8ZJ7QJ13
title: Release tuohi v0.3.1
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/release
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/tuohi-clipboard-image
  branch: t3/release-v0.3.1
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-clipboard-image
  commit: 2729be2bb671a776ae7484a87c536c8f6acec2a6
  session: null
  claimed_at: 2026-10-24T03:16:18Z
  expires_at: null
archive: null
created_at: 2026-10-24T03:15:56Z
updated_at: 2026-10-24T03:16:18Z
created_by:
  id: agent:claude-code/tuohi-clipboard-image
  name: ""
updated_by:
  id: agent:claude-code/tuohi-clipboard-image
  name: ""
extensions: {}
---

## Description

### What

Publish tuohi v0.3.1, a patch release carrying the Windows default-background
fix from TKT-01M5NSFRABVJ7H2561GVY304FN (Use the documented IID for
ICoreWebView2Controller2), merged in #104 as 2729be2.

### Why

The owner asked, on 2026-10-24, for the Windows fix to go out as a patch
after v0.3.0, and chose to cut v0.3.1 at once rather than wait for v0.4.0's
API work: the WebView2 data-directory field (TKT-01M5NSFRB4) and the
context-menu filter (TKT-01M5NWN4).

### Procedure

The same as v0.3.0 (TKT-01M5NSDCJ574MXR81G4P5CSSDC): reviewed notes in an
internal PR, merge, GitHub sync without force, native CI on the merge commit,
one annotated tag pushed to each forge, the tag workflow publishes, and a
proxy check. The owner approved the tag when choosing to cut v0.3.1 now.

## Acceptance criteria

- [ ] Release notes for v0.3.1 reviewed and merged
- [ ] The merged commit passes native cgo-free CI on GitHub
- [ ] One annotated v0.3.1 tag points to that commit on both forges
- [ ] The tag workflow publishes the GitHub release and proxy.golang.org serves v0.3.1
- [ ] Kuiske is told the version
