---
schema: 4
id: TKT-01M3MWY0QQQ6J07DHDY0CCBN2V
title: "Stop dropping a data: page's binding call on Windows now and then"
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-windows
  - area/bridge
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:TKT-01M3HWWRT7X1RZZYY6KFEP0ERE
    path: null
  - ref: code:lib_windows.go
    path: lib_windows.go
  - ref: evidence:github-run-36480092776-attempt-1
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T20:57:33Z
updated_at: 2026-09-28T20:57:33Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/c04aed4f
  name: ""
extensions: {}
---

## Description

### What

`TestDataURLCanUseBindings` failed on Windows in GitHub run 36480092776, attempt 1, and passed on attempt 2. The first attempt ran on `main` at 8e3302c, which changed only `.tickets/` since 70750bf, where run 36471248641 passed. So this is intermittent, and it is not caused by the code it ran against.

The page loaded and saw the bridge and the binding, but its call never reached Go within the scenario's 10 s:

```text
loopback_app_test.go:189: data: URL = "no call, page saw chrome=object&webview=object&bridge=object&hit=function&href=data%3Atext%2Fhtml%2C...", want "called data"
tuohi: message from "" dropped: its origin "" is not trusted
tuohi: message from "" dropped: its origin "" is not trusted
```

### Where

`WebMessageReceived` in `lib_windows.go` reads a data: document's sender as `about:blank`, and replaces it with `w.committedURI`. `kindContentLoading` sets `committedURI` from the URI `NavigationStarting` recorded under the same navigation ID, and clears it first. A sender of `""` means one of three things happened when the message arrived:

- `committedURI` was empty: `ContentLoading` had not run, or had run without finding the navigation in `pendingNavs`.
- `ContentLoading` had cleared it for a navigation it could not match.
- `GetSource` failed.

Which one has not been determined. The comment on `kindContentLoading` rests on messages never arriving before `ContentLoading` (GitHub run 36466440440), and this run may be a counterexample.

### Why it matters

To a consumer, this is a binding call from a data: page that is silently dropped once in a while. It is also a CI flake on `main` that a person has to rerun.

### Things to settle

- Log the navigation ID, `committedURI`, and the event order around a dropped message in this scenario, and repeat the Windows job until it fails again.
- If a message can precede `ContentLoading`, whether to hold messages from `about:blank` until the next commit and deliver them after it, rather than dropping them.

### Related

- TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings). Its PRs #19 and #20 wrote this path.
- Found while syncing tuohi PR #22 to GitHub.

## Acceptance criteria

- [ ] The cause of the empty sender is found and recorded
- [ ] A data: page's binding call is not dropped when its message arrives before or around ContentLoading
- [ ] TestDataURLCanUseBindings passes on 20 consecutive Windows runs
