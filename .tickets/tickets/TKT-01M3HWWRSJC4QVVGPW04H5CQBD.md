---
schema: 4
id: TKT-01M3HWWRSJC4QVVGPW04H5CQBD
title: Review tuohi's architecture and write down its target shape
type: spike
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG
blocks_on: none
references:
  - ref: ticket:meta/TKT-01M3HS2HHEKMNZCGCBMYMHA37P
    path: null
moved_to: null
claim:
  actor: agent:claude-code/t3code-92c88910
  branch: spike/architecture-review
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-92c88910
  commit: 9bdaa18391a56eb4df23934c6c8a0a1b28a8d22f
  session: null
  claimed_at: 2026-09-27T19:01:57Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T19:01:57Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
extensions: {}
---

## Description

### What

Review tuohi's architecture before changing it, and write down the target shape: which package owns what, where the platform boundary sits, and what the public API promises. The output is a design doc in `docs/` and tickets for each change it calls for.

### Inputs

From terva-sh/meta TKT-01M3HS2HHEKMNZCGCBMYMHA37P (Create the tuohi repository and its GitHub release mirror):

- **The bridge's security model.** Today every page in every view gets every binding, and no platform checks where a message came from.
- **The permission surface.** macOS auto-grants camera and microphone, and Linux enables MediaStream and clipboard access with no handler.
- **Single instance as designed IPC.** A per-user runtime directory only, peer credentials, a message size limit, and forwarded arguments treated as untrusted.
- **The FFI layer.** Replace `pure` with upstream purego, and decide how the three engine backends share one interface. Today `lib_unix.go`, `lib_darwin.go`, and `lib_windows.go` are about 2,500 lines each.
- **Package boundaries.** Window and view, bridge, dialog, notify, tray, clipboard, autostart, and the `App.HTTP` loopback server: which are core and which optional. Whether `atotto/clipboard`, which shells out to `xclip`, `xsel`, or `wl-copy`, stays.
- **Borrowed code.** For the Wails autostart helpers and the webview WebView2 loader, keep them credited or rewrite them as ours.
- **Side effects nobody asked for.** GTK3 under Wayland writes icons and a `.desktop` file on startup.
- **Hygiene.** Review labels such as "T5", "(P1)", "R5", "E4/R1", and "P2/E2" run through the code, with paraphrased comments.
- **Support tiers and testing.** FreeBSD and NetBSD are compile-only. macOS and Windows are tested only on GitHub's hosted runners.
- **Go minimum.** 1.27 here, against 1.25 for git-ticket-canvas.

### Constraints

- **No cgo in anything that ships.**
- **Keep the functionality.** The owner asked for it kept, so a feature is removed only with a recorded reason.
- **Serve the consumers.** It must work for a program that already serves its interface over loopback HTTP, as terva, lampi, ketju, and git-ticket-canvas do. That is the case tuohi exists for, and the review should make it the first-class one.

## Acceptance criteria

- [ ] A design doc in docs/ records package boundaries, the platform interface, and the public API
- [ ] Each input listed in this ticket has a recorded decision
- [ ] A ticket is filed for each change the review calls for

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T18:22:05Z

An input from PR #3's review of TKT-01M3HWWRRXTAR4T01SK79Z4BSM: decide the lifecycle rule for UI calls made after the main loop has stopped for good. On Unix every marshalled call, now including Destroy, queues on the default GLib context and runs only if the UI thread iterates again. Windows behaves the same, and macOS's performOnMain waits instead. Pick one rule, and say what happens to a Close from a goroutine after App.Wait returns.
