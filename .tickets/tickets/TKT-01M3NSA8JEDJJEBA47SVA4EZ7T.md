---
schema: 4
id: TKT-01M3NSA8JEDJJEBA47SVA4EZ7T
title: Document why macOS keeps element fullscreen on
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area/engine-darwin
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
  branch: t3/fullscreen-policy
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: f9335e6241ba7ed6ab56c62b751e10402f77da12
  session: null
  claimed_at: 2026-10-23T04:39:56Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:13:34Z
updated_at: 2026-10-23T04:50:42Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

### What

Decide whether macOS keeps turning on WKWebView's `fullScreenEnabled`, which is off by default.

### Current behaviour

`lib_darwin.go` sets `fullScreenEnabled` to YES as "appkit's tuned default". With it on, a page can take the whole screen through the Fullscreen API with a user gesture. WebKitGTK and WebView2 allow element fullscreen by default, so turning it on makes macOS behave like the other engines.

### Direction

Either:
- keep it and document why, which gives consistency across engines; or
- turn it off unless the application asks, which makes it a permission like those in `View.Permissions`.

The architecture review listed it next to the media grants. It is split out of TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the app allows them), because that ticket's acceptance criteria cover only camera, microphone and clipboard.

## Acceptance criteria

- [x] The comment in lib_darwin.go gives the cross-engine reason for fullScreenEnabled
- [x] The View.Permissions documentation says element fullscreen is allowed on every engine and is not a permission
- [ ] Native macOS verifies the enabled public element-fullscreen preference in the copied WKWebView configuration; unavailable older APIs are reported accurately.

## Implementation plan

Keep the owner-selected cross-engine element fullscreen policy enabled using public WKPreferences.elementFullscreenEnabled where available. Remove the contradictory false assignment, keep legacy guarded compatibility without adding new private SPI, and explain gesture gating in constructor, View.Permissions and architecture docs. Add a native macOS regression querying the copied WKWebView configuration to ensure the public preference is enabled after construction. Local cgo-free CI/cross builds and Darwin test compilation first; actual native preference execution follows reviewed merge/mirror. No additional permissions API or gesture bypass.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: keep it on, and document why

The owner decided, 2026-10-01. macOS keeps `fullScreenEnabled` on, so element fullscreen behaves as it does on WebKitGTK and WebView2, which allow it by default. A page still needs a user gesture to enter fullscreen.

What's left is documentation. Replace the "appkit's tuned default" comment in lib_darwin.go with this reason, and state the cross-engine behaviour wherever View.Permissions is documented, so nobody expects it to be a permission. Making it a permission lost because it would have to apply on all three engines to be consistent, which costs more work on Linux and Windows to guard against a gesture-gated risk.

It does not block v0.1.0.

**agent:codex/t3-b903096c** at 2026-10-23T04:39:56Z

Source inspection found the historical ticket assumption is stale: newWebView attempts legacy fullScreenEnabled=true, then explicitly writes public elementFullscreenEnabled=false. Current WebKit WKPreferences.mm shows setElementFullscreenEnabled writes the same fullScreenEnabled preference, so the later write disables the intended policy on supporting macOS versions. Source: https://github.com/WebKit/WebKit/blob/main/Source/WebKit/UIProcess/API/Cocoa/WKPreferences.mm . The legacy setFullScreenEnabled selector is not the current public API (private SPI is _setFullScreenEnabled). The owner decision dated 2026-10-01 explicitly chooses enabled element fullscreen across engines; documenting the existing false setter as enabled would be inaccurate. Implement that selected policy through the public guarded setter and retain the older guarded attempt for legacy runtimes; add a native preference-query regression instead of claiming a docs-only no-op. Actual user-gesture fullscreen execution remains native acceptance work.

**agent:codex/t3-b903096c** at 2026-10-23T04:42:16Z

Implementation applies the recorded owner policy through guarded public elementFullscreenEnabled=true, replacing the contradictory false write; legacy guarded spelling retained without adding private SPI. View.Permissions and architecture describe cross-engine parity and engine gesture/API availability. Added TestElementFullscreenPreference: create a native WKWebView and query its copied WKPreferences.isElementFullscreenEnabled, so the old false assignment would fail on a supporting runtime. Older engines without this public getter explicitly skip with a reason. Local CGO_ENABLED=0 just ci passes both modules/prescribed cross builds; Darwin arm64 test binary compiles. Actual macOS test execution and user-gesture fullscreen are not established here; native preference execution remains pending reviewed merge/mirror. Ticket remains in progress until that result.

**agent:codex/t3-b903096c** at 2026-10-23T04:50:42Z

Merged current main f022d8cd07bca41787b54386c612e6118287e2b2 after PR #96. Repeated CGO_ENABLED=0 just ci successfully on the combined tree: both modules, JavaScript/import checks, prescribed eight-target cross builds, and strict tickets. Darwin arm64 test binary compiles. This remains compilation evidence only for the new macOS preference scenario; its native result is pending the reviewed merge/mirror workflow. Latest complete native baseline is 31d2f8d9c492745547815d1f47d66d1aeb5f4689, GitHub run 39901736578, all jobs successful. No release published.
