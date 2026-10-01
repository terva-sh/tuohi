---
schema: 4
id: TKT-01M3W9PT4ZEDH0SVD645JW23AA
title: Keep WebKitGTK 2.54 on the process's main thread
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-linux
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-6bca1629
  branch: fix/gtk3-clipboard-crash
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-6bca1629
  commit: 755a19f2e5a81f03f4d455020deb796cacb43fbd
  session: null
  claimed_at: 2026-10-01T17:55:35Z
  expires_at: null
archive: null
created_at: 2026-10-01T17:55:29Z
updated_at: 2026-10-01T17:58:48Z
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

WebKitGTK 2.54.0, now in Debian 13 for both stacks (webkit2gtk-4.1 and webkitgtk-6.0), aborts the process when WebKit is first used on a thread that is not the process's main thread:

```text
Thread 4 "lb" received signal SIGABRT
#2  abort ()
#3-#4  ?? () from libjavascriptcoregtk-6.0.so.1
#6  pthread_once ()
#7  WTF::initializeMainThread() () from libjavascriptcoregtk-6.0.so.1
#8-#13 ?? () from libwebkitgtk-6.0.so.4
```

On Linux, tuohi pins the OS thread of whichever goroutine makes the first `App.Show`, in `newView` (`uiThreadOnce`). The main goroutine starts on the main thread, but Go may move it to another thread until it is locked. A program that does work before its first Show, as `examples/loopback` does by starting its HTTP server, then sometimes creates its first window on another thread and aborts.

### Evidence

- `examples/loopback` on webkitgtk-6.0 in CI's image (`golang:1.27-trixie`, 2 CPUs): SIGABRT in `windowInit` in 2 of 4 runs, and the gdb trace above on the second try.
- The root GUI suite locks the thread first thing in TestMain and passes.
- A test that called App.Show from a test goroutine aborted at once on webkit2gtk-4.1 2.54.0.

### Fix

As on macOS, the package's `init` locks the main goroutine to the main thread. A program that calls Show from `main`, as the README says to, then always creates its windows there.

## Acceptance criteria

- [x] The main goroutine stays on the process's main thread from package init on Unix
- [x] examples/loopback passes repeatedly on webkitgtk-6.0 2.54 in CI's image

## Implementation plan

Lock the main goroutine to the main thread in lib_unix.go's init, as lib_darwin.go does for AppKit, and update the README's threading paragraph. Considered and not done here: returning ErrNotMainThread when a program's first Show runs off the main thread on Unix. Before 2.54 that worked, so refusing it is an API change that needs the owner's say. With the init lock, a program that follows the README never hits the abort.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T17:55:35Z

Picked up under the owner's request to finish v0.1.0 ('Let's tackle what is left'): every Linux consumer on Debian 13 can abort at startup, so it gates the release the way the crash-class bugs before it did. The owner can drop the gate.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T17:58:47Z

Verified in CI's image with WebKitGTK 2.54.0 on both stacks: examples/loopback passed 10/10 on webkitgtk-6.0 and 10/10 on webkit2gtk-4.1 (2/4 aborted before), and the Forgejo GUI job's full script passed on both stacks, with the root package and examples.
