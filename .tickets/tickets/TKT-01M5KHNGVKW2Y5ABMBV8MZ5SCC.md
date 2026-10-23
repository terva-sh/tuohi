---
schema: 4
id: TKT-01M5KHNGVKW2Y5ABMBV8MZ5SCC
title: Release tuohi v0.2.0
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/release
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/release-v0.2.0
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: f022d8cd07bca41787b54386c612e6118287e2b2
  session: null
  claimed_at: 2026-10-23T04:53:05Z
  expires_at: null
archive: null
created_at: 2026-10-23T04:52:49Z
updated_at: 2026-10-23T04:53:05Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner explicitly requested cutting and publishing a release on 2026-10-23, after selecting completion of fullscreen policy and release readiness. Latest published version is v0.1.1. Target v0.2.0 for the new opt-in notification, tray, lifecycle, external-browser and desktop updater APIs; preserve existing defaults.

Follow docs/pr-reviews.md and the established release procedure from TKT-01M3HWWRZA18G11ZZ4QVY7RB31 (Release tuohi v0.1.0): reviewed internal main, synchronize GitHub main without force, inspect native cgo-free CI, then push one annotated tag separately to both forges and let the tag workflow gate source-only publication. Confirm the exact tag target, published GitHub release, and proxy.golang.org module availability. Do not publish a consumer update feed or signed consumer artifacts; this authorization is for the tuohi library release.

Finish PR #97 / TKT-01M3NSA8JEDJJEBA47SVA4EZ7T (Document why macOS keeps element fullscreen on) and inspect actual native TestElementFullscreenPreference execution before selecting the final candidate. Prepare reviewed release notes covering API additions, threading/lifetime defaults and capability limits, plus accurate acceptance evidence. TKT-01M5KG5ZYY361QYFCVNET7KA63 (Validate packaged desktop integration before release) retains the pending packaged macOS/signing and hardware checks; a library source release must not claim those passed. The owner has requested publication with these known platform-validation gaps documented rather than inferred away.

No existing release ticket covers this version. Prior v0.1.0 ticket is done. Existing v0.1.1 tag/release remain untouched. No force-push, retagging or credential/settings changes.

## Acceptance criteria

- [ ] Review and merge release notes, version rationale and accurate acceptance limits.
- [ ] Final mirrored source passes native cgo-free CI, including the macOS fullscreen preference regression.
- [ ] One annotated v0.2.0 tag points to the validated source on both forges.
- [ ] Gated tag CI publishes the source-only GitHub release and the Go module proxy serves v0.2.0.
- [ ] Record release evidence and notify the Kuiske consumer session.

## Implementation plan

Prepare a v0.2.0 source release because the new opt-in APIs expand the library substantially beyond v0.1.1; a patch label would understate that scope. Keep Go 1.26 and existing static consumers/defaults. Review the release notes and updated acceptance evidence in a small internal PR. Finish the already-reviewed fullscreen PR independently, inspect its actual macOS test result and the final mirrored native matrix, and resolve any failures before tagging. Push an annotated tag at that validated merged commit separately to origin and github; do not rewrite it. Let the existing tag CI gate GitHub publication. Verify the tag target, public release and module proxy/checksum resolution. Record post-publication outcomes through a normal reviewed ticket-only follow-up and send the tested version/evidence to Kuiske. Leave interactive packaged-macOS, production signing, Wayland/WSLg/Steam Frame and daemon/account acceptance honest and open; no consumer feed publication.
