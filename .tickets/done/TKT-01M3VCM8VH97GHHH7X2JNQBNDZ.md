---
schema: 4
id: TKT-01M3VCM8VH97GHHH7X2JNQBNDZ
title: "Stop a queued macOS stop: from ending the next Run"
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
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-10-01T09:27:17Z
updated_at: 2026-10-01T09:49:36Z
created_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

On macOS, closing the last counted window by the OS (here ⌘W) queues two `stop:` requests for one `[NSApp run]` loop. `onWindowDestroyed` stops the loop when the count reaches zero, and the `View.Close` that follows calls `Terminate`, which queues another. Both run later from the main queue. The first ends the loop. The second waits in the main queue until the next loop starts, and ends that one at once.

GitHub runs 36839261792 and 36839839080 show it: in TestMenuCloseKey the frameless window's Run ended before its page loaded, and the next scenario's Run ended with no report. Both runs had 0 windows counted going into the menu scenarios. Passing runs had 2 or 4 still counted, so the last-window stop never fired there.

A consumer that closes its last window and then runs another window would see the second Run return immediately. App.Wait is not affected, because it loops on its exit flag.

### Where

`Terminate` and `Run` in `lib_darwin.go`. The Windows engine had the same shape of bug with PostQuitMessage, fixed in #67.

## Acceptance criteria

- [x] A stop queued for one loop does not end a later Run
- [x] TestMenuCloseKey passes on GitHub macOS in runs where no window is left counted

## Implementation plan

Gate Terminate's queued stop to the loop that was running when it was called. A loopGen counter goes up each time tuohi enters [NSApp run] (runNSApp, used by Run and appUIWait), and the dispatched block stops the loop only if tuohiRunsLoop is still set and loopGen is unchanged. Terminate also closes the view's closed channel, and Run returns at once if it is already closed, so a Close that comes before Run still ends it. Considered and rejected: a per-view running flag like Windows #67 has. On macOS the loop is process-wide, so a flag does not tell one loop from the next. Also rejected: making the test avoid closing the last window. That hides a bug a consumer can hit.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T09:38:45Z

Review of #72 (high, accepted): Run checked the closed channel before loopGen was raised, so a Terminate between the two captured the old generation and its stop was dropped, leaving Run stuck. Now runNSApp raises loopGen, then reads the view's terminated flag, while Terminate sets terminated and then reads loopGen. Both are sequentially consistent atomics, so either Run sees terminated and returns, or Terminate reads the new generation and its stop applies. The channel alone could not give that guarantee, because Go's memory model orders atomics, not a channel close against an atomic.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T09:49:35Z

GitHub run 36844813205 (main d7aff9c, #72 merged): macOS went into the menu scenarios with 0 windows counted, the condition under which runs 36839261792 and 36839839080 failed. TestMenuCloseKey and TestMenuEditKeys passed, and the count after the menu scenarios was 0 (it was 1 in the failing runs, the frameless window left behind). One run under the failing condition is the evidence. The condition depends on timing in earlier scenarios and cannot be forced from CI.

## Summary

Fixed in #72. macOS Terminate's queued stop: now acts only on the loop that was running when Terminate was called (loopGen, runNSApp). A Terminate before Run's loop starts is seen through the view's terminated flag, ordered against loopGen with atomics. This is the macOS counterpart of #67 on Windows. Verified on GitHub run 36844813205 under the failing condition.
