---
schema: 4
id: TKT-01M5JR0E89AAJXVDF2GNK33MTQ
title: Verify signed update metadata and stage bounded artifacts
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/api
  - security
assignees: []
milestone: null
parent: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
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

Implement the optional updater public API and versioned Ed25519 signed release metadata. Validate pinned key identity, signature before payload interpretation, app/channel/OS/architecture, expiry, monotonic sequence, HTTPS URLs, signed digest/size and resource limits. Copy caller configuration and expose immutable verified release/stage handles. Transport is a context-aware consumer callback returning a stream: tuohi does not import HTTP/TLS. Reject malformed/ambiguous JSON, unsupported formats and rollback metadata. Stage privately with streaming verification, cancellation, cleanup and revalidation before installation. Consumer supplies compiled current sequence and persists accepted installed sequence; there is no hidden global updater state.

## Acceptance criteria

- [ ] Tampered, expired, wrong-app/channel/platform, rollback, malformed and oversized metadata/artifacts fail closed.
- [ ] Cancellation, corrupt/truncated/oversized download and cleanup regressions pass without network dependencies.
- [ ] Public API docs and external-consumer examples describe signing, trust and transport boundaries.

## Implementation plan

Read architecture/import restrictions; implement signed manifest parsing and immutable client/release/stage with transport injection. Add adversarial metadata and streamed artifact tests, then run cgo-free focused tests and document the API.
