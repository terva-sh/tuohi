---
schema: 4
id: TKT-01M3N2Q03HV78EKDSH9E4CG0EY
title: Give macOS apps a default main menu
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT
    path: null
moved_to: null
claim:
  actor: agent:claude-code/t3code-6bca1629
  branch: feat/darwin-main-menu
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-6bca1629
  commit: 973d852d9b108c01cf9d99eda9b348cc03f33a42
  session: null
  claimed_at: 2026-10-01T05:56:31Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:38:34Z
updated_at: 2026-10-01T07:42:23Z
created_by:
  id: agent:claude-code/fe5548cb
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

On macOS, a tuohi app has no main menu. Nothing calls `setMainMenu:`, so the standard key equivalents never reach a window: ⌘W does not close it, and ⌘Q, ⌘M and ⌘H do nothing.

### Found by

git-ticket-canvas, working ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT (Run the desktop window's GUI scenarios on macOS). The maintainer pressed ⌘W on the canvas window and nothing happened. The close button works. Checked at main (57870a3): no `setMainMenu:`, `NSMenu`, or key equivalent anywhere outside the tray package.

### Suspected, not yet checked

WKWebView normally receives ⌘C, ⌘V, ⌘X, ⌘A and ⌘Z in text fields through the Edit menu's key equivalents. Without an Edit menu, copy and paste in a page's form fields may not work either. That would be a much bigger problem than ⌘W. Check it in a text field before settling the scope here.

### Things to settle

- **A default menu.** An application menu with About, Hide (⌘H), Hide Others, and Quit (⌘Q). A Window menu with Minimize (⌘M) and Close (⌘W). An Edit menu with Undo, Redo, Cut, Copy, Paste and Select All, sent to the first responder so WKWebView handles them.
- **Whether a consumer can replace it or add to it**, or whether the default is all a first version offers.
- **Quit from the menu.** Is it `App.Quit`, or a close of every window through the delegate? The second path reports each close, while a Destroy-driven close does not; see TKT-01M3N0PTS10TG629DVNEFSNVSZ (Check that a Close from Go reaches App.Wait on macOS).
- **Frameless windows.** They take key equivalents from the menu as well. Close has to work for them without a title bar.

## Acceptance criteria

- [ ] ⌘W closes the focused window and ⌘Q quits, on framed and frameless windows
- [x] Whether copy and paste work in a page's text fields without a menu is checked and recorded
- [ ] Cut, copy, paste, select all, undo and redo work in a page's text fields
- [ ] Whether a consumer can replace or extend the menu is decided and recorded

## Implementation plan

menu_darwin.go installs a fixed main menu from windowInitProceed, on the main thread, when NSApp has none. A menu that an embedding host installed first is left alone.

- Application menu: About (orderFrontStandardAboutPanel:), Hide ⌘H, Hide Others ⌥⌘H, Show All, and Quit ⌘Q. Quit goes to TuohiMenuTarget's tuohiQuit:, which calls the scope's requestExit, the same path as App.Quit. With no scope it calls NSApp terminate:.
- Edit: undo:, redo: (⇧⌘Z), cut:, copy:, paste: and selectAll:, sent to the first responder, which is WKWebView.
- Window: Minimize ⌘M and Close ⌘W go to the target. A window with a close button gets performClose:. A frameless window, which has no close button, gets close, which its delegate reports the same way. Minimize uses the engine's own Minimize for frameless windows and performMiniaturize: otherwise. NSApp's windowsMenu is set to this menu.

The title is App.Name, or the process name. Per the owner's 2026-10-01 decision there is no consumer hook to replace or extend the menu.

Tests (menu_darwin_test.go): the menu's key equivalents; ⌘W through [NSApp sendEvent:] on a framed and on a frameless key window; the Edit keys in a page's text field (copy, paste, select all and cut, undo, redo), checked against the field's value and the pasteboard; and one ⌘C with the main menu removed, logged rather than asserted, to record what pages got before. Only GitHub's macOS runner can run any of this, after the merge.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: a fixed default menu, and it blocks v0.1.0

The owner decided, 2026-10-01. Ship the default the ticket describes: an application menu (About, Hide ⌘H, Hide Others, Quit ⌘Q), an Edit menu (Undo, Redo, Cut, Copy, Paste, Select All, sent to the first responder), and a Window menu (Minimize ⌘M, Close ⌘W). A consumer can neither replace nor extend it in this version. A menu API waits until a consumer asks for one, which keeps the public API small for the first tag. Both alternatives lost on that point: an opt-out field, and an API to extend the menu across every engine.

⌘Q goes through `App.Quit`. ⌘W must close frameless windows too.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T06:36:00Z

### Landed in PR #62 (merge 77cad99). macOS tests fail on GitHub run 36825339187

- **TestDefaultMainMenu:** the menu is installed and every expected key equivalent is there. The test fails because AppKit adds its own items to a menu titled Edit (dictation and emoji, with "d", "e" and space equivalents), which the test counted. A test bug: it should check that the expected entries are present, not that nothing else is.
- **TestMenuCloseKey:** ⌘W sent through [NSApp sendEvent:] closed neither the framed nor the frameless window.
- **TestMenuEditKeys:** ⌘C, ⌘V, ⌘A/⌘X, ⌘Z and ⇧⌘Z through [NSApp sendEvent:] changed nothing (`copy= paste= cut=/ undo= redo=`).
- **TestCopyWithoutMainMenu** logged `copy=` as well, so this run cannot tell a broken menu from synthetic key events that never reach AppKit's key-equivalent dispatch on the runner. Criterion 2, whether copy works without a menu, is therefore still open.

Untested candidates: the app may not be active on the runner, so NSApp may not dispatch key equivalents, or keyWindow may be nil and menuClose does nothing. The events may also need a real timestamp, or need posting with postEvent:atStart: instead of sendEvent:. Calling [[NSApp mainMenu] performKeyEquivalent:] directly would test the menu's routing without depending on event delivery. menuClose and menuMinimize could fall back to mainWindow when there is no keyWindow.

The run also failed on Windows, from TKT-01M3V1Z4 (App.Wait sometimes misses a Close from Go on Windows), which is unrelated to this PR. No criterion is ticked.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T06:45:44Z

Follow-up to GitHub run 36825339187. The structure test now ignores items AppKit adds to Edit. The key tests hand each key to [[NSApp mainMenu] performKeyEquivalent:], which tests the menu's own routing without depending on how the runner delivers synthesized events. pressKey reports whether the menu handled the key and whether the window was key, main, and the app active, and the failure messages include that. Close and Minimize fall back to the main window when no window is key. The pasteboard is polled for up to 2 s after copy and cut. The no-menu record still goes through [NSApp sendEvent:], because with no menu that is the only path.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T07:06:52Z

GitHub run 36827912989: copy, paste and select-all/cut now pass through the menu. With no main menu, ⌘C is not handled (handled=false) and nothing is copied, which answers criterion 2: pages had no copy before the menu. ⌘W reached tuohiClose: (handled=true), but the test window was neither key nor main, because the scenario pressed before the page loaded. It now waits for the page and the Focus. Undo did not restore the cut text, though the menu handled ⌘Z. The scenario now waits 1 s after the cut and records the window's canUndo, to tell 'nothing registered' from 'undo did nothing'.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T07:42:23Z

GitHub run 36831446783 (after #67): Windows is green. On macOS, ⌘W still closed nothing, with the test window neither key nor main after the press, even though the scenario activated the app and sent makeKeyAndOrderFront: in its own main-thread turn. Activation likely returns key status to the window that was key before (one left from an earlier scenario) between that turn and the press. pressKey can now make the window key in the same turn as the key, and reports keyBefore. Undo: the menu handled ⌘Z with canUndo=true, but after 2 s of polling the field was still empty, and the log has no AppKit message. The scenario now records canUndo and canRedo after ⌘Z, to tell 'the undo manager never ran' from 'it ran and the page did not change'.
