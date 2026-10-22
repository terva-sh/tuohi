---
schema: 4
id: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
title: Support optional signed desktop self updates
type: epic
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/api
  - area/release
  - security
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
  branch: t3/self-update
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: dc693b2b1c26a19c6dd467df35d938759635c76b
  session: null
  claimed_at: 2026-10-22T21:24:25Z
  expires_at: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T21:24:25Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected planning, filing, grooming, promotion and autonomous implementation of self-updating desktop support on 2026-10-22. This explicitly authorizes promotion of this epic and its implementation children. Deliver an optional updater subpackage, preserving the root window library and its no-net/http/no-crypto/tls and cgo-free rules. Consumer owns transport, release channel, consent, state persistence, package-manager policy and UI. No repository settings, merge, signing credentials or public release publication are authorized.

### Decisions and scope
Use pinned Ed25519 public keys and a versioned signed manifest envelope; bind app/channel/platform/sequence/expiry and asset digest/size. A caller-supplied context-aware transport preserves existing dependency boundaries. Stage bounded verified payloads privately. Support portable executables on desktop targets and complete signed macOS app bundles, with explicit refusal for manager-owned installations. Use a helper entry point before normal consumer startup to wait for parent exit, perform recoverable replacement and relaunch, without globals or automatic process exit. Consumer shuts down normally and releases single-instance ownership first. Unsupported native/runtime evidence is reported honestly. API examples, release-producer documentation and meaningful fault/cancellation/integrity regressions are part of delivery.

## Acceptance criteria

- [ ] Implementation children complete with public docs/examples and meaningful security/lifecycle tests.
- [ ] CGO_ENABLED=0 validation and prescribed cross builds pass; internal PR review findings are addressed; no native evidence or merge/release permission is invented.

## Implementation plan

Implement children in dependency order on t3/self-update based on current origin/main dc693b2. Existing PRs #82–#84 remain untouched. Keep all rationale in these tickets and commit ticket updates as work proceeds. Review the complete implementation through small internal PRs if platform changes warrant separation; do not merge/release.
