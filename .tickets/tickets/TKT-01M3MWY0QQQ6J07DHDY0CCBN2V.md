---
schema: 4
id: TKT-01M3MWY0QQQ6J07DHDY0CCBN2V
title: "Stop dropping a data: page's binding call on Windows now and then"
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/windows-data-sender-flake
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-72958710
  commit: 5d9a4882234c7ee023483714d75736f71278de22
  session: null
  claimed_at: 2026-09-28T23:14:21Z
  expires_at: null
archive: null
created_at: 2026-09-28T20:57:33Z
updated_at: 2026-09-28T23:46:20Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
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

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-28T23:16:42Z

### First reading of the failing run

I read the WEBVIEW2_DEBUG trace from GitHub run 36480092776, attempt 1 (job 109123317964). For the data: scenario's view, the handler events arrived in this order:

1. NavigationStarting (kind 6)
2. ContentLoading (kind 8)
3. WebMessageReceived (kind 2), dropped
4. WebMessageReceived (kind 2), dropped
5. NavigationCompleted (kind 5)

So ContentLoading **did** run before both messages. "A message can precede ContentLoading", the ticket's second hypothesis, is not what happened in this run. The empty sender therefore came from one of these:

- ContentLoading ran without committing a URI: its navigation ID did not match one NavigationStarting recorded, `get_NavigationId` failed, or `get_IsErrorPage` failed or returned true.
- `get_Source` on the message failed, or returned `""` rather than `about:blank`, so the committed URI was never substituted.

The existing trace logs only the event kinds, so it cannot tell these apart. Commit 3d5cda8 adds debug lines with the navigation IDs, the recorded and error-page flags, the committed URI, and each message's `get_Source` HRESULT and value. It also adds `TUOHI_REPEAT_DATAURL=n`, and a branch-only workflow, `diag-dataurl.yml`, that runs 20 repeats on each of six Windows runners.

**agent:claude-code/t3code-72958710** at 2026-09-28T23:46:20Z

### Not reproducible on the current WebView2 runtime; the one failure ran on an older one

**Diagnostic runs** (branch-only `diag-dataurl.yml`, WEBVIEW2_DEBUG on). Every repeat passed:

| Run | Commit | Size | Failures |
|---|---|---|---|
| 36497119600 | 3d5cda8 | 6 runners × 20 repeats, plus the regular scenario | 0 of 126 |
| 36497396452 | 8d2a4db | 6 runners × 10 fresh launches × 200 repeats, plus 60 regular runs | 0 of 12,060 |

The new trace showed the same shape in every run:

- NavigationStarting names the data: URL.
- ContentLoading matches its navigation ID (`recorded=true`, `errorPage=false`) and commits the data: URL.
- Each message reports `about:blank` with HRESULT 0, and is read as the committed URL.

Nothing anomalous appeared: no empty commit, no ID mismatch, no failed `get_Source`, and no message before ContentLoading.

**Windows CI history.** I tallied the last 40 `ci.yml` runs, all attempts, by the runtime `findEmbeddedBrowserDLL` loaded and by outcome:

- Four jobs failed `TestDataURLCanUseBindings`. Three ran on commits before PR #20 (merge 8542142), when the data: sender was known to be broken: 36465210066, 36465995933 and 36466440440.
- The only failure after PR #20 is 36480092776 attempt 1, the one this ticket was filed for. It is also the only job that loaded WebView2 **149.0.4022.98**, on runner image 20260628.
- Every other job loaded **153.0.4234.48**, on images 20260828 and 20260922. All 33 successful Windows jobs, and every diagnostic run, used 153.

**Reading.** The flake is tied to the older runtime, or to that older runner image, not to a race that the current runtime exhibits. On 149 the committed URI was empty when both messages arrived, although ContentLoading had fired first. The likely cause is a navigation-ID mismatch, an error-page report, or `get_Source` returning `""` instead of `about:blank` on that version. Which one cannot be settled without a failing trace from 149.

GitHub's hosted runners do not let a job pick an older image. Reproducing on 149 would mean installing the WebView2 Fixed Version runtime 149 on the runner and pointing the loader at it, which `findEmbeddedBrowserDLL` does not support today.
