---
schema: 4
id: TKT-01M5JR0EJ03H792WPQ6MDWERYW
title: Document and demonstrate desktop update lifecycle integration
type: task
status: done
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

Provide a reference public-API-only desktop consumer and release-producer instructions. Demonstrate the early helper entry point, pinned key/feed/current sequence, consumer HTTP transport with redirects/status/timeouts bounded, user consent/status/progress, normal App.Quit/Wait teardown, releasing single-instance ownership and scheduling restart. Do not add updater fields/globals to App or change existing default behavior. Explain package-manager handoff, signing-key custody/rotation limits, release sequencing, installation write permissions, rollback/recovery, platform guarantees and native evidence. Validate both modules with CGO_ENABLED=0, prescribed cross builds and relevant Linux native tests. Publish through normal Forgejo PR/review process, address every finding, and stop before merge/public release without further authorization.

## Acceptance criteria

- [x] An external-consumer example uses only public APIs and demonstrates graceful teardown/restart without automatically updating or quitting on startup.
- [x] Producer and consumer docs include signed metadata format, key/sequence/expiry rules, platform matrix and recovery limitations.
- [x] Relevant tests, full just ci and prescribed cross builds pass; PR has exact-head/base review with dispositions.

## Implementation plan

Add example and producer/consumer docs, validate cgo-free both modules/all targets and relevant native scenarios, publish internal PR and request full exact-head/base review; record and fix findings.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T21:48:20Z

Consumer reference example implemented and focused headless tests compile/pass. It uses only public APIs, consumer HTTP transport, explicit user prepare/install actions, early helper handling, managed-by-default installation mode, compiled sequence, normal Quit/Wait and single-instance release, and a no-install signed staging GUI check. Remaining work is producer documentation/tooling, native Linux GUI validation and full final CI/review. PR #86 cleanup fix now has a clean full review at 1212b665 against 1d8d40e (run 93e873e2-de2e-46b6-a91b-6b0886edb931 / Actions #586). PR #87 review 2736 found a high issue with standalone helper copies losing bundle-relative dylibs; pause integration publication and fix that on the owning bundle branch, preserving all example work in this commit.

**agent:codex/t3-b903096c** at 2026-10-22T21:54:02Z

Public integration is implemented: examples/selfupdate has early helper dispatch, managed-by-default packaging choice, trusted configuration/public-key pinning, consumer HTTP transport with HTTPS-only bounded redirects/status/timeout/cancellation, signed download progress, explicit consent and Save/Schedule/Quit/Wait/close/release ordering. Its -check path stages a harmless in-memory signed release through the native bridge, never installs, and reacquires the released instance ID. examples/updatefeed is an offline Ed25519 seed signer that publishes nothing; its output roundtrips through the public consumer API and bounded staging. Producer docs cover build/sign/notarize/hash/sequence/expiry/key rotation/immutable uploads, consumer consent and result/recovery semantics, managed install modes and unavailable runtime evidence. Root API/defaults/import boundaries remain unchanged.

Full CGO_ENABLED=0 just ci passes for both modules, including all eight prescribed targets. Linux WebKitGTK 6.0 native suite including the new selfupdate smoke check passed; the 4.1 stack is still running when this note is written. Lower PRs are all clean at exact revisions: #85 https://git.local.sothr.com/terva-sh/tuohi/pulls/85 head 1d8d40e487df3c60a8d96d97ff8fd50eaa287259/base dc693b2b1c26a19c6dd467df35d938759635c76b, clean full review run daedd397-f228-445d-b4da-67fc25850afd (Actions #580); #86 https://git.local.sothr.com/terva-sh/tuohi/pulls/86 head 1212b665f036c6edea73a1d63f1530901a2789f8/base 1d8d40e, clean full review run 93e873e2-de2e-46b6-a91b-6b0886edb931 (Actions #586); #87 https://git.local.sothr.com/terva-sh/tuohi/pulls/87 head 6a96a31d29f4d01f9aa59ee0611670c3478caa9e/base 1212b665, clean full review run 229215f6-abdb-4c8d-a0c2-ec78b62d0c03 (Actions #592). Each finding was accepted/fixed with tests and individual PR/ticket dispositions. No native macOS/Windows/BSD execution, production signing/notarization, merge, tag, feed publication or public library release is claimed. Final integration PR review/checks remain to complete this ticket/epic.

**agent:codex/t3-b903096c** at 2026-10-22T21:57:47Z

Both complete Linux GUI suites passed with CGO_ENABLED=0: WebKitGTK 6.0 and 4.1, including the signed-staging/normal-shutdown/instance-release example. The final consumer additionally displays the retained previous update outcome through ReadResult without trusting the record's sequence or automatically deleting recovery backups; that final source passed the native example check again on both stacks. Full just ci/all eight cross builds passed after merging the bundle overlap guard. PR #87 is clean again at exact head 3f5e4024f383622564810f4e03360c084d1df39e/base 1212b665, full run 87529304-886c-4b6e-93c9-479de782dee8 / Actions #598. PR #88's initial example/docs source was clean at ff0002910ef08e518ab350ed9c173784da24bf64/base 6a96a31d, full run 0b843e3e-a8b3-4214-a246-3f060ae85b2d / Actions #596; it had no findings. The updated integration head/base needs a fresh full review after pushing this source commit. No native non-Linux execution or publication/merge is inferred. Final task completion awaits that exact-revision review and checks.

**agent:codex/t3-b903096c** at 2026-10-22T22:00:32Z

Final validation improvement: portable subprocess restart/cancellation regressions are now enabled on every supported native OS rather than permanently skipped outside Linux. Windows uses an .exe target; the relaunched consumer stays alive until the test has opened a stable lifetime watch and explicitly releases it, so test cleanup waits for image release. This permits the repository's normal post-merge native macOS/Windows runners to exercise real replacement/restart without credentials. The tests passed three repetitions on Linux and compile for Darwin and Windows. Updated full CGO_ENABLED=0 just ci/all eight targets passes. This does not manufacture non-Linux execution evidence: successful native production bundle installation and platform signing/notarization acceptance remain owner/platform-runner release checks. Final full exact-head/base review will be requested after this test change; ticket completion follows its disposition.

**agent:codex/t3-b903096c** at 2026-10-22T22:07:33Z

Final integration source bd63b0a merges GUI harness fix 3f67b4d from PR #87. The updater example/native-test source f26f14d received clean full review 4fa95d92-d59c-4cda-9130-2fe448f47470, Actions #602, at exact base 3f5e402; hosted CI #603 passed. New integration CGO_ENABLED=0 just ci passes all eight targets. Root GUI fixes passed full just test-gui on both Linux WebKitGTK 6.0 and 4.1; the final selfupdate example separately passed both native stacks before this test-only merge. No native macOS/Windows/BSD runtime evidence is claimed. Remaining work: fresh exact-head/base reviews/checks on PRs #87/#88, then close the integration and GUI-validation tickets and epic, commit ticket records, and carry the review. No merge or public release.

**agent:codex/t3-b903096c** at 2026-10-22T22:10:38Z

Integrated PR #87 fix 643ba16553867276b3be2ecb6757f18ad4a2b084 for review 2741 high finding: bundle/stage overlap now uses filesystem identity rather than case-sensitive string containment. Complete cgo-free ci/all eight targets and Linux updater tests passed on the fix; case-alias test skips truthfully on this case-sensitive volume, Darwin and Windows test binaries compile, native macOS remains unavailable. The bundle ticket is reopened until the fresh review/check cycle is complete. The GUI harness ticket is complete; review 2741 raised no defect in its changes. Request a fresh full exact-head/base review for this integrated example head after publication.

**agent:codex/t3-b903096c** at 2026-10-22T22:29:01Z

Selected updater work is implemented and published as an internal PR stack; no merge, tag, public feed or release publication was performed. The last complete tested integration source is 96ce233d0af58d9884729450ce00c9e85f7964a7. All three checks are green on every PR at the following exact revisions:

- PR #85 https://git.local.sothr.com/terva-sh/tuohi/pulls/85 — head 1d8d40e487df3c60a8d96d97ff8fd50eaa287259, base dc693b2b1c26a19c6dd467df35d938759635c76b; clean full review daedd397-f228-445d-b4da-67fc25850afd (Actions #580), CI #581.
- PR #86 https://git.local.sothr.com/terva-sh/tuohi/pulls/86 — head 1212b665f036c6edea73a1d63f1530901a2789f8, base 1d8d40e487df3c60a8d96d97ff8fd50eaa287259; clean full review 93e873e2-de2e-46b6-a91b-6b0886edb931 (#586), CI #587.
- PR #87 https://git.local.sothr.com/terva-sh/tuohi/pulls/87 — head 0b1c1d9f728bdec67b63bacedba4cdb9458c0880, base 1212b665f036c6edea73a1d63f1530901a2789f8; clean full review ab81960a-1775-4ec9-91f4-3f0d1ede22dc (#616), CI #615.
- PR #88 https://git.local.sothr.com/terva-sh/tuohi/pulls/88 — head 96ce233d0af58d9884729450ce00c9e85f7964a7, base 0b1c1d9f728bdec67b63bacedba4cdb9458c0880; clean full review 24f3f522-7764-499a-a4de-ac9419f696e7 (#617), CI #618.

All actionable review findings were individually dispositioned and fixed: signed-JSON case aliases (1d8d40e), partial helper cleanup/retry (1212b665), preserved helper bundle/dylib layout (6a96a31), and filesystem-identity protection against case-aliased overlapping bundle stages (643ba16). GUI timing/diagnostic improvements are in 3f67b4d and 0b1c1d9. Both modules pass CGO_ENABLED=0 just ci/all eight prescribed target builds and both native Linux WebKitGTK suites. Real subprocess replacement/restart/cancellation executes on Linux and now compiles for other native runners; complete signed-bundle installation and macOS/Windows/BSD native execution remain unavailable. The real case-alias regression skips on this case-sensitive Linux volume; no native case-insensitive-volume execution is claimed.

TKT-01M5JVN8Z0XHCDRPRRHPGJXHF1 — Investigate intermittent GTK4 permission test stalls preserves one historical unreproduced permissions stall as a separate unselected draft. Four controlled CI-image runs and latest hosted checks pass. The original stall is not declared fixed. This does not change the updater API, production permissions policy or known GTK3 empty-clipboard workaround.

Only ticket-store closure follows this source revision. Request review carry for the ticket-only final commit and verify its hosted checks before handing back. Native production signing/notarization and eventual platform acceptance remain consumer/owner release validation, not proof supplied by cross compilation.

## Summary

Published public-API-only consent/progress/graceful teardown/restart example, retained outcome display, offline manifest signer and producer/consumer recovery/security docs in PR #88. Integration source 96ce233 has clean exact-head/base full review #617 and all three hosted checks green (#618). Full cgo-free validation, all eight cross targets and both Linux native stacks pass. Other native runtimes and production macOS signing acceptance are accurately documented as unavailable.
