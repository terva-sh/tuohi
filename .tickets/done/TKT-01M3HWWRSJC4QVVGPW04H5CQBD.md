---
schema: 4
id: TKT-01M3HWWRSJC4QVVGPW04H5CQBD
title: Review tuohi's architecture and write down its target shape
type: spike
status: done
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
claim: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T19:38:10Z
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

- [x] A design doc in docs/ records package boundaries, the platform interface, and the public API
- [x] Each input listed in this ticket has a recorded decision
- [x] A ticket is filed for each change the review calls for

## Implementation plan

### Method

1. Four read-only investigators each mapped one slice of the source, with file:line evidence and inferences marked:
   - the engine boundary, threading, and the FFI layer;
   - the public API, packages, dependencies, side effects, hygiene, and the Go minimum;
   - the bridge protocol, the trust points, navigation hooks, and permissions;
   - the desktop services and the borrowed code.
2. Load-bearing claims were checked by hand before they went into the doc: the loopback idle timeout, the missing Host check, the macOS message body, the single-instance unlink order, the `validateScheme` allow-list, and x/sys's declared Go version.
3. git-ticket-canvas's `docs/desktop-shell.md` and TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) define the consumer the design serves first.
4. The owner decided the three choices that change the public API or other repositories: the package split, the Go minimum, and `file:` in `App.Open`. The rest are technical, and each is decided in `docs/architecture.md` with the alternative and why it lost.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T18:22:05Z

An input from PR #3's review of TKT-01M3HWWRRXTAR4T01SK79Z4BSM: decide the lifecycle rule for UI calls made after the main loop has stopped for good. On Unix every marshalled call, now including Destroy, queues on the default GLib context and runs only if the UI thread iterates again. Windows behaves the same, and macOS's performOnMain waits instead. Pick one rule, and say what happens to a Close from a goroutine after App.Wait returns.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:29:11Z

### Owner decisions, 2026-09-27

- **Package layout:** move the desktop services out of the root package. Rejected: one package. Recorded in TKT-01M3J59M1H9PZ04J2C9JJZ7V13 (Move desktop services out of the root package).
- **Go minimum:** 1.26. Rejected: 1.25 with a pinned old x/sys. Recorded on TKT-01M3HWWRXMN56AG2GNC3M92GWZ (Decide tuohi's support tiers and make CI match them).
- **`App.Open`:** drop `file:`. Rejected: keep it and document the risk. Recorded in TKT-01M3J59M32VGK83J7JKYPKSHJE (Drop file: from the schemes App.Open accepts).

### On criterion 2

"Support tiers and testing" was answered by handing the tier choice to TKT-01M3HWWRXMN56AG2GNC3M92GWZ, which already owns it, with the facts it needs recorded in the doc. The review chose no tiers itself. Every other input has a decision in `docs/architecture.md`.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:32:26Z

### Review disposition, PR #4, terva-review run 15d45699 on 03bddbe

- **high: order file-scheme rejection before external-navigation forwarding. Fixed.** TKT-01M3HWWRT7X1RZZYY6KFEP0ERE (Let only trusted origins call a view's Go bindings) now depends on TKT-01M3J59M32VGK83J7JKYPKSHJE (Drop file: from the schemes App.Open accepts), and `docs/architecture.md` states the ordering and why.
- **medium: do not present a Host check as protection from local processes. Fixed.** TKT-01M3J59M4EJRPMKHWBS7K4XD3S is retitled "Guard tuohi's loopback server and settle its idle shutdown", and now separates the two threats:
  - a Host check stops DNS rebinding;
  - an unguessable per-server token keeps out other local users;
  - processes running as the same user are documented as able to read `App.FS` regardless.

  A criterion for the token was added, and the doc says the same.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:33:30Z

Review disposition, PR #4, terva-review run c5d9701a. Medium finding, do not issue the access cookie to an unauthenticated first requester: fixed. TKT-01M3J59M4EJRPMKHWBS7K4XD3S (Guard tuohi's loopback server and settle its idle shutdown) now says the cookie may only answer a request that already presents the token, and must never be set for one that did not.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:35:06Z

Review disposition, PR #4, terva-review run 58d99346. Two medium findings, both fixed. (1) Do not offer unlinking a held lock file as a fix: it still permits two primaries. A superseding note on TKT-01M3HWWRVGMZTXBYJ86FCTH0V8 requires the lock file never be unlinked, and the doc now says so. (2) Prevent timed-out UI work from executing later: the timeout rule was wrong. The threading rule is now a pending, running, or cancelled state on each operation, with atomic claim and cancel, and callers give up only when the loop owner marks the loop stopped. It is recorded in a superseding note on TKT-01M3J1H8CPMZX9EJX8R2CQRA6P and in the doc's 'One threading rule'.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:36:38Z

Supersedes 'On criterion 2' in the owner-decisions note. The review now records a tier decision, with rationale, in docs/architecture.md and on TKT-01M3HWWRXMN56AG2GNC3M92GWZ: Tier 1 is Linux, macOS, and Windows, and Tier 2 is FreeBSD and NetBSD, cross-built only. Every input now has a decision, so the tick on criterion 2 stands. Review disposition, PR #4, terva-review run 711b661d. Medium, define the App.FS allowlist using the origin actually loaded: fixed, with a superseding note on TKT-01M3HWWRT7X1RZZYY6KFEP0ERE and the doc. Low, record a support-tier decision: fixed as above.

## Summary

Landed through Forgejo PR #4 (terva-sh/tuohi), branch `spike/architecture-review`.

- **`docs/architecture.md`** records the target shape, and the README links it:
  - the loopback consumer comes first;
  - the root package is the window, and the desktop services move to subpackages;
  - an unexported engine interface and a shared bridge core;
  - one threading rule: safe from any goroutine, with a pending, running, or cancelled state on each marshalled operation and no timeouts;
  - an origin allowlist per view, taken from the URL the engine actually loads, with one gate and a navigation policy;
  - permissions denied by default;
  - `App.FS` serving kept, with its loopback server guarded;
  - upstream purego;
  - Tier 1 Linux, macOS, and Windows, and Tier 2 FreeBSD and NetBSD;
  - Go 1.26.
- **Owner decisions,** all 2026-09-27: split the packages, Go 1.26, and drop `file:` from `App.Open`.
- **Filed:** TKT-01M3J59M0V through TKT-01M3J59M7A, ten tickets. Decisions were noted on eight existing tickets, dependency links set, and the off-thread ticket widened to every engine.
- **Review:** five terva-review rounds. Each finding was fixed and recorded in a note here:
  - the navigation policy ordered after `file:` is dropped;
  - the loopback server's two threats separated, and its cookie bootstrapped only from the token;
  - the lock file kept for good;
  - timeouts replaced by atomic operation states;
  - the allowlist taken from the resolved origin;
  - the tier decision recorded.

The claims the doc marks INFERRED, and every macOS and Windows behaviour, come from reading code, not from running it.
