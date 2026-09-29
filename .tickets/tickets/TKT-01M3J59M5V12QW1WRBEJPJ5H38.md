---
schema: 4
id: TKT-01M3J59M5V12QW1WRBEJPJ5H38
title: Make the macOS main-thread rule explicit and enforced
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/macos-main-thread
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-main
  commit: cdbd8ae9d27641832b5afce08269f21ec48870a1
  session: null
  claimed_at: 2026-09-29T22:28:26Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-09-29T22:57:04Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Make the macOS rule that UI runs on the process's main thread explicit, and fail clearly when a program breaks it.

On macOS, AppKit must run on the main thread. tuohi locks the thread that first calls `Show`, and records whether it is main (`lib_darwin.go:535-542`, `:1739`). Nothing makes the program call `Show` and `Wait` from the main goroutine: no `init` locks it, and no documentation says so. A consumer that starts its HTTP server on the main goroutine and opens the window from another gets AppKit on a secondary thread, and the result is inferred to be a crash or a silent failure.

### Shape

- An `init` in the darwin engine that calls `runtime.LockOSThread`, which is the usual Go pattern for macOS UI libraries, so the main goroutine stays on the main thread.
- `Show` and `Wait` return an error when they are called off the main thread and no loop owner exists.
- The package doc and a consumer example show the loopback shape: the server in a goroutine, `Show` and `Wait` on main.

## Acceptance criteria

- [ ] The darwin engine keeps the main goroutine on the main thread
- [ ] Show and Wait called off the main thread with no loop owner return an error instead of misbehaving
- [x] The package docs show a loopback consumer with its server in a goroutine and the window on main

## Implementation plan

### Approach

- **`init` in `lib_darwin.go`** calls `runtime.LockOSThread()`. Package init runs on the main goroutine on the main thread, so the main goroutine stays there for the life of the process. The old `uiThreadOnce.Do(runtime.LockOSThread)` in `newView` goes.
- **`ErrNotMainThread`**, exported from `app.go` so it exists on every platform. `uiThreadErr()` is a per-engine hook:
  - darwin returns the error when the caller is off the main thread and `NSApp isRunning` is false;
  - Unix and Windows return nil.
  - `newView` and `App.Wait` both check it.
- **Remove `uiIsMain`.** `onUIThread` is `onMainThread()`. The special case existed only for a first window made off the main thread with no loop, and that shape is now refused (the note from PR #24's review).
- **`appUIWait` off the main thread under an external loop** waits for the exit and leaves events to the owner, as `webview.Run` already does. Pumping `nextEventMatchingMask` from a secondary thread was the same AppKit misuse.
- **Docs.** The package doc gets "The main thread" with a loopback consumer example. README and architecture.md are updated.

### Alternatives considered

- **Marshal `Show` to the main thread with a queue when called off it and no loop runs.** Rejected: nothing drains the main queue until something runs a loop on the main thread, so it would hang, which is worse than an error.
- **Log a warning instead of an error.** Rejected: the ticket asks for a clear failure, and AppKit off the main thread crashes or fails silently.
- **Keep `uiIsMain` for compatibility.** Rejected: the shape it served is exactly the one being refused.

### Test

`offMainScenario` is a macOS GUI scenario that runs first in TestMain, before any loop. From a goroutine it checks that the goroutine is off the main thread, then expects `Show` and `Wait` to return `ErrNotMainThread` and no window to be created. It also checks that TestMain itself is on the main thread. It runs on GitHub after the merge.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-28T22:16:17Z

TKT-01M3J1H8CPMZX9EJX8R2CQRA6P (Make every View method safe to call from any goroutine) adds a shared UI dispatcher. On macOS its onUIThread hook treats every goroutine as the UI thread once the first webview is created off the main thread with no loop running (uiIsMain false), because nothing drains a queue on that thread. terva-review raised this on PR #24, and it was deferred here: refusing that shape, as this ticket plans, is the fix. Once it is refused, the '!uiIsMain' case in onUIThread and performOnMain can go.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:28:26Z

Implemented on fix/macos-main-thread. just ci passes, and go vet passes for darwin, linux and windows. The darwin arm64 test binary compiles. The macOS scenario (TestNotMainThread) and the rest of the darwin suite run only on GitHub after the merge, so AC1 and AC2 stay unticked until GitHub main is green with it. AC3 (package docs show the loopback consumer) is done in app.go's package doc.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:36:33Z

Disposition for the terva-review findings at 851aae3 (run on request macos-main-review-1), fixed in 94029c9:

- **Accepted, high: "Do not call AppKit from the thread being rejected".** `uiThreadErr` no longer touches AppKit. It asks `pthread_main_np` (libSystem) whether this is the main thread, and CoreFoundation whether the main run loop is running, through `CFRunLoopCopyCurrentMode(CFRunLoopGetMain())`. CFRunLoop calls are thread-safe, and that run loop is what drains the main dispatch queue the work would be handed to.
  - `App.Show` (in `showFirst`) and `App.Wait` call the check before `begin`, so a refused call runs no `ensureInit` on the wrong thread.
  - The external-loop scenario used to poll `isRunning` from its goroutine, which turns true before the run loop is entered. It now also waits for `mainLoopRunning()`.
- **Accepted, medium: "Start UI-thread services before allowing off-main Wait".** `Wait`'s start step, `a.start` (the icon) and `App.Start`, now runs through a per-engine `startOnUI`.
  - On macOS it is `ui.call`: in place on the main thread, and otherwise handed to the owner's loop that `uiThreadErr` checked for.
  - Linux and Windows run it in place, as before.
  - The external-loop scenario calls `startOnUI` from its goroutine and checks that the function ran on the main thread.

`just ci` passes. `go vet` passes for darwin, linux and windows, and the darwin arm64 test binary compiles. The macOS scenarios run on GitHub after the merge.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:48:17Z

GitHub run 36641291121 at e6f9406, after #44 merged, failed Test MacOS with 'panic: appkit: objc class "NSThread" not found'. offMainScenario runs first in TestMain and called onMainThread before Foundation was loaded; the panic stopped every macOS scenario in the root package. Fixed on fix/macos-ci-main: onMainThread uses pthread_main_np, which needs no framework. The other macOS scenarios have not run since #44, so this PR is the first real test of the main-thread change.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:57:04Z

GitHub run 36642081724 at 166eed5, the first macOS run with #47: every macOS scenario passed except TestMultiWindowRefCount ('1->3->1', want '0->2->0'). TestNotMainThread passed, and so did TestNewUnderAnExternalRunLoop, which now checks that startOnUI runs on the main thread. The count was 1 before that scenario started. My hypothesis is a count-down still queued on the main dispatch queue from an earlier scenario. Fix and diagnostics are on fix/macos-window-count. AC1 and AC2 stay unticked until GitHub main is green.
