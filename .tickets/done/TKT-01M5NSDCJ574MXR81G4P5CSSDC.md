---
schema: 4
id: TKT-01M5NSDCJ574MXR81G4P5CSSDC
title: Release tuohi v0.3.0
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
created_at: 2026-10-24T01:46:40Z
updated_at: 2026-10-24T02:39:45Z
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

- [x] Release notes for v0.3.0 reviewed and merged, with accurate validation limits
- [x] The merged release-notes commit passes native cgo-free CI on GitHub
- [x] One annotated v0.3.0 tag, approved by the owner, points to that commit on both forges
- [x] The tag workflow publishes the GitHub release and proxy.golang.org serves v0.3.0
- [x] Kuiske is told the version, the API and its limits

## Implementation plan

v0.3.0: minor bump for the new clipboard image API. This PR carries docs/releases/v0.3.0.md, the README pointer, the clipboard-image evidence in docs/desktop-acceptance.md, and a doc line that GTK clipboard reads can wait indefinitely, which Kuiske asked for. After merge: sync GitHub without force, check native CI on the merge commit, then ask the owner before pushing an annotated v0.3.0 tag to each forge. Follow-ups filed, not in this release: TKT-01M5NSFRAB (Controller2 IID), TKT-01M5NSFRB4 (WebView2 user data folder), TKT-01M5NSFRBX (cancellable clipboard reads).

## Notes

**agent:claude-code/tuohi-clipboard-image** at 2026-10-24T02:39:45Z

### Publication

- PR #102 merged as 4083fa914b2e394a109791b92387bba79ac20097 on Forgejo; its terva review was clean at f733848 (run 3fa9f134), and both CI jobs passed.
- GitHub main fast-forwarded debc890..4083fa9, without force. Native run 40054617865 on 4083fa9 passed every test, lint and build job; its release job skipped, as on a branch.
- The owner approved the tag. One annotated tag, v0.3.0 -> 4083fa9, was pushed separately to origin and github.
- Tag run 40054865744 passed and its "Release v0.3.0" job published https://github.com/terva-sh/tuohi/releases/tag/v0.3.0 (not draft, not prerelease) at 2026-10-24T02:38:41Z.
- proxy.golang.org serves v0.3.0 at 4083fa9 (refs/tags/v0.3.0). sum.golang.org: h1:vKd+T8uOYj/NkRoh6XxZ8rLw/Gdw8PAzqe8xJDZ5bBU=, go.mod h1:byWobGhSQfSLBumj/1xFVfZWWaAa/qcJPXaMYnxpTg8=. A clean module resolves `go get github.com/terva-sh/tuohi@v0.3.0`. The proxy's @latest still answered v0.2.0 straight after publication.
- Kuiske's session was sent the version, the checksums and the API limits. Its first session had restarted, so the message went to its new one.

## Summary

Published tuohi v0.3.0: annotated tag at 4083fa9 on both forges, GitHub release from tag run 40054865744, and proxy.golang.org and sum.golang.org serve it. The release adds clipboard.PasteImage and #100's macOS fixes. Kuiske was told to repin. Windows fixes for a patch release are next, starting with TKT-01M5NSFRAB.
