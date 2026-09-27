---
schema: 4
id: TKT-01M3J59M32VGK83J7JKYPKSHJE
title: "Drop file: from the schemes App.Open accepts"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/api
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
updated_at: 2026-09-27T19:54:52Z
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

`App.Open` accepts `http`, `https`, `mailto`, and `file` (`app.go:1110-1125`). A `file:` URL handed to `ShellExecuteW`, `NSWorkspace openURL:`, or `xdg-open` can launch an executable, a `.app`, a `.lnk`, or a `.desktop` file. A UNC host in a `file:` URL makes Windows reach out to the network.

The owner decided on 2026-09-27, in the architecture review, to drop `file:`. `Open` takes `http`, `https`, and `mailto`. A local file is shown with `Reveal`, which only opens the file manager.

This matters more once the navigation policy (TKT-01M3HWWRT7X1RZZYY6KFEP0ERE) sends every external link a page follows through `Open`. A page then controls the argument.

## Acceptance criteria

- [x] App.Open rejects file: URLs with ErrScheme and accepts http, https, and mailto
- [x] The Open and Reveal docs say which to use for a local file

## Summary

Open allows http, https, and mailto only. file: returns ErrScheme, and the tests cover a plain file: path, an upper-case FILE: scheme, and a UNC host. The Open doc says why file: is refused and points to Reveal for local files. The README never listed file: for Open, so it needed no change. just ci passes. This unblocks the navigation policy in TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings), which depends on it.
