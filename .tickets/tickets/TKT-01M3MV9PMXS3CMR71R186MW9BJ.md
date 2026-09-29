---
schema: 4
id: TKT-01M3MV9PMXS3CMR71R186MW9BJ
title: Let a consumer title a view's window
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-linux
  - area/engine-darwin
  - area/engine-windows
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: feat/view-title
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-72958710
  commit: be027be70b7d2b1ef2c3e02ddbe30292db9743ec
  session: null
  claimed_at: 2026-09-29T03:47:48Z
  expires_at: null
archive: null
created_at: 2026-09-28T20:28:58Z
updated_at: 2026-09-29T04:00:09Z
created_by:
  id: agent:claude-code/c04aed4f
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

A view has no window title that a consumer can set, and on Linux the window does not follow the page's `document.title` either. git-ticket-canvas wants its window titled after the store it has open, and tuohi gives it no way to do that.

### Found by

git-ticket-canvas, working TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) against `v0.1.0-alpha.1`. That ticket asks that "the window title names the open store", and the canvas cannot meet it. Checked again on `main` at 70750bf: `View` has no title field, and no engine sets a window title outside the tray and notify packages.

### Things to settle

- **Go or page.** Either a `View.Title` field set at creation plus a setter that marshals to the UI thread, or a view that follows the page's `document.title`, or both, with Go winning when set. Following the page suits a consumer serving its own UI, as the canvas does, since the page already knows what it shows. A Go field suits a page that is not the consumer's.
- **Per engine.**
  - WebKitGTK exposes `notify::title` on the web view, and GTK sets the title with `gtk_window_set_title`.
  - WebView2 has `DocumentTitleChanged`. Its vtable slots are already declared in `lib_windows.go` and unused.
  - WKWebView's `title` is observable by KVO, and the window takes `setTitle:`.
- **Frameless windows.** There is no title bar, but the title still names the window in the taskbar, the window switcher, and accessibility tools, so set it anyway.
- **Untrusted pages.** A page on an origin the view does not trust, such as `about:blank`, should not rename the application's window.

## Acceptance criteria

- [ ] A consumer can set a view's window title on all three engines
- [ ] Whether the title follows the page's document.title is decided and recorded, and an untrusted page cannot rename the window

## Implementation plan

### Decisions (settled with the maintainer, 2026-09-29)

1. **Both sources, Go first.** A non-empty `View.Title`, or a later `View.SetTitle`, wins. `SetTitle("")` clears the Go title and hands the window back to the page. The rejected alternatives:
   - A separate `FollowPageTitle` switch was a second knob that could disagree with the title.
   - A Go title that stays final once set leaves the consumer no way to return control to the page.

   The cost of this choice is that a consumer cannot force a blank title while the page has one.
2. **Default is `App.Name`.** When no Go title is set and no trusted page has given a non-empty title, the window takes `App.Name`. If that is empty too, tuohi sets nothing, as today. This also fills the blank title Windows windows are created with (`lib_windows.go`, `CreateWindowExW` is given `""`). The rejected alternatives:
   - Keeping today's behaviour leaves the Windows gap in place.
   - An explicit empty string is blank everywhere.
3. **Host windows** (the unexported `View.window`) receive only an explicit Go title. The page title and the `App.Name` default never touch a window tuohi does not own. The rejected alternatives:
   - The same rules as owned windows would silently retitle a host window.
   - Never touching host windows would ignore an explicit request.
4. **No title callback in this ticket.** A `TitleChanged` observer or a `FormatTitle` formatter can be added later on the same internal message, so it will be filed only if a consumer asks for it.

### How the page title reaches Go

The page title travels through the bridge, not each engine's native title notification. The init script already runs only in the top frame, and only a trusted page's bridge can reach Go. So it sends `document.title` once at load, and again on every change that a MutationObserver on `<head>`/`<title>` sees, as a new internal message. `onMessage` stores the title and recomputes the window title.

An untrusted page, including about:blank, has no working bridge, so it cannot rename the window. That satisfies the second acceptance criterion without a trust check in each engine.

The native route was rejected on cost:
- WebKitGTK `notify::title` is cheap.
- WebView2 `DocumentTitleChanged` needs a new COM handler object, and Windows is tested only on GitHub CI.
- WKWebView would need KVO through a runtime-registered observer class.
- Each engine would also need its own trust check before renaming.

### Work

- `View.Title` field and `View.SetTitle(string)`, dispatched through `onUI`, like `Maximize`.
- Per-view state: the Go title, the last page title, and whether tuohi owns the window. Shared code works out the window's title as Go title, then page title, then `App.Name`, applied only when it changes.
- `SetTitle(string)` on the engine interface:
  - GTK3 and GTK4 use `gtk_window_set_title`;
  - Windows uses `SetWindowTextW`;
  - macOS uses `setTitle:`.
- The bridge JS sends the title, and `onMessage` handles the new internal message.
- Tests:
  - A Node case for the title observer.
  - A GUI scenario on every engine, which reads the native title back and walks through: the default Name; the page title; the Go title winning; `SetTitle("")` handing back to the page; an untrusted about:blank not renaming the window.
  - A negative control for each.
- `docs/architecture.md` and the View doc comment.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T03:57:06Z

### Implementation (feat/view-title)

As planned, with these details settled while building it:

- **Internal message name.** It is `__tuohiPageTitle`, not `__appkit...`. The repository rule is not to add new uses of the inherited appkit name.
- **The script watches narrowly.** An observer on the whole document subtree, with `characterData`, would make the engine record every DOM change a busy page makes. So after DOMContentLoaded, one observer watches the first HTML `<title>`'s text, and another watches the child lists of the document, its root and its head, finding the title again when it is added, removed or replaced. The cost is that a `<title>` a script adds outside `<head>` after load is not seen until one of those child lists changes.
- **about:blank.** A trusted document reports `""` at document start, so each trusted page starts from App.Name. An untrusted page, such as about:blank, reports nothing, so the window keeps the last trusted title. That satisfies "cannot rename"; the page title is not cleared at each engine's commit, because that would need a hook on every engine.
- **Title applied only on change.** `applyTitle` sets the native title only when it changes. An owned window with no App.Name can be set back to `""` after having had a title. A host window is never blanked.

### Tests and controls

- `TestApplyTitle` (headless, stub engine) covers the precedence steps, including a host window and an owned window with no App.Name.
- `TestPageTitleScript` (Node) is checked by four controls, each of which fails:
  - removing the "same title" check;
  - not observing the title text;
  - not observing head;
  - adding a whole-subtree observer.
- `TestWindowTitle` (GUI, every engine) walks through: App.Name before the page; the page title; the page title changed; the Go title; a page change under the Go title (not applied); SetTitle("") handing back to the page; the title removed (App.Name); retitled; and about:blank (keeps "Fourth"). On GTK4 it is checked by three controls, each of which fails at the expected steps:
  - no page script;
  - the page winning over Go;
  - no App.Name default.
- The untrusted-page guarantee is the bridge gate's, which TestBridgeGate and the origin-gate scenario already cover.

Checks passed: `just ci`, `just test-gui` on both stacks, and golangci-lint on three GOOS.

**agent:claude-code/t3code-72958710** at 2026-09-29T04:00:09Z

### Review disposition for PR #28, round 1 (terva-review run on 1ce79c8)

GitHub run 36519419676 on 1ce79c8 passed every job, including `TestWindowTitle` on macOS and Windows.

1. **Medium: observe title elements added outside the head.** Declined. It was already documented on `initPageTitle` and in the implementation note.
   - **What is covered.** `document.title = ...` rewrites the existing `<title>`. With none, it creates one in `<head>` (HTML's title setter does nothing if there is no head), and the head observer sees that. A `<title>` written in the markup, anywhere, is read at DOMContentLoaded.
   - **What is missed.** A script inserting a `<title>` element under `<body>` after load, where HTML's content model does not allow a `<title>`.
   - **Why not fix it.** Catching that case needs a subtree child-list observer on the whole document, which makes the engine create a record for every node a page inserts or removes, for the page's whole life. That cost is what the narrow observers avoid, and git-ticket-canvas re-renders often.
   - **How bad a miss is.** The window catches up at the next change to the head's, root's or document's child list. An untrusted page still cannot rename the window, so a miss leaves a stale title and nothing worse.

No code change in this round.
