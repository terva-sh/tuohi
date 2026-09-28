---
schema: 4
id: TKT-01M3J1H8CPMZX9EJX8R2CQRA6P
title: Make every View method safe to call from any goroutine
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-linux
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3J59M0VJYQ3E652Y0FD90H3
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/view-threading
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-72958710
  commit: 222f69a073f738781119ab07a8a9aced02c54e44
  session: null
  claimed_at: 2026-09-28T21:34:32Z
  expires_at: null
archive: null
created_at: 2026-09-27T18:20:14Z
updated_at: 2026-09-28T22:21:38Z
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

On the Unix engine (`lib_unix.go`), `webview.Navigate`, `webview.Eval`, and `webview.Focus` call WebKitGTK and GTK directly on whatever goroutine calls them. GTK is not thread-safe. Bindings run on their own goroutines (see `serialQueue` in bind.go), so a binding that navigates, evaluates script, or focuses its view makes GTK calls off the UI thread. That is the class of bug that crashed `Destroy` in TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks).

`View.Focus`'s doc comment says "the backends marshal to the UI thread when called from a background goroutine", and on Unix they do not. `Show`, `Hide`, `Maximize`, `Minimize`, `Unminimize`, and `Unmaximize` do marshal.

`View.Close` reads and clears `v.w` and `v.app` without synchronization. It is documented safe from any goroutine, but two concurrent calls race. That affects every platform.

### Fix direction

Marshal each through `dispatchMain` when `onUIThread()` is false, as `Destroy` now does. Check the macOS and Windows engines for the same gaps. Add a GUI scenario that calls each method from a binding. Make `View.Close` safe against concurrent calls.

The architecture review, TKT-01M3HWWRSJC4QVVGPW04H5CQBD, may prefer one rule for the whole `webview` surface over per-method fixes, so decide there if it has not landed first.

## Acceptance criteria

- [ ] Navigate, Eval, and Focus on Unix run their GTK calls on the UI thread from any goroutine
- [ ] A GUI scenario calls each from a binding goroutine on both WebKitGTK stacks
- [x] Concurrent View.Close calls do not race under go test -race

## Implementation plan

### Scope

The owner chose, 2026-09-29: the whole View surface on all three engines, through one shared marshal layer. App-level gaps go to their own tickets (see "Not in this ticket").

### Approach

1. **Shared layer, `uithread.go`.** A `uiDispatcher` holds three per-engine hooks: `onUI() bool`, `post(func()) bool`, and `external() bool`, which reports that someone else's loop is draining the UI queue (macOS only, for the tray or a host `[NSApp run]`). It offers:
   - `run(f)`: in place on the UI thread, otherwise posted without waiting.
   - `call(f) error`: in place on the UI thread. Otherwise it creates an operation with an atomic state (pending, running, cancelled, done), posts it, and waits. The UI thread moves it from pending to running before running it and skips a cancelled one. The caller gives up only when the loop stops, never on a timer.
   - Loop tracking: `enterLoop` and `exitLoop` count running loops. When the count reaches zero, every pending operation is cancelled and its caller gets `errUILoopStopped`. A running operation is left to finish, and its caller waits for it. A `call` made while no loop runs, and none runs externally, returns `errUILoopStopped` at once.
   - "Stopped" means "no loop is running now", not "stopped for good". Tests and embedders call `Run` again after it returns, so a permanent mark would break the second `Run`.
2. **Loop owners mark the loop.** `App.Wait` wraps its loop, and each engine's `webview.Run` wraps its own.
3. **View methods** (view.go, bind_evt.go):
   - `Navigate`, `Eval`, `Focus`, `Show`, `Hide`, `Maximize`, `Minimize`, `Unminimize` and `Unmaximize` go through `run`.
   - `Dialog` goes through `call`, so it no longer hangs after the loop stops. On the UI thread it now runs in place, where it deadlocked before.
   - `v.w` and `v.app` are guarded by a mutex on View. `Close` takes both and clears them under the lock, so exactly one caller closes the engine.
4. **Engines:**
   - **Unix:** `post` is `dispatchMain`, and `onUI` is the existing `onUIThread`.
   - **macOS:** `performOnMain` waits through `call` semantics instead of forever. `onUI` is `onMainThread() || !uiIsMain`, which keeps today's inline case. `external` is `NSApp isRunning && !appkitRunsLoop`. As a side effect, `Quit` before `Wait` returns instead of hanging.
   - **Windows:** the first `newView` records the app UI thread and creates a message-only window (HWND_MESSAGE) on it. `post` sends WM_APP to that window. A message-only window keeps receiving messages during modal loops, where a thread message would be lost. `Destroy` uses `onUI` instead of `w.window != 0`, so a `Close` after the user closed the window no longer releases COM objects off the UI thread. `webview.Dispatch` falls back to `post` once the window is gone, instead of posting to a NULL HWND, which delivers to the caller's own thread.
5. **Tests:**
   - Headless: the dispatcher's operation states, cancellation when the loop stops, running in place, a failed post, and concurrent `View.Close` against a fake engine under `-race`.
   - GUI, shared by all engines: a scenario that calls `Navigate`, `Eval` and `Focus(true)` from inside a binding and checks each took effect.
6. **Docs:** the threading table in docs/architecture.md, the stale `performOnMain` line citation there, and the View method comments.

### Alternatives rejected

- **Per-engine fixes only** (the ticket's original direction). This repeats marshalling three times, and the contract drifted exactly that way before TKT-01M3J59M0V.
- **Removing the engines' own internal dispatch.** `App.Show`'s reveal path and `handleInternal` call engine methods from the caller's goroutine, and `App.Show` is out of scope, so the engines keep their own marshalling. On the UI thread it is harmless.
- **A timeout on waiting calls.** Rejected in the architecture review: a timeout cannot tell a stopped loop from a busy one.
- **Thread messages on Windows** (PostThreadMessageW). These are dropped while a modal loop, such as a window drag or a dialog, runs its own pump.

### Not in this ticket (filed separately)

- `App.Show` from a goroutine creates the window on the caller's thread. On Windows, its `BindBatch` pumps `GetMessageW` there.
- `App.Notify`, `Open` and `Reveal` make direct native calls on macOS and Windows.
- On macOS, a `Destroy`-driven close may not reach `appWindowClosed`. This was found by reading and has not been checked on macOS.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Scope widened by TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

Retitled from "Unix Navigate, Eval, and Focus call GTK off the UI thread". Windows has the same gaps.

| Method | Unix | macOS | Windows |
|---|---|---|---|
| Navigate, Eval, Focus, Raise | direct | synchronous marshal | direct |
| Internal BindBatch | direct | synchronous marshal | direct; pumps `GetMessageW` on the caller's thread |

**The rule, from `docs/architecture.md` "One threading rule":** every exported `View` and `App` method is safe from any goroutine on every engine. On the UI thread it runs in place. Elsewhere it is marshalled asynchronously, except where the caller needs a result; those wait with a timeout and return an error rather than hang when the loop has stopped. This lands in the shared engine layer from TKT-01M3J59M0VJYQ3E652Y0FD90H3 (Declare the engine interface and share the bridge core), so each method is marshalled once rather than three times.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:35:06Z

### Supersedes the timeout rule in the previous note

The previous note said calls needing a result "wait with a timeout and return an error rather than hang when the loop has stopped". A timeout cannot tell a stopped loop from a busy one. A call could return an error while its operation is still queued, then run when the loop resumes, and a caller that retries a dialog would get two. Found by terva-review on tuohi PR #4.

The rule instead:

- **Each marshalled operation carries a state:** pending, running, or cancelled.
- **The UI thread claims an operation before running it,** moving it from pending to running atomically. It skips a cancelled one.
- **The loop owner marks the loop stopped when its loop exits for good:** the end of `App.Wait` or `Run`. Anything still pending is then cancelled.
- **A caller waiting for a result gives up only when the loop is marked stopped,** never on a timer.
- **A caller that gives up cancels its operation atomically.** If the operation has already started, the caller waits for it to finish. An operation that reported failure therefore never runs.
- **A call made after the loop is marked stopped** returns an error at once, without queuing.

**agent:claude-code/c04aed4f** at 2026-09-28T20:29:09Z

Consumer report from git-ticket-canvas, working TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) against v0.1.0-alpha.1. The canvas hit this outside a binding. Its GUI test drives the page with View.Eval from a test goroutine, and that is off the GTK thread for the same reason a binding is. The test calls Eval from View.Ready instead, which runs on the UI thread. So any goroutine is affected, not only bindings: a consumer's own background work is the more common caller. Filed under TKT-01M3MV9EBAGG435E6V7JFQHYA2 (Capture problems tuohi's consumers find).

**agent:claude-code/t3code-72958710** at 2026-09-28T22:04:48Z

### Implemented; Forgejo PR #24, stacked on #23

Landed as planned, with these differences from the plan:

- **Where `Close` locks:** `View.Close` takes and clears the engine handle and the App under a new `View.mu`. Every other reader of `v.w` in the shared code goes through `View.live()`.
- **`App.Show`'s reveal path** (a second `Show` on a live view) goes through `ui.run` too. It is one line, and it removes the direct Raise and Focus off the UI thread.
- **Test workarounds removed:** three shared scenarios wrapped `w.w.Navigate` and `w.w.Eval` in `w.w.Dispatch` only because the public methods were not safe off the UI thread. They now call `w.Navigate` and `w.Eval` directly. The wrappers also read `w.w` unguarded, which is the one race `-race` found (see below).

### Evidence (Linux)

- **The scenario catches the bug:** `TestViewMethodsFromBinding` calls Eval, Focus(true) and Navigate from a binding. With the old direct calls restored in view.go, it aborted with SIGABRT in 3 of 3 runs on WebKitGTK 6.0. With the fix it passes on both stacks.
- **`just ci` and `just test-gui`** pass.
- **The GUI suite under `-race`** (`CGO_ENABLED=1 go test -race`) passes with zero races on both stacks. The first run found one race, in nav_policy_test.go: the test read `w.w` inside a `Dispatch` closure while a `Close` cleared it. Using the public methods removed it.
- **Headless `-race`** passes: the dispatcher tests and `TestViewCloseConcurrent`, which runs 8 goroutines over 50 rounds.

### Not verified here

- The macOS and Windows engines. The branch is pushed to the GitHub mirror, whose CI runs on every push (run 36490108667).
- The Windows message-only window is new native code that nothing here can run.

**agent:claude-code/t3code-72958710** at 2026-09-28T22:07:02Z

The owner decided, 2026-09-29, that PR #24 keeps its macOS and Windows engine changes together. This relaxes the earlier guidance to change one native engine per PR. That guidance comes back when platform-specific failures start appearing. Until then, a change that spans engines may land as one PR, tested on GitHub before merging.

**agent:claude-code/t3code-72958710** at 2026-09-28T22:12:12Z

GitHub run 36490213869, on 865f6be (the code, plus the Linux-evidence note), passed every job:

- the macOS and Windows engine tests
- all four Linux GUI jobs (amd64 and arm64, GTK3 and GTK4)
- golangci-lint
- 19 cross-builds

`TestViewMethodsFromBinding` therefore passes on WKWebView and WebView2 too, and the Windows message-only dispatch window works on the hosted runner.

### Review disposition for PR #24 (terva-review run 14516, on 5cf7c09)

1. **High: cancel waiting calls when an external UI loop stops.** Fixed. `call` now waits through `uiDispatcher.wait`, which checks every 50 ms that some loop still runs and cancels the operation when none does. This is not a timeout: a loop that is running but busy is waited for as long as it takes. A cancel after the operation has started is a no-op, so the caller then waits for it to finish. Test: `TestUICallCancelledWhenExternalLoopStops`. With the cancel removed it fails at its 5 s guard.
2. **Medium: keep Close from unregistering a concurrently re-shown View.** Fixed. `appScope.views` maps each View to its engine, and `unregisterView(v, w)` deletes the entry only when it still names `w`. Serializing Show with the whole teardown was rejected because it can deadlock on macOS: `Destroy` waits for the main thread through `performOnMain`, and the main thread could be inside `App.Show`, waiting for the same lock. Test: `TestViewCloseKeepsReshownRegistration`. With an unconditional delete it fails.

After the fixes, `just ci`, `just test-gui` on both stacks, and golangci-lint on linux, darwin and windows all pass.

**agent:claude-code/t3code-72958710** at 2026-09-28T22:16:17Z

### Review disposition for PR #24, round 2 (terva-review run 14521, on 2aba774)

Both round-1 findings were reported resolved.

1. **High: prevent queued View calls from running after Close.** Fixed.
   - View methods queue through `View.onUI`. When the closure runs, it checks that the View still holds the engine it was queued for, and drops itself otherwise.
   - `App.Show`'s reveal path does the same through `onUIWith`.
   - Unix `Navigate` also returns once its web view is gone, as `Eval` and `Focus` already did.
   - Test: `TestViewQueuedCallDroppedAfterClose`. With the check removed it fails.
   - An alternative was to give `ui.run` operations a state and cancel a view's pending ones on Close. It was rejected because the dispatcher would need to know which view each operation belongs to, and the check at run time covers the same case with one comparison.
2. **High: do not expose an engine to Close before Show finishes initializing it.** Fixed. `showFirst` keeps the engine private until it is set up and registered, then publishes `view.w` and `view.app` together. A Close before that finds the View unshown and is a no-op, the same as closing a View that was never shown. No headless test: `showFirst` needs a real engine. The existing GUI scenarios all go through it and pass.
3. **High: macOS treats every thread as the UI thread after creation off main.** Deferred to TKT-01M3J59M5V12QW1WRBEJPJ5H38 (Make the macOS main-thread rule explicit and enforced).
   - This is not a regression. The old `performOnMain` also ran everything inline when `uiIsMain` was false.
   - In that shape nothing drains a queue on the thread that created the view, so there is nowhere to marshal to. AppKit is already off the main thread, whatever tuohi does.
   - TKT-01M3J59M5V refuses the shape outright. The limit is now documented on `onUIThread`.

Still open, and not part of this ticket: two concurrent first `App.Show` calls on one View both see it unshown and both create a window. That belongs with TKT-01M3N0PTQ7ZY1X78PX70SZ56AN (Let App.Show create a window from any goroutine).

After the fixes, `just ci`, `just test-gui` on both stacks, and golangci-lint on three GOOS all pass.

**agent:claude-code/t3code-72958710** at 2026-09-28T22:21:38Z

### Review disposition for PR #24, round 3 (terva-review run on d55329b)

The round-2 findings were reported handled: 1 and 2 resolved, and 3 declined, as deferred to TKT-01M3J59M5V. GitHub run 36491384974 on d55329b passed every job.

1. **High: do not queue Windows teardown after the last message loop has exited.** Fixed at the source.
   - `engineMsg`'s WM_DESTROY now closes the controller and releases the environment on the UI thread (`closeController`, `releaseEnvironment`), while the window and the loop that delivered the message are still alive.
   - A user close that ends the last loop therefore leaves nothing for a later Close to release. That Close's posted `destroyOnUI` would only drop pending closures and unregister the engine, and WM_DESTROY already unregisters it.
   - Test: `winCloseViaUIScenario` now also checks that the controller and environment are zero once `Run` returns after WM_CLOSE. Only GitHub's Windows runner can run it.
   - Not fixed, and unchanged from before this PR: a Close from a goroutine after `App.Quit` has ended the loop while the window is still open posts a teardown that never runs. A process that quit its loop is exiting.
2. **Medium: do not release COM objects off-thread if dispatch-window creation fails.** Fixed. Off the UI thread, `Destroy` always goes through `w.Dispatch`. That posts to the view's window while it lives, which is the pre-PR path, and otherwise to the dispatch window. When neither can take the teardown, it is dropped and never run in place. The earlier code ran it in place whenever `postUI` refused.

Checks: `just ci`, and golangci-lint on linux, darwin and windows. The change is Windows-only, so the Linux GUI suite is unaffected.
