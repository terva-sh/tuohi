---
schema: 4
id: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
title: Support optional signed desktop self updates
type: epic
status: done
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
claim: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T22:29:02Z
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

- [x] Implementation children complete with public docs/examples and meaningful security/lifecycle tests.
- [x] CGO_ENABLED=0 validation and prescribed cross builds pass; internal PR review findings are addressed; no native evidence or merge/release permission is invented.

## Implementation plan

Implement children in dependency order on t3/self-update based on current origin/main dc693b2. Existing PRs #82–#84 remain untouched. Keep all rationale in these tickets and commit ticket updates as work proceeds. Review the complete implementation through small internal PRs if platform changes warrant separation; do not merge/release.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T22:29:02Z

Selected updater work is implemented and published as an internal PR stack; no merge, tag, public feed or release publication was performed. The last complete tested integration source is 96ce233d0af58d9884729450ce00c9e85f7964a7. All three checks are green on every PR at the following exact revisions:

- PR #85 https://git.local.sothr.com/terva-sh/tuohi/pulls/85 — head 1d8d40e487df3c60a8d96d97ff8fd50eaa287259, base dc693b2b1c26a19c6dd467df35d938759635c76b; clean full review daedd397-f228-445d-b4da-67fc25850afd (Actions #580), CI #581.
- PR #86 https://git.local.sothr.com/terva-sh/tuohi/pulls/86 — head 1212b665f036c6edea73a1d63f1530901a2789f8, base 1d8d40e487df3c60a8d96d97ff8fd50eaa287259; clean full review 93e873e2-de2e-46b6-a91b-6b0886edb931 (#586), CI #587.
- PR #87 https://git.local.sothr.com/terva-sh/tuohi/pulls/87 — head 0b1c1d9f728bdec67b63bacedba4cdb9458c0880, base 1212b665f036c6edea73a1d63f1530901a2789f8; clean full review ab81960a-1775-4ec9-91f4-3f0d1ede22dc (#616), CI #615.
- PR #88 https://git.local.sothr.com/terva-sh/tuohi/pulls/88 — head 96ce233d0af58d9884729450ce00c9e85f7964a7, base 0b1c1d9f728bdec67b63bacedba4cdb9458c0880; clean full review 24f3f522-7764-499a-a4de-ac9419f696e7 (#617), CI #618.

All actionable review findings were individually dispositioned and fixed: signed-JSON case aliases (1d8d40e), partial helper cleanup/retry (1212b665), preserved helper bundle/dylib layout (6a96a31), and filesystem-identity protection against case-aliased overlapping bundle stages (643ba16). GUI timing/diagnostic improvements are in 3f67b4d and 0b1c1d9. Both modules pass CGO_ENABLED=0 just ci/all eight prescribed target builds and both native Linux WebKitGTK suites. Real subprocess replacement/restart/cancellation executes on Linux and now compiles for other native runners; complete signed-bundle installation and macOS/Windows/BSD native execution remain unavailable. The real case-alias regression skips on this case-sensitive Linux volume; no native case-insensitive-volume execution is claimed.

TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1 — Investigate intermittent GTK4 permission test stalls preserves one historical unreproduced permissions stall as a separate unselected draft. Four controlled CI-image runs and latest hosted checks pass. The original stall is not declared fixed. This does not change the updater API, production permissions policy or known GTK3 empty-clipboard workaround.

Only ticket-store closure follows this source revision. Request review carry for the ticket-only final commit and verify its hosted checks before handing back. Native production signing/notarization and eventual platform acceptance remain consumer/owner release validation, not proof supplied by cross compilation.

## Summary

Delivered optional cgo-free signed desktop updater through PRs #85–#88: authenticated bounded metadata/artifacts, staged portable install/restart with recovery, complete signed macOS bundle support, consent-driven consumer example and offline publisher tooling/docs. All selected implementation and supporting harness tickets are done; all four source PR heads have clean exact reviews and green hosted checks. Tested integration source 96ce233d0af58d9884729450ce00c9e85f7964a7. Native macOS/Windows/BSD runtime limits and one unreproduced permissions stall are retained honestly; the latter has a separate draft investigation. No merge or release performed.
