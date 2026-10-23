---
schema: 4
id: TKT-01M5NGSF6XTGKC5TMZ7KQ6SRHM
title: Exit macOS fullscreen before hiding retained windows
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
updated_at: 2026-10-23T23:41:53Z
created_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
updated_by:
  id: agent:codex/tuohi-native-20261023
  name: ""
extensions: {}
---

## Description

Hiding a fullscreen NSWindow with orderOut leaves a black dedicated Space. View.Hide must wait for AppKit's completed fullscreen exit, retaining the same window and renderer. Track entry/exit, coalesce repeated hides, cancel pending hide on reveal/quit/close, and defer a toggle requested during DidEnter until the callback unwinds. Native visibility notifications during transition must not cancel an accepted hide.

Source: Kuiske PR #50 https://git.local.sothr.com/terva-sh/kuiske/pulls/50, implementation 89d3aba3c5c6047eaeb23e34f3820f08344f3ded; tuohi v0.2.0. Isolated macOS acceptance established exit-before-hide/reopen, repeated close during exit, custom RequestClose during entry and quit during pending exit. The owner selected this generic upstream work after Kuiske PR #51 merged c8226eabca125d91395c2c95712f8059b617f869.

Boundary: fix View.Hide implementation, preserve other platform semantics and pure Go/CGO_ENABLED=0 support. Kuiske retains tray availability/host-loss policy and keep-running decisions; no tray-specific public API. Tests use disposable windows and synthetic content, never installed apps/accounts.

## Acceptance criteria

- [x] Hide waits for completed exit and preserves the same window and renderer on reopen.
- [x] Repeated close during exit and close during entry coalesce safely; DidEnter exit is deferred.
- [x] Reveal, host-loss reveal, quit and close invalidate pending hide; intermediate visible callbacks do not cancel it.

## Definition of done

- [x] Required checks, isolated native macOS acceptance and exact-head terva review pass; PR records evidence and boundaries.

## Implementation plan

Keep fullscreen transition state private to the Darwin engine and observe its own delegate notifications. Hide waits for DidExit; DidEnter schedules a guarded asynchronous exit. Show/Raise and lifecycle quitting cancel pending hides without responding to intermediate visibility. Exercise retained renderer identity and transition races in an isolated native scenario; preserve all other platforms.

## Notes

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:23:50Z

Implemented private Darwin menu supplementation and fullscreen-aware Hide. No new public types or methods, cgo, Matrix policy, tray registration logic or Kuiske dependency changes. Host menu actions and custom-selector shortcuts are preserved. Frameless menu Close now follows CloseRequested instead of bypassing policy. Fullscreen notifications use a separate observer, preserving an embedding host's window delegate, and observer teardown removes native and Go registry references.

Native synthetic acceptance on darwin/arm64 macOS 26.6.2 (25G83), CGO_ENABLED=0: menu shapes (absent, empty, placeholder, populated, nested existing actions, custom host shortcuts), idempotence, native WKWebView editing, framed/frameless Cancel+Hide, native Quit/Wait with joined worker; fullscreen exit-before-hide, retained window/renderer identity, repeated close during exit, close during entry, reopen/host-loss equivalent Show, Raise during entry, quit during pending exit and close teardown. All new assertions passed in the full suite; a focused run also passed before the final custom-shortcut case was added.

Required non-test checks passed: vet in both modules including Darwin/Windows, gofmt, import restrictions, public-API examples/module tidiness, embedded JavaScript, eight cross-build targets in both modules and strict ticket check. Full just ci reached native tests; its only reported failure was the existing TestPermissions WKWebView error expectations (TypeError versus OverconstrainedError/denied). Baseline reproduction is being checked separately before disposition. A separate pre-existing -short initialization crash reproduces on unchanged main 728218d48d300fd704c884342ec9009754d05b24, filed as draft TKT-01M5NH4HQ50W4KW26WXT4DYB01 (Keep Darwin short reveal tests independent of GUI initialization); it is not selected implementation scope.

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:24:23Z

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

**agent:codex/tuohi-native-20261023** at 2026-10-23T23:41:53Z

Historical Linux CI evidence correction/recurrence: the owner supplied the actual PR #100 Actions #706 GUI log for head 52fa5f9be7378035c62a593f88749a63dd7d6b12. The sole failure was WebKitGTK 6.0 root-package TestViewMethodsFromBinding at goroutine_calls_test.go:121:

```
View methods from a binding: got start=none eval=none navigate=none offUI=false request=none page=none ready=0; want start=/a eval=eval:/a navigate=/b offUI=true
```

The existing 20-second deadline observed no HTTP fixture request, no page-script report, and no Ready callback. This establishes the missing phases, not their cause. GTK6 maximize, updater and all examples passed; the GTK4.1 library, maximize and examples passed. GUI step: 1m48s; GTK6 root: 56.956s; GTK4.1 root: 35.369s. Actual runner evidence: https://git.local.sothr.com/terva-sh/tuohi/actions/runs/706/jobs/1 . Accessibility warnings appeared throughout and are not an established cause.

This matches existing unselected draft TKT-01M5JXMTDYT0V6SG8K7MNPN3VB (Diagnose GTK4 windows missing initial binding reports), including the PR #97/run #683 signature. Append there rather than file a duplicate or promote it. goroutine_calls_test.go and Linux runtime source are unchanged from the base by PR #100. Both CI engines passed on d39030fab606e0fd74eac57c184f3d6123e3938d (Actions #708), but Darwin fixes do not establish a correction for this intermittent Linux scenario. Preserve strict assertions/timeouts; no further Linux source changes or repeats are required for these native macOS tickets.

The earlier local bubblewrap/process-watch limitations arose only in disposable emulation, and permission failures only in separate macOS baseline testing. None was the cause shown in runner #706. This supersedes the earlier note that the historical runner failure was unavailable/unattributed as to test identity; its underlying cause remains unknown. Parent owns final-head gate readiness and merge.

PR evidence: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34695

## Summary

Implemented and accepted in PR #100 on d39030fab606e0fd74eac57c184f3d6123e3938d; both CI gates and full exact-head terva review pass. Native synthetic regression acceptance passes, including both prior review findings. Evidence: https://git.local.sothr.com/terva-sh/tuohi/pulls/100#issuecomment-34693. Final ticket-only closeout uses review carry. Parent owns merge/release; Kuiske pin and wrappers remain unchanged.
