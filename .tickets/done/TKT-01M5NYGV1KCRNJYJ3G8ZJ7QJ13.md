---
schema: 4
id: TKT-01M5NYGV1KCRNJYJ3G8ZJ7QJ13
title: Release tuohi v0.3.1
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-24T03:15:56Z
updated_at: 2026-10-25T16:12:21Z
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

- [x] Release notes for v0.3.1 reviewed and merged
- [x] The merged commit passes native cgo-free CI on GitHub
- [x] One annotated v0.3.1 tag points to that commit on both forges
- [x] The tag workflow publishes the GitHub release and proxy.golang.org serves v0.3.1
- [x] Kuiske is told the version

## Notes

**agent:claude-code/tuohi-clipboard-image** at 2026-10-25T16:12:21Z

### Publication

- PR #105 merged as 3b505c4bd5408ebeee5ea7ed42d32fdedce8e551 on Forgejo. Its terva review was clean (run 813bfeaa), and both CI jobs passed.
- GitHub main was fast-forwarded 2729be2..3b505c4, without force. Native run 40210062649 on 3b505c4 passed every test, lint and build job.
- One annotated tag, v0.3.1 -> 3b505c4, was pushed separately to origin and to github. The owner approved it by choosing to cut v0.3.1 now.
- Tag run 40210304382 passed and published https://github.com/terva-sh/tuohi/releases/tag/v0.3.1 at 2026-10-25T16:11:30Z. It is not a draft.
- proxy.golang.org serves v0.3.1 at 3b505c4 (refs/tags/v0.3.1). The sum.golang.org hashes are h1:7QCaGZL85u0AEHXY7ey44j81pqRpWRqyKtWqQbCpaH8= and go.mod h1:byWobGhSQfSLBumj/1xFVfZWWaAa/qcJPXaMYnxpTg8=.
- Kuiske's session was sent the version, the checksums, and the frameless-transparency behaviour change.

## Summary

Published tuohi v0.3.1. The annotated tag is at 3b505c4 on both forges, tag run 40210304382 created the GitHub release, and proxy.golang.org and sum.golang.org serve it. The release fixes the WebView2 default background on Windows (#104). Kuiske was told to repin. Next is v0.4.0: TKT-01M5NSFRB4 (WebView2 data folder App field) and TKT-01M5NWN4 (context-menu filter).
