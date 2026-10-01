---
schema: 4
id: TKT-01M3J59M7A0XK9NFE0RG72ZBFA
title: Find what installs a SIGSEGV handler without SA_ONSTACK under GTK4
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-linux
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T19:25:58Z
updated_at: 2026-10-01T19:24:17Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

Find which library in a WebKitGTK 6.0 (GTK4) process installs a SIGSEGV handler without `SA_ONSTACK`, and decide what tuohi should do about it.

### Why

While that handler is installed, any nil dereference in Go code, whether tuohi's or the consumer's, is a fatal error ("non-Go code set up signal handler without SA_ONSTACK flag") instead of a recoverable panic with a stack trace. This was seen in TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK stacks). JavaScriptCore is the likely source, because it installs signal handlers. `ensureInit` already unsets `JSC_SIGNAL_FOR_GC` (`lib_unix.go:326`), which may be related. INFERRED.

### Questions

- Which library, and at which call?
- Does the 4.1 stack do the same?
- Can an environment variable or a JSC option stop it, or can tuohi re-install Go's handler with `SA_ONSTACK` after WebKit initialises, the way Go's own cgo documentation suggests?

## Acceptance criteria

- [ ] The library and call that install the handler are named, with how it was found
- [ ] A recorded decision on whether tuohi works around it, with a ticket filed if it does

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T22:33:53Z

Seen again on 2026-09-29 in a local just test-gui on webkitgtk-6.0: "fatal error: non-Go code set up signal handler without SA_ONSTACK flag" after signal 11. The branch was feat/clipboard-linux merged with main at e78e6b5. It crashed after the permissions scenario, before the late loopback and clipboard scenarios. An immediate rerun of the same stack passed, and webkit2gtk-4.1 passed in the same run. Still intermittent, and still only on GTK 4.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T19:24:17Z

Owner, 2026-10-01: pinned until after v0.1.0; not a release blocker despite the v0.1.0 milestone.
