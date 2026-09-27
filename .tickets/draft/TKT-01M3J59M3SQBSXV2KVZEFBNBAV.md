---
schema: 4
id: TKT-01M3J59M3SQBSXV2KVZEFBNBAV
title: Stop a non-string bridge message crashing macOS views
type: bug
status: draft
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
updated_at: 2026-09-27T19:25:58Z
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

- [ ] A page posting a non-string message body cannot crash the process, shown by a GUI scenario on macOS
- [ ] setInspectable: is sent only where the selector exists
