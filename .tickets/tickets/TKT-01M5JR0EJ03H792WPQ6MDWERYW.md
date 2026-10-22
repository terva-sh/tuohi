---
schema: 4
id: TKT-01M5JR0EJ03H792WPQ6MDWERYW
title: Document and demonstrate desktop update lifecycle integration
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/docs
  - area/api
  - area/ci
assignees: []
milestone: null
parent: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
origin: null
dependencies:
  - TKT-01M5JR0EE7560PQ05E78J8B445
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/updater-example
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 129b5376f3e7b52c938de7fa90cff5a93d508c40
  session: null
  claimed_at: 2026-10-22T21:45:09Z
  expires_at: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T21:54:03Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

Provide a reference public-API-only desktop consumer and release-producer instructions. Demonstrate the early helper entry point, pinned key/feed/current sequence, consumer HTTP transport with redirects/status/timeouts bounded, user consent/status/progress, normal App.Quit/Wait teardown, releasing single-instance ownership and scheduling restart. Do not add updater fields/globals to App or change existing default behavior. Explain package-manager handoff, signing-key custody/rotation limits, release sequencing, installation write permissions, rollback/recovery, platform guarantees and native evidence. Validate both modules with CGO_ENABLED=0, prescribed cross builds and relevant Linux native tests. Publish through normal Forgejo PR/review process, address every finding, and stop before merge/public release without further authorization.

## Acceptance criteria

- [x] An external-consumer example uses only public APIs and demonstrates graceful teardown/restart without automatically updating or quitting on startup.
- [x] Producer and consumer docs include signed metadata format, key/sequence/expiry rules, platform matrix and recovery limitations.
- [ ] Relevant tests, full just ci and prescribed cross builds pass; PR has exact-head/base review with dispositions.

## Implementation plan

Add example and producer/consumer docs, validate cgo-free both modules/all targets and relevant native scenarios, publish internal PR and request full exact-head/base review; record and fix findings.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T21:48:20Z

Consumer reference example implemented and focused headless tests compile/pass. It uses only public APIs, consumer HTTP transport, explicit user prepare/install actions, early helper handling, managed-by-default installation mode, compiled sequence, normal Quit/Wait and single-instance release, and a no-install signed staging GUI check. Remaining work is producer documentation/tooling, native Linux GUI validation and full final CI/review. PR #86 cleanup fix now has a clean full review at 1212b665 against 1d8d40e (run 93e873e2-de2e-46b6-a91b-6b0886edb931 / Actions #586). PR #87 review 2736 found a high issue with standalone helper copies losing bundle-relative dylibs; pause integration publication and fix that on the owning bundle branch, preserving all example work in this commit.

**agent:codex/t3-b903096c** at 2026-10-22T21:54:02Z

Public integration is implemented: examples/selfupdate has early helper dispatch, managed-by-default packaging choice, trusted configuration/public-key pinning, consumer HTTP transport with HTTPS-only bounded redirects/status/timeout/cancellation, signed download progress, explicit consent and Save/Schedule/Quit/Wait/close/release ordering. Its -check path stages a harmless in-memory signed release through the native bridge, never installs, and reacquires the released instance ID. examples/updatefeed is an offline Ed25519 seed signer that publishes nothing; its output roundtrips through the public consumer API and bounded staging. Producer docs cover build/sign/notarize/hash/sequence/expiry/key rotation/immutable uploads, consumer consent and result/recovery semantics, managed install modes and unavailable runtime evidence. Root API/defaults/import boundaries remain unchanged.

Full CGO_ENABLED=0 just ci passes for both modules, including all eight prescribed targets. Linux WebKitGTK 6.0 native suite including the new selfupdate smoke check passed; the 4.1 stack is still running when this note is written. Lower PRs are all clean at exact revisions: #85 https://git.local.sothr.com/terva-sh/tuohi/pulls/85 head 1d8d40e487df3c60a8d96d97ff8fd50eaa287259/base dc693b2b1c26a19c6dd467df35d938759635c76b, clean full review run daedd397-f228-445d-b4da-67fc25850afd (Actions #580); #86 https://git.local.sothr.com/terva-sh/tuohi/pulls/86 head 1212b665f036c6edea73a1d63f1530901a2789f8/base 1d8d40e, clean full review run 93e873e2-de2e-46b6-a91b-6b0886edb931 (Actions #586); #87 https://git.local.sothr.com/terva-sh/tuohi/pulls/87 head 6a96a31d29f4d01f9aa59ee0611670c3478caa9e/base 1212b665, clean full review run 229215f6-abdb-4c8d-a0c2-ec78b62d0c03 (Actions #592). Each finding was accepted/fixed with tests and individual PR/ticket dispositions. No native macOS/Windows/BSD execution, production signing/notarization, merge, tag, feed publication or public library release is claimed. Final integration PR review/checks remain to complete this ticket/epic.
