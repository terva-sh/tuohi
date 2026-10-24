---
schema: 4
id: TKT-01M5NSDCJ574MXR81G4P5CSSDC
title: Release tuohi v0.3.0
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
  branch: t3/release-v0.3.0
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-clipboard-image
  commit: debc890bea1ef5c241b8e2a689fdda80464b3620
  session: null
  claimed_at: 2026-10-24T01:49:14Z
  expires_at: null
archive: null
created_at: 2026-10-24T01:46:40Z
updated_at: 2026-10-24T01:49:14Z
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

Prepare and publish tuohi v0.3.0, a source release for Kuiske, which needs
`clipboard.PasteImage` from a tagged version rather than a pseudo-version.

### Why

The owner asked on 2026-10-24 to get ready to cut a release for Kuiske to use,
after reporting that pasting an image now works for them. Since v0.2.0
(`cebf4c0`), `main` has gained:

- #100: native macOS menu supplementation and exiting fullscreen before
  hiding retained windows.
- #101: `clipboard.PasteImage`, `ImageLimits`, `ErrNoImage`,
  `ErrImageTooLarge` (TKT-01M5NJ6Q60PV4XPBTP9P7ZZ7B0, Read an image from the
  system clipboard).

New public API, so the version is a minor bump: v0.3.0.

### Procedure

As TKT-01M5KHNGVKW2Y5ABMBV8MZ5SCC (Release tuohi v0.2.0) did: reviewed
release notes and acceptance evidence in an internal PR; merge; sync GitHub
main without force; check native cgo-free CI on the merged commit; push one
annotated `v0.3.0` tag separately to both forges, only once the owner
approves the tag; let the tag workflow gate publication; confirm the tag
target, the GitHub release and proxy.golang.org; tell Kuiske. No retagging,
force-push, consumer feed or signed artifacts.

## Acceptance criteria

- [ ] Release notes for v0.3.0 reviewed and merged, with accurate validation limits
- [ ] The merged release-notes commit passes native cgo-free CI on GitHub
- [ ] One annotated v0.3.0 tag, approved by the owner, points to that commit on both forges
- [ ] The tag workflow publishes the GitHub release and proxy.golang.org serves v0.3.0
- [ ] Kuiske is told the version, the API and its limits

## Implementation plan

v0.3.0: minor bump for the new clipboard image API. This PR carries docs/releases/v0.3.0.md, the README pointer, the clipboard-image evidence in docs/desktop-acceptance.md, and a doc line that GTK clipboard reads can wait indefinitely, which Kuiske asked for. After merge: sync GitHub without force, check native CI on the merge commit, then ask the owner before pushing an annotated v0.3.0 tag to each forge. Follow-ups filed, not in this release: TKT-01M5NSFRAB (Controller2 IID), TKT-01M5NSFRB4 (WebView2 user data folder), TKT-01M5NSFRBX (cancellable clipboard reads).
