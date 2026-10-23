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
updated_at: 2026-10-23T05:13:13Z
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

Finish PR #97 / TKT-01M3NSA8JEDJJEBA47SVA4EZ7T (Document why macOS keeps element fullscreen on) and inspect actual native TestElementFullscreenPreference execution before selecting the final candidate. Prepare reviewed release notes covering API additions, threading/lifetime defaults and capability limits, plus accurate acceptance evidence. TKT-01M5KG5ZYY361QYFCVNET7KA63 (Validate packaged desktop integration before release) retains the pending packaged macOS/signing and hardware checks; a library source release must not claim those passed. The publication plan discloses these platform-validation gaps; the release request does not establish that pending acceptance passed.

No existing release ticket covers this version. Prior v0.1.0 ticket is done. Existing v0.1.1 tag/release remain untouched. No force-push, retagging or credential/settings changes.

## Acceptance criteria

- [x] Review and merge release notes, version rationale and accurate acceptance limits.
- [x] Final mirrored source passes native cgo-free CI, including the macOS fullscreen preference regression.
- [x] One annotated v0.2.0 tag points to the validated source on both forges.
- [ ] Gated tag CI publishes the source-only GitHub release and the Go module proxy serves v0.2.0.
- [ ] Record release evidence and notify the Kuiske consumer session.

## Implementation plan

Prepare a v0.2.0 source release because the new opt-in APIs expand the library substantially beyond v0.1.1; a patch label would understate that scope. Keep Go 1.26 and existing static consumers/defaults. Review the release notes and updated acceptance evidence in a small internal PR. Finish the already-reviewed fullscreen PR independently, inspect its actual macOS test result and the final mirrored native matrix, and resolve any failures before tagging. Push an annotated tag at that validated merged commit separately to origin and github; do not rewrite it. Let the existing tag CI gate GitHub publication. Verify the tag target, public release and module proxy/checksum resolution. Record post-publication outcomes through a normal reviewed ticket-only follow-up and send the tested version/evidence to Kuiske. Leave interactive packaged-macOS, production signing, Wayland/WSLg/Steam Frame and daemon/account acceptance honest and open; no consumer feed publication.

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T04:55:55Z

Owner requested release publication explicitly in this session. Target v0.2.0 for the additive desktop API expansion since v0.1.1; retain Go 1.26. Prepared docs/releases/v0.2.0.md and corrected the stale README first-release claim. Recorded real Kuiske panel acceptance while preserving unexecuted packaged macOS, signing, account delivery and hardware checks. Read NOTICE/provenance; no imported third-party code or attribution change. Current main f022d8cd07bca41787b54386c612e6118287e2b2 passed all GitHub native jobs/builds in run 39902385484. PR #97 has a clean exact review but internal GTK4 GUI failed an existing missing-initial-load scenario, which must be dispositioned and rerun before merge. No tag created or release published yet.

**agent:codex/t3-b903096c** at 2026-10-23T05:00:49Z

PR #98 clean full review 38be59c3-b741-4b33-b4f8-75ef37e1d03e at a181417/base f022d8c found no issues. Headless/cross-build CI passed; GTK4 GUI failed camera permission setup with timeout at navigation, while the other two permission cases, GTK3 and remaining packages/examples passed. Recorded exact recurrence under TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1 (Investigate intermittent GTK4 permission test stalls). Release notes now disclose unresolved GTK4 test navigation stalls without asserting a shared cause or production correction. Full local cgo-free just ci for this docs/ticket branch passed; the unchanged Linux runtime also passed a full local just test-gui repeat from the fullscreen branch. Fresh full review/CI requested for updated notes; publication remains pending the gates in this ticket. No tag created.

**agent:codex/t3-b903096c** at 2026-10-23T05:02:38Z

PR #97 merged internally and mirrored as 960bb41533cb5a05b7763273d3202dc074df3878 after all three checks passed at a7ca22122d5843da859922055d04c2278b914b91 against f022d8cd07bca41787b54386c612e6118287e2b2. Clean full review ea691c31-76f0-4ad3-826f-8edec7ef35be covered runtime head 14d2d9d; successful review carry verified the later commit touches only its ticket. Strict local full GUI repeat also passed both Linux stacks/modules and the real-WM regression. Incorporated merged main into the release-preparation branch without rewriting history. Fresh full review/CI required for PR #98 at its new base. Native macOS TestElementFullscreenPreference and full final-source native matrix remain publication gates; fullscreen ticket remains in progress pending actual execution. No tag or release yet.

**agent:codex/t3-b903096c** at 2026-10-23T05:09:32Z

PR #98 merged and mirrored as cebf4c0750bd1cd5618268e0b2e13925e53c350a after clean exact head 4d288e1b80c17c347e138a1d2022d7cc78b83bc3/base 960bb41533cb5a05b7763273d3202dc074df3878 review 454febf9-4c34-4bbf-a34e-bda5f04c23c7 and all three checks. PR #97 native macOS TestElementFullscreenPreference passed (not skipped), job 120250200321/run 39903533841. Final candidate is merged source cebf4c0; full native run 39904111530 is queued/in progress and must pass before tagging. Release-notes and publication authorization are complete; no tag created yet. Post-publication ticket records are prepared on t3/record-v0.2.0-release, not part of the planned candidate; any tag command must explicitly target the validated candidate SHA rather than this evidence branch HEAD. All packaged-app/hardware acceptance gaps remain in their owning ticket and release notes.

**agent:codex/t3-b903096c** at 2026-10-23T05:12:45Z

Final source cebf4c0750bd1cd5618268e0b2e13925e53c350a passed every required native test/lint/build job in GitHub run 39904111530; its branch release job correctly skipped. Actual macOS job 120252109863 log contains --- PASS: TestElementFullscreenPreference (not SKIP). Native Linux GTK3/GTK4 on x64/ARM, Windows, macOS, 19-target example builds and lint all passed with the workflow CGO_ENABLED=0 setting. Pushed annotated v0.2.0 separately to origin and github; both report identical tag object 5ab583744291f2f3dd5b8be56ef7a1dbaf18b9a8 peeling to cebf4c0. No tags were rewritten. Gated tag/publication run 39904422070 is queued/in progress; the GitHub release is not yet claimed published. Public proxy/checksum verification is in progress. Native/pre-tag and identical annotated-tag criteria satisfied; publication/consumer notification remain open. Evidence branch is a post-release record, not the tag target.

**agent:codex/t3-b903096c** at 2026-10-23T05:13:13Z

Public module verification succeeded using GOPROXY=https://proxy.golang.org and GOSUMDB=sum.golang.org: go mod download -json github.com/terva-sh/tuohi@v0.2.0 returned origin hash cebf4c0750bd1cd5618268e0b2e13925e53c350a, Ref refs/tags/v0.2.0, module sum h1:qGgLFgL9+hCOg9r+PO/lhqyBif0WEsc/E5JNPtKyoUo= and go.mod sum h1:byWobGhSQfSLBumj/1xFVfZWWaAa/qcJPXaMYnxpTg8=. This establishes public proxy/checksum availability; gated GitHub release publication is still pending tag run 39904422070, so AC4 remains open.
