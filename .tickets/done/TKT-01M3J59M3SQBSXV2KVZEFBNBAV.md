---
schema: 4
id: TKT-01M3J59M3SQBSXV2KVZEFBNBAV
title: Stop a non-string bridge message crashing macOS views
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/engine-darwin
  - area/bridge
  - security
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
updated_at: 2026-09-28T02:19:56Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
extensions: {}
---

## Description

### What

A page can probably crash the process on macOS. `userContentController:didReceiveScriptMessage:` (`lib_darwin.go:219-224`) sends `UTF8String` to `message.body` without checking its type. `window.webkit.messageHandlers.__webview__.postMessage({})` delivers an NSDictionary, and a number delivers an NSNumber, neither of which responds to `UTF8String`. Probably an unrecognised-selector exception, which aborts. This is inferred from the code, not yet run.

`setInspectable:` (`lib_darwin.go:758`) is also sent unguarded when Debug is on, and is a macOS 13.3 selector.

### Fix

Check `isKindOfClass:[NSString class]` before reading the body, and drop anything else. Guard `setInspectable:` with `respondsToSelector:`. Add a GUI scenario that posts a non-string body.

## Acceptance criteria

- [x] A page posting a non-string message body cannot crash the process, shown by a GUI scenario on macOS
- [x] setInspectable: is sent only where the selector exists

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:13:29Z

Fixed on branch feat/bridge-sender-darwin, together with the macOS sender check for TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings), because both change the same handler line. The body is read only when isKindOfClass:NSString, and setInspectable: is guarded by respondsToSelector:. badMessagesScenario in lib_darwin_test.go posts {}, 42, null, and [1,2], plus a well-formed message from an iframe, and expects the process alive with no frame call. That scenario runs only on GitHub's macOS runner, so criterion 1 is ticked only after the post-merge run passes. Criterion 2 is done in code, with no older macOS runner to exercise it.

## Summary

Landed in Forgejo PR #8 with the macOS sender check. The message handler reads the body only when it is an NSString, and setInspectable: is guarded by respondsToSelector:. TestBadMessagesAreDropped posts {}, 42, null, [1,2], and an iframe message, and passed on GitHub's macOS runner in run 36369201757 on 29ca235.
