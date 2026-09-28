---
schema: 4
id: TKT-01M3N2Q03HV78EKDSH9E4CG0EY
title: Give macOS apps a default main menu
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/engine-darwin
  - area/api
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T22:38:34Z
updated_at: 2026-09-28T22:38:34Z
created_by:
  id: agent:claude-code/fe5548cb
  name: ""
updated_by:
  id: agent:claude-code/fe5548cb
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
- [ ] Whether copy and paste work in a page's text fields without a menu is checked and recorded
- [ ] Cut, copy, paste, select all, undo and redo work in a page's text fields
- [ ] Whether a consumer can replace or extend the menu is decided and recorded
