---
schema: 4
id: TKT-01M5JR0E89AAJXVDF2GNK33MTQ
title: Verify signed update metadata and stage bounded artifacts
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T21:36:25Z
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

- [x] Tampered, expired, wrong-app/channel/platform, rollback, malformed and oversized metadata/artifacts fail closed.
- [x] Cancellation, corrupt/truncated/oversized download and cleanup regressions pass without network dependencies.
- [x] Public API docs and external-consumer examples describe signing, trust and transport boundaries.

## Implementation plan

Read architecture/import restrictions; implement signed manifest parsing and immutable client/release/stage with transport injection. Add adversarial metadata and streamed artifact tests, then run cgo-free focused tests and document the API.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T21:28:56Z

Implemented the optional updater package with copied pinned Ed25519 keys, domain-separated signatures, strict/duplicate-safe bounded JSON, app/channel/platform/sequence/expiry checks, HTTPS URL validation, streamed bounded SHA-256 staging, progress and cancellation cleanup. Transport stays consumer-injected so the root/module HTTP/TLS import boundary is unchanged. Public package docs and an external-package API example are included. Focused CGO_ENABLED=0 tests passed, including tampering/rollback/wrong identity, oversized metadata/downloads, partial/corrupt content, cancellation and cleanup. All eight prescribed target cross builds passed. Version remains display-only; compiled sequence plus consumer durable highest-installed floor governs rollback. Expiry is rechecked before staging and will be rechecked by installation. Full native GUI and final review occur in integration.

**agent:codex/t3-b903096c** at 2026-10-22T21:36:24Z

done to in-progress: Address PR #85 exact-head review 2733 case-variant JSON field aliases

**agent:codex/t3-b903096c** at 2026-10-22T21:36:24Z

PR #85 full exact-head/base review 2733 (run 5cbce308-119d-41b1-839c-ad6bf45863cb, Actions #578), head 33ba190c86b958ad1a4a4e695cb1ce7c835b76f7 against dc693b2b1c26a19c6dd467df35d938759635c76b, found one medium issue: encoding/json accepts case-insensitive struct aliases despite DisallowUnknownFields. Accepted and fixed. The bounded token pass now checks every object key against the destination type's exact JSON field names before decoding; map keys remain free but duplicate-checked. Case aliases are rejected, not merely lowercased. Regressions cover envelope alias and alias duplicate, signed sequence alias and alias duplicate, and nested asset alias. CGO_ENABLED=0 just ci passed, including all prescribed cross builds and ticket strict check. Request a fresh full review after this substantive fix. Installation remains in the dependent branch, unaffected by this review scope.

## Summary

Implemented optional signed metadata verification and bounded streaming staging with public docs/examples and adversarial tests; cgo-free focused tests and all prescribed cross builds pass.
