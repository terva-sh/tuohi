---
schema: 4
id: TKT-01M5NGSF5EPRVCVD0XSWHPJ6XM
title: Supplement missing native macOS menu commands
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-darwin
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:https://git.local.sothr.com/terva-sh/tuohi/pulls/100
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-10-23T23:15:59Z
updated_at: 2026-10-23T23:40:25Z
created_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
extensions: {}
---

## Description

Bundled AppKit launches can supply a placeholder main menu. tuohi currently skips installation whenever any main menu exists, leaving editing and lifecycle key equivalents absent. Supplement missing responder-chain Edit actions and Quit/Close commands without replacing host menus or actions. Quit must use App.Quit so the caller can join teardown; Close keeps normal window policy.

Source: Kuiske PR #50 https://git.local.sothr.com/terva-sh/kuiske/pulls/50, implementation 89d3aba3c5c6047eaeb23e34f3820f08344f3ded, merge ba1f8706718704b43d209926641c57b4dcd7e423; consumer tuohi v0.2.0 (cebf4c0750bd1cd5618268e0b2e13925e53c350a). Kuiske's isolated macOS acceptance covered missing/existing menus, real close/Quit dispatch and joined teardown. The owner selected this upstream work after Kuiske PR #51 merged c8226eabca125d91395c2c95712f8059b617f869.

Boundary: no clipboard payload path through Go, native keyboard interception, Matrix clipboard review/send policy or Kuiske-specific lifecycle policy. Preserve pure Go and host menu targets/key equivalents. Native acceptance uses isolated synthetic apps only.

## Acceptance criteria

- [x] Absent, placeholder and populated host menus retain existing objects, actions and key equivalents; supplementation is idempotent.
- [x] Missing Edit actions use nil responder targets; native editing works with synthetic clipboard data.
- [x] Native Quit returns App.Wait for joined cleanup and Close follows CloseRequested for framed and frameless windows.

## Definition of done

- [x] Required checks, isolated native macOS acceptance and exact-head terva review pass; PR records evidence and boundaries.

## Implementation plan

Reuse existing menu builders and targets, scan actions across the host menu tree, and add only missing standard commands. Retain host objects and avoid a public menu API. Add native synthetic menu-shape, responder dispatch and App.Wait teardown regression scenarios.

## Notes

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:23:50Z

Implemented private Darwin menu supplementation and fullscreen-aware Hide. No new public types or methods, cgo, Matrix policy, tray registration logic or Kuiske dependency changes. Host menu actions and custom-selector shortcuts are preserved. Frameless menu Close now follows CloseRequested instead of bypassing policy. Fullscreen notifications use a separate observer, preserving an embedding host's window delegate, and observer teardown removes native and Go registry references.

Native synthetic acceptance on darwin/arm64 macOS 26.6.2 (25G83), CGO_ENABLED=0: menu shapes (absent, empty, placeholder, populated, nested existing actions, custom host shortcuts), idempotence, native WKWebView editing, framed/frameless Cancel+Hide, native Quit/Wait with joined worker; fullscreen exit-before-hide, retained window/renderer identity, repeated close during exit, close during entry, reopen/host-loss equivalent Show, Raise during entry, quit during pending exit and close teardown. All new assertions passed in the full suite; a focused run also passed before the final custom-shortcut case was added.

Required non-test checks passed: vet in both modules including Darwin/Windows, gofmt, import restrictions, public-API examples/module tidiness, embedded JavaScript, eight cross-build targets in both modules and strict ticket check. Full just ci reached native tests; its only reported failure was the existing TestPermissions WKWebView error expectations (TypeError versus OverconstrainedError/denied). Baseline reproduction is being checked separately before disposition. A separate pre-existing -short initialization crash reproduces on unchanged main 728218d48d300fd704c884342ec9009754d05b24, filed as draft TKT-01M5NH4HQ50W4KW26WXT4DYB01 (Keep Darwin short reveal tests independent of GUI initialization); it is not selected implementation scope.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:24:22Z

Confirmed identical TestPermissions failure on unchanged main 728218d48d300fd704c884342ec9009754d05b24, native darwin/arm64 macOS 26.6.2, CGO_ENABLED=0. This is a baseline limitation; remaining native suite and examples are being run with that one existing assertion excluded.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:30:00Z

PR #100 is open at exact implementation head 52fa5f9be7378035c62a593f88749a63dd7d6b12, base 728218d48d300fd704c884342ec9009754d05b24. Exact-head focused native acceptance passed all three assertions in 12.110s using CGO_ENABLED=0 TUOHI_REQUIRE_GUI=1 TUOHI_TEST_MACOS_REGRESSIONS=1 go test -count=1 -run TestNative/MenuSupplement-or-FullscreenHide-or-TestMenuEditKeys -v . (the literal regex is recorded in the PR follow-up evidence). terva dispatch returned the documented unexpected-end-of-JSON error, but Actions #707 was confirmed running with matching PR/request-id; no duplicate dispatch. Parent notified to watch. Full native suite remains unclaimed: baseline permission failure is draft TKT-01M5NH8TV3APBN79DC6YN05D6J (Reconcile native macOS permission fixture with current WKWebView); a later run also exposed pre-existing-scenario refcount/activation/focus instability before the new scenarios.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:32:33Z

Review dispositions for terva review 2818, run d0fe18b9-d60b-4c34-9e64-2bb1e7656251, Actions #707, exact original head 52fa5f9be7378035c62a593f88749a63dd7d6b12/base 728218d48d300fd704c884342ec9009754d05b24: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34675.

HIGH (DidEnter host observer): accepted. Keep fullscreenEntering true until an unconditional later main-queue turn, even when no hide is pending at the instant tuohi receives DidEnter. Revalidate the window/observer lifetime and transition epoch, then inspect the current hide intent. Later host observers can now request Hide or Cancel+Hide during real notification delivery without toggling prematurely or latching an exit that AppKit rejected. A separate epoch prevents a stale completion from changing a later native transition.

MEDIUM (actionable Edit/Window leaf): accepted. ensureSubmenu now skips matching actionable leaves and creates/reuses a separate submenu for the missing commands. Existing host leaf action, target, key equivalent and modifiers remain intact. Native acceptance checks both titled leaves and supplementation idempotence.

Focused native command (this literal command supersedes the abbreviated pseudo-regex in the preceding evidence note):
CGO_ENABLED=0 TUOHI_REQUIRE_GUI=1 TUOHI_TEST_MACOS_REGRESSIONS=1 go test -count=1 -run 'TestNative(MenuSupplement|FullscreenHide)|TestMenuEditKeys' -v .

The fixed working tree passes all three native assertions in 15.812s on macOS 26.6.2 arm64. The fullscreen cases forward the real DidEnter notification to tuohi before invoking the synthetic host action while delivery is still active, making the ordering deterministic without assuming NSNotificationCenter ordering. Both direct Hide and RequestClose with Cancel+Hide are covered. Vet, formatting, public-example/import checks, eight-target cross builds in both modules and strict ticket validation pass after the fixes. A fresh full terva review will be requested after pushing the substantive changes.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:36:39Z

Substantive review fixes are committed and pushed as d39030fab606e0fd74eac57c184f3d6123e3938d. Exact-head focused native acceptance passes all three assertions in 15.360s with the literal command recorded in the preceding note. Regression proof using the extended test file on the prior 52fa5f9 implementation fails both actionable-leaf preservation and the late-DidEnter host Hide case (enter=6, exit=5, pending=true). Disposition/evidence comment: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34678.

Fresh FULL review (carry=false) requested and confirmed as Actions #709, request native-macos-d39030f-full-review; CI is #708. The parent owns watching and merger decisions. Original protected Linux GUI gate #706 failed; its failing test/log is still unavailable. Parent's isolated emulated linux/amd64 Podman reproduction stopped at a local user-namespace/bubblewrap limitation, which establishes no Forge runner cause. Source changes remain Darwin-only except comments in view.go; no Linux preflight/source change is warranted by the local environment failure. No merge or release performed.

The clean actual upstream checkout is /Users/drewshort/.local/state/agent-scratch/tuohi-native-20261023/repo on agent/native-macos-menu-fullscreen. Staged orientation: /Users/drewshort/.local/state/agent-handoffs/tuohi/2026-10-23-native-menu-fullscreen-upstream.md. Keep Kuiske v0.2.0 pin and wrappers until a separately authorized upstream release and consumer follow-up.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:40:25Z

Implementation evidence is complete on head `d39030fab606e0fd74eac57c184f3d6123e3938d`, base `728218d48d300fd704c884342ec9009754d05b24`.

- All three exact-head statuses are success: [CI #708](https://git.local.sothr.com/terva-sh/tuohi/actions/runs/708) headless/vet/cross-build and GUI scenarios on both WebKitGTK stacks, plus terva-review/code.
- [Fresh full clean review](https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34677), run `35a84e43-d5c6-40ef-8367-45e0b0662cd8`, [Actions #709](https://git.local.sothr.com/terva-sh/tuohi/actions/runs/709), explicitly resolves both review 2818 findings with no new findings. [Dispositions and regression evidence](https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34678) remain applicable.
- Exact-head isolated macOS 26.6.2 arm64 acceptance passed all three native assertions in 15.360s with `CGO_ENABLED=0 TUOHI_REQUIRE_GUI=1 TUOHI_TEST_MACOS_REGRESSIONS=1 go test -count=1 -run 'TestNative(MenuSupplement|FullscreenHide)|TestMenuEditKeys' -v .`. Extended tests fail both reviewed defects on the old implementation and pass on d39030f.

Earlier CI #706 Linux failure remains unattributed. The parent's independent emulated linux/amd64 Podman diagnostic initially hit a bubblewrap namespace limitation. With the WebKit sandbox deliberately disabled only in the disposable container, both engines passed maximize, the root GUI package (6.0: 57.127s; 4.1: 70.519s), and examples; updater failed on unsupported process watch under emulation, with an additional GTK6 Xvfb cleanup error. This is not a full-suite or CI-equivalent pass and establishes no cause for #706. No Linux source/preflight changes or further repeats were made. Full local macOS suite is also not claimed: separately ticketed baseline failures remain as documented.

Closing and releasing the selected menu/fullscreen tickets with all acceptance criteria earned. The final commit will touch only `.tickets/`; review carry will be requested under repository rules. Parent owns final-head gate readiness and merge. No merge, mirror sync, release, or Kuiske pin/wrapper change is authorized here. A separately authorized upstream release containing the eventual merge must precede the consumer version/pin follow-up; no release number is assumed.

PR closeout evidence: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34693

## Summary

Implemented and accepted in PR #100 on d39030fab606e0fd74eac57c184f3d6123e3938d; both CI gates and full exact-head terva review pass. Native synthetic regression acceptance passes, including both prior review findings. Evidence: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34693. Final ticket-only closeout uses review carry. Parent owns merge/release; Kuiske pin and wrappers remain unchanged.
