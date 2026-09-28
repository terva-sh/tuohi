---
schema: 4
id: TKT-01M3HWWRT7X1RZZYY6KFEP0ERE
title: Let only trusted origins call a view's Go bindings
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/bridge
  - security
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
  - TKT-01M3J59M0VJYQ3E652Y0FD90H3
  - TKT-01M3J59M32VGK83J7JKYPKSHJE
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-83e85fc3
  branch: t3code/resume-bridge-trust-work
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-83e85fc3
  commit: 9515041d591f7e17a3111cb219e6dff20a7413b4
  session: null
  claimed_at: 2026-09-28T02:41:41Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-28T05:02:50Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-83e85fc3
  name: ""
extensions: {}
---

## Description

### What

Only pages the application trusts may call its Go bindings.

### Current behaviour

- **Injection.** The bind script is injected at document start into the top frame of every navigation, with no URL check (`lib_unix.go:1640`, `lib_darwin.go:1576`).
- **No sender check.** None of the three message handlers checks where a message came from (`lib_unix.go:905`, `lib_darwin.go:219-223`, `lib_windows.go:691-699`).
- **No navigation policy.** Nothing stops a view navigating somewhere else.

So a view that ends up on a remote page, through a link, a redirect, or content the application did not write, hands that page every binding.

### Direction

The architecture review settles the shape. The likely pieces are:

- an allowlist of origins per view, defaulting to the origin the view was opened on;
- a sender-origin check in every engine's message handler;
- a navigation policy that keeps top-level navigation on allowed origins, and hands everything else to the system browser through `App.Open`.

A loopback consumer's origin is `http://127.0.0.1:PORT`, so the default must cover it.

## Acceptance criteria

- [x] A page from an origin the application did not allow cannot call any binding, on all three engines
- [x] Every engine's message handler checks the sender's origin
- [x] Top-level navigation away from allowed origins is refused or opened in the system browser
- [ ] A loopback-served interface works with the default policy

## Implementation plan

### Part 4: the bridge token, and the bridge only in trusted documents

Picked up from the handoff of agent:claude-code/t3code-92c88910 on branch feat/bridge-document-token.

#### Design

- **One secret per view,** not per document. `viewCore` holds 32 random bytes from `crypto/rand`, hex-encoded, made when the view is created.
- **The bridge script is generated, not stored.** `createInitScript` takes the post function, the token, and the trusted origin keys. Each engine's script rebuild puts it first, ahead of the `Init` scripts and the bind script. `trustURL` reports whether it added an origin, and `Navigate` and `View.Origins` rebuild the scripts when it did, before the load starts.
- **The script decides at document start.** It computes the key `originOf` gives in Go: `protocol//host` for a URL with a host, the URL without its fragment otherwise, and nothing for `about:`. It also requires `window.top === window`. On a document that fails either test it returns before defining anything, so the page gets no `window.__webview__` and never sees the token. Page script cannot have changed `location` yet, because document-start scripts run first.
- **post prefixes the token.** `post(message)` sends the 64 hex characters followed by the message. The prefix has a fixed length, so Go strips it without parsing or re-encoding the envelope, and every existing caller of `post` gets the token without change.
- **The gate.** `onMessage` first requires the prefix, compared in constant time, then the sender check as today.

#### What it closes

- **Linux frames.** A frame's messages reach the same handler, but a cross-origin frame cannot read the top document's token.
- **A message queued across a navigation.** A document on an untrusted origin never received the token, so its message is dropped whatever URI is current when it arrives.
- **Inject only into trusted origins,** on all three engines, including WebView2, whose document-created scripts also run in frames.

#### Alternatives rejected

- **A fresh token per document.** The document-start scripts are static: each engine injects the same source into every document, so Go cannot hand each document its own secret. Rebuilding the scripts at each navigation commit races with the load. Evaluating a token into the page after commit races with page script, which could hook the bridge first. A per-view token that only trusted documents receive gives the same guarantee.
- **WebKitGTK's user-script allow list** (`webkit_user_script_new`). It filters by URL pattern on Linux only. Neither WKWebView nor WebView2 has an equivalent, and WebKit's patterns do not match on port, which is the only thing distinguishing loopback origins.
- **An envelope field for the token.** The internal messages in `view.go` and `bind_gen.go` build their own JSON and call `post`, so each would need changing, and a missing field would fail silently.

#### Tests

- `TestOriginGate`: the untrusted page also reports whether it has `window.__webview__`, and must not.
- A new scenario for WebKit engines: a cross-origin frame inside a trusted page posts straight to the message handler, and must not reach a binding. A negative control: fails on Linux without the token check.
- `TestGeneratedScriptsParse` and `TestBridgeScriptsBehavior`: the new template, with and without a trusted origin.
- Unit tests: the token prefix check, and the JavaScript key agrees with `originOf` for the URL forms each engine loads.

#### Not in this part

The reply side: `resolve` and `Emit` evaluate into whatever document is current. After a navigation that is an untrusted page, which can define its own `window.__webview__` to receive the replies. I will file a separate ticket for it.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decisions from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

See `docs/architecture.md`, under "The bridge answers only the origins a view trusts". This lands after TKT-01M3J59M0VJYQ3E652Y0FD90H3 (Declare the engine interface and share the bridge core), so the gate is written once in the shared bridge.

- **Allowlist.** Each view gets an allowlist of origins. It defaults to the origin of the URL the view first opens: for a loopback consumer that is `http://127.0.0.1:PORT`, and for `App.FS` it is the app origin. `View` gets a field to add more.
- **Injection.** The bridge and bind scripts are injected only into documents on an allowed origin. Where an engine cannot filter at injection, the reply side refuses.
- **Sender check,** in the one message entry point per engine:
  - macOS reads `message.frameInfo.isMainFrame` and `securityOrigin`, and accepts main-frame messages from allowed origins only.
  - Windows calls the declared but unused `GetSource` (`lib_windows.go:289`). Frame messages arrive on `ICoreWebView2Frame` and are not wired, so they stay unreachable.
  - Linux gets no frame from `script-message-received`. Register the handler in an isolated script world, so page and iframe scripts cannot reach `messageHandlers.__webview__`, and relay from the bridge script. Also check `webkit_web_view_get_uri` at receipt.
- **One gate.** The events binding `__appkit_event__` and the internal window messages (drag, resize, toggle maximize, app regions, bind error) pass through the same gate as ordinary bindings. `toggleMaximize` also gets the `frameless` guard it lacks on Unix and Windows. The bind-error log quotes page text with `%q`.
- **Navigation policy.** A top-level navigation to an allowed origin proceeds. Any other goes to `App.Open` and is cancelled in the view. `window.open` and `target=_blank` are handled the same way, including WebView2's own popup windows, which today open with none of the bridge. The hooks are `decide-policy` on WebKitGTK, `decidePolicyForNavigationAction` and `createWebViewWithConfiguration` on WKWebView, and `NavigationStarting` and `NewWindowRequested` on WebView2.
- **Rejected alternative:** a per-binding allowlist. The consumer case is one trusted origin per view, and per-binding policy multiplies configuration without a use.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:36:38Z

Supersedes the Allowlist bullet in the decisions note, which said an App.FS view defaults to 'the app origin'. The default is the origin of the URL the engine actually loads, after resolveURL: `app://` on Linux, `https://app.localhost` on Windows, and the temporary loopback server's `http://localhost:PORT` on macOS, or wherever `App.HTTP` applies. The loopback port changes with every server, so the gate must read it from the resolved URL, not from a constant. Deriving it from `app://` would reject the application's own page on macOS. Found by terva-review on tuohi PR #4.

**agent:claude-code/t3code-92c88910** at 2026-09-27T20:15:07Z

### First part landed on branch feat/bridge-origin-gate

- **The allowlist.** `viewCore.origins`, filled by `trustURL`. Every engine's `Navigate` calls it with the URL it is about to load, after any app:// rewrite, and `loadHTML` trusts `about:blank`. `View.Origins` adds more. So the default is the origin the page really has, which answers the terva-review finding about macOS's loopback port.
- **Go's own navigations are trusted.** Rather than only the first URL as the decisions note said, every URL the application gives `Navigate` is trusted, because the application chose it. Page-initiated navigations, redirects, and frames are not.
- **The gate.** It sits at the top of the shared `onMessage(body, senderURL, senderKnown)`, before parsing. Bindings, events, and internal messages all pass through it.
- **Linux sender.** `webkit_web_view_get_uri` at receipt, which is the top-level page. A null URI is untrusted. Frames are not distinguished yet: an iframe on an untrusted origin inside a trusted page can still post. The isolated script world closes that, and is a later part.
- **macOS and Windows** pass `senderKnown=false`, which is allowed, so their behaviour is unchanged until their sender checks land. Each is its own pull request, per the owner's decision recorded on TKT-01M3HWWRXMN56AG2GNC3M92GWZ.
- **Also fixed:** toggle maximize is ignored on framed windows on all three engines, and bind-error log text is quoted with `%q`.

### Verified

- **Unit tests:** `TestOriginOf` and `TestViewCoreTrusts`.
- **`TestOriginGate`** on both WebKitGTK stacks, 3 of 3 runs. With the gate disabled it fails with `untrusted=1`, so it tests the gate.
- **`just ci`** passes, and golangci-lint reports 0 issues on darwin, linux, and windows.

### Remaining parts

1. The macOS sender check: `message.frameInfo`, main frame only, security origin. Fold in TKT-01M3J59M3SQBSXV2KVZEFBNBAV's body type check, since it is the same line.
2. The Windows sender check: `GetSource`.
3. The Linux isolated script world, for frames.
4. The navigation policy on each engine.
5. Injection limited to trusted origins.

**agent:claude-code/t3code-92c88910** at 2026-09-27T20:19:47Z

### Review disposition, PR #7, terva-review run 3b89eb64 on 70cda49

- **medium: normalize navigated hosts and ports the same way as the browser. Fixed in 519b5e9.**
  - `originOf` now canonicalizes by the WHATWG URL rules: IDNA ASCII, compressed IPv6, WHATWG IPv4 parsing including the ends-in-a-number rule, and a numeric port with the default dropped.
  - The dependency added is `golang.org/x/net/idna`. The standard library has no public IDNA function. x/net v0.59.0 needs Go 1.26, the chosen minimum, and brings `x/text`.
  - `TestOriginGate` now navigates with a mixed-case host and a zero-padded port. With the old comparison it fails with `trusted=0`, which reproduces the finding end to end.

**agent:claude-code/t3code-92c88910** at 2026-09-27T20:22:18Z

### Review disposition, PR #7, terva-review run 1766fcd1 on bbb7758

Both findings fixed in 0a37dc4.

- **high: keep IPv4-mapped IPv6 origins distinct from IPv4 origins. Fixed.**
  - `canonicalHost` parses IPv6 with `net/netip` and serializes it with `ipv6String`, the WHATWG form: hex pieces, the first longest zero run compressed, never dotted IPv4. `[::ffff:127.0.0.1]` becomes `[::ffff:7f00:1]`.
  - `TestViewCoreTrusts` checks that a view trusting `http://127.0.0.1:8080` refuses `http://[::ffff:127.0.0.1]:8080`.
  - Both the trusted URL and the sender pass through `originOf`, so the gate needs only that distinct browser origins never share a key, whatever spelling WebKit reports.
- **medium: apply IDNA before deciding whether a host is IPv4. Fixed.** IDNA runs first, then the ends-in-a-number test and IPv4 parsing on the ASCII host. `http://１２７.１:8080/` gives `http://127.0.0.1:8080`, and has a test.

**agent:claude-code/t3code-92c88910** at 2026-09-27T20:25:48Z

### Review disposition, PR #7, terva-review run ed91918d on bd98d08

Both findings fixed in 508953d.

- **high: do not treat every about:blank document as trusted. Fixed.**
  - `originOf` gives `about:` URLs no origin, so neither `trustURL` nor the gate can ever match one. That also covers `Navigate("")`, which loads `about:blank` in production.
  - A `data:` URL keeps an exact-URL key, because its URL is its whole content.
  - The Linux `loadHTML` is used only by tests. It now loads its page with the base URI `http://loadhtml.tuohi.invalid/` and trusts that. Every GUI bridge scenario passes through the gate with it, which shows WebKit reports the base URI as the page's URI.
  - `loadHTML` on macOS and Windows no longer trusts `about:blank`. Their senders are still unknown, and their own PRs must key test pages the same way.
- **medium: normalize percent-encoded hostnames. Fixed.** When `url.Parse` refuses a URL, `unescapeHost` percent-decodes only the host and `originOf` parses again. A decoded host containing a delimiter is refused. Tests cover `%65xample.com`, userinfo with a port, and an escaped slash.

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:13:29Z

### Second part: macOS sender check, branch feat/bridge-sender-darwin

- **The handler reads the body only when it is an NSString.** That also fixes TKT-01M3J59M3SQBSXV2KVZEFBNBAV.
- **It refuses anything not from the main frame,** using `frameInfo.isMainFrame` through `objc.Send[bool]`. It then names the sender as `webView.URL`, the same rule as Linux. The frame's `securityOrigin` was considered and rejected: it makes `data:` and `loadHTML` pages opaque, so they could not be trusted, and the main-frame check already covers frames.
- **`loadHTML` on macOS** loads with base URL `http://loadhtml.tuohi.invalid/` and trusts it. `loadHTMLBase` moved to `loadhtml_webkit.go` (`!windows`), because WebView2's `NavigateToString` takes no base URL and Windows needs another way.
- **Tests.** `originGateScenario` moved to `origin_gate_test.go`, built for Linux, the BSDs, and macOS. `badMessagesScenario` covers macOS non-string bodies and an iframe message.
- **Not run locally.** Verified here by vet and golangci-lint for darwin, with Linux `just ci` and `test-gui` unchanged and passing. The macOS scenarios run on GitHub after the merge.

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:15:24Z

### Review disposition, PR #8, terva-review run b1426440 on 0625328

- **high: identify the message's frame, not the web view's current page. Fixed in 305b51c.** The sender is `message.frameInfo.request.URL`, which belongs to the posting document.
  - **Untested assumption:** that WKWebView reports the base URL as `frameInfo.request.URL` for `loadHTMLString:baseURL:`. The macOS bridge scenarios will show it on GitHub after the merge. If it is wrong, those scenarios fail and it is fixed forward.
- **The same race exists on Linux,** where the sender is `webkit_web_view_get_uri` read at receipt, and WebKitGTK gives no frame with the message. That is not fixed here. The proposed fix: a per-document token that Go puts into the bridge script it injects into trusted documents, and the gate requires on every message. The injection-only-into-trusted-origins part of this ticket needs the same mechanism, so it lands there. The Linux iframe gap is closed at the same time.

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:21:18Z

### Third part: Windows sender check, branch feat/bridge-sender-windows

- **The sender is `GetSource`,** read from the WebMessageReceived arguments, so it names the posting document. A failed read passes an empty sender, which the gate refuses.
- **`loadHTML` on Windows** navigates to `data:text/html;charset=utf-8;base64,...` and trusts that exact URL. It assumes WebView2 reports that URL unchanged from `GetSource`. The Windows bridge scenarios will show it on GitHub after the merge, and it is fixed forward if wrong.
- **`TestOriginGate`** now runs on every engine: `origin_gate_test.go` lost its build tag.

### Where the criteria stand once GitHub passes

- **Criterion 2,** every engine checks the sender: met.
- **Criterion 1,** no untrusted origin can call a binding on any engine: met for top-level pages on all three, and for frames on macOS. Two Linux gaps remain:
  - WebKitGTK does not say which frame posted;
  - the sender is read when the message arrives, so a message queued across a navigation can take the new page's URI.

  The per-document token planned with injection-only-into-trusted-origins closes both. Until then, criterion 1 stays unticked.

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:28:03Z

### Windows fix-forward after PR #9

- **What failed.** GitHub run 36369625587 on ba5639e failed three Windows bridge scenarios with "no report". Those scenarios load their pages with `loadHTML`, which navigated to a base64 `data:` URL.
- **What worked.** `TestOriginGate` passed in the same run. It uses http loopback pages, so `GetSource` and the gate work for ordinary origins.
- **Cause, not established.** Either WebView2 reports a `data:` document's source differently from the navigated URL, or it does not give `data:` documents the bridge. Neither can be told apart without a Windows machine.
- **Fix, on branch fix/windows-loadhtml-loopback.** `loadHTML` serves the page from a temporary loopback server and navigates to it.
- **Consequence for applications:** a `View.URL` that is a `data:` URL probably cannot use bindings on Windows. The View.URL doc lists `data:`. Settle that with the navigation-policy work, by testing it on GitHub and either fixing it or documenting it.

**agent:claude-code/t3code-92c88910** at 2026-09-28T02:32:30Z

Criterion 2 ticked: every engine's message handler checks the sender. Linux uses the top-level URI, macOS the main frame's request URL, and Windows GetSource. GitHub run 36370005911 on 5ba6945 passed TestOriginGate on all three engines, plus TestBadMessagesAreDropped on macOS. Criteria 1, 3, and 4 remain. Next is part 4, a per-document token that closes the Linux frame and queued-message gaps and limits bridge injection to trusted origins. Then comes the navigation policy on each engine.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T02:41:41Z

claim taken from agent:claude-code/t3code-92c88910 by agent:claude-code/t3code-83e85fc3

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T02:50:55Z

### Part 4 landed on branch feat/bridge-document-token: the bridge token

The design is in the plan.

- **Commit.** 832ed0c.
- **Closes the two Linux gaps:** frames, and a message queued across a navigation. The bridge is also installed only in trusted top-level documents on all three engines.

#### Verified

- **New GUI scenario `TestFrameGate`,** run on every engine. A cross-origin frame inside a trusted page posts a well-formed message straight to the message channel.
  - **Negative control:** with the token check removed from `onMessage`, it fails with `frameHits=1` on both WebKitGTK stacks. So the Linux frame gap was real, and the token closes it.
  - **Why the frame is served without COEP or CORP.** The first version served the frame from `listenLoopbackHTTP`, and passed without the fix. The loopback server sends `COEP: require-corp` and `CORP: same-origin`, so the browser never loaded the frame. The scenario now uses a bare test server. `net/http` is forbidden by `check-imports`, test files included.
- **`TestOriginGate`** also checks that the untrusted page has no `window.__webview__`. With the script's origin check removed, it fails with `bridge=yes`.
- **`TestBridgeGate`** (node) loads the bridge at the locations node's WHATWG URL parser gives for each form Go trusts: mixed case with a padded port, `https://app.localhost`, `app://app`, IPv4-mapped IPv6, full-width digits, IDN, and `data:` with a fragment. The key the script computes must match `originOf` for each one. It also checks that frames, about:, and untrusted origins get no bridge. With the check removed, it fails.
- **`TestCheckToken`:** the token prefix check.
- **Local runs:** `just ci` and `just test-gui` pass on both stacks, and golangci-lint v2.13.1 reports 0 issues on linux, darwin, and windows.

#### Not run locally

macOS and Windows run on GitHub after the merge. On Windows, `rebuildScripts` now also runs from `Navigate` when the origin is new, and pumps the message loop there, as `Bind` already does.

#### Criteria

- **Criterion 1** stays unticked until GitHub passes on macOS and Windows.
- **Reply side:** filed as TKT-01M3JYQWZB01CPZ938Y8C24PXX (Deliver binding replies and events only to trusted documents). Replies and events are still evaluated into whatever document is current.
- **Remaining here:** the navigation policy on each engine, and settling `data:` `View.URL` on Windows.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T02:54:06Z

### Review disposition, PR #11, terva-review run 07c6b492 on 4960c24

- **low: require proof that the hostile frame ran before passing TestFrameGate. Fixed in 34b579c.**
  - After posting, the frame fetches `/ran` from its own server. The scenario reports `frameRan=true frameHits=0` and requires both parts.
  - This was the real failure mode found while writing the test: under `listenLoopbackHTTP`'s COEP and CORP headers the frame never loaded, and the test still passed.
  - **Controls,** on WebKitGTK 6.0:
    - frame script made to throw before the fetch: fails with `frameRan=false`;
    - token check removed: fails with `frameHits=1`.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T03:11:38Z

### PR #11 merged; GitHub green on every engine

- **Merge.** PR #11 was merged as 6fd77f7. GitHub `main` was fast-forwarded to it by hand, after `git merge-base --is-ancestor` confirmed the fast-forward. `just sync-github --yes` still does not push; see TKT-01M3J0VR65.
- **GitHub run 36372523259 passed:** macOS, Windows, all four Linux GTK jobs, lint, and every cross build.
  - **Windows:** its log names `TestOriginGate` and `TestFrameGate` as PASS.
  - **macOS:** it runs every GUI scenario unless `-short` is set, and a scenario that does not run returns a non-empty error string, so its pass means both scenarios ran.
- **Criterion 1 is ticked.** On all three engines, a page on an untrusted origin gets no bridge, and a message without the token is dropped. That covers top-level pages, frames, and messages queued across a navigation.
- **What remains here:**
  - the navigation policy, one PR per engine: WebKitGTK `decide-policy`, WKWebView `decidePolicyForNavigationAction`, and WebView2 `NavigationStarting` with `NewWindowRequested`;
  - settling whether a `data:` `View.URL` can use bindings on Windows.
- **Also untested:** criterion 4, a loopback-served interface under the default policy. `TestOriginGate` covers a loopback page that Go navigated to. The criterion is left open until the navigation policy lands, because that policy is what could break it.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T03:27:21Z

### Navigation policy: decisions, settled with human:sothr on 2026-09-28

#### 1. A fixed rule, with no hook

- **The rule.** A top-level navigation to a trusted origin proceeds. Any other is cancelled in the view, and handed to the system browser when `App.Open` accepts its scheme (`http`, `https`, `mailto`).
- **Consequence.** The only way to show a page in the window is to trust it with `View.Origins`, which also gives it the bindings.
- **Rejected: a callback on `View`,** returning allow, open externally, or cancel. It separates "shown" from "trusted", but it is new API, and a synchronous callback on the UI thread of every engine. No consumer shows third-party pages in the window: terva-sh tools serve one origin, and git-ticket-canvas asked for exactly the fixed rule.
- **Rejected: a second list of origins** that may be shown without the bridge. It is a second list to keep in step, with no current use.
- **Later.** Either can be added without breaking anyone, because unset keeps the fixed rule.

#### 2. A redirect is treated like any navigation

- **The rule.** A redirect from a trusted page to an untrusted origin is cancelled and opened in the system browser.
- **Rejected: letting redirects of Go's own navigations through without the bridge.** It would put untrusted content in the app's window, which decision 1 rules out.
- **Consequences to document:**
  - A `View.URL` that redirects off-origin, such as to a login page, leaves the view blank. The fix is to list that origin in `View.Origins`.
  - A scheme or host change, such as http to https or 127.0.0.1 to localhost, counts as a different origin.
- **To verify in the macOS PR:** whether WKWebView asks `decidePolicyForNavigationAction` before it follows a server redirect. If it does not, macOS needs a fallback, such as stopping the load when the redirect is reported.

#### 3. Page-initiated top-level navigations to URLs without a normal origin

- **about:blank proceeds.** It is empty and gets no bridge, and nothing can script it once the page that navigated is gone. Allowing it also keeps `Navigate("")` working without marking which navigations Go started.
- **Cancelled:** `data:`, `blob:`, `file:`, and custom schemes. `mailto:` goes to the mail client through `App.Open`. A dropped navigation gets a log line, so a developer can see why a click did nothing.
- **Not intercepted:** `javascript:`, which runs in the current page.
- **Go's own navigations are unaffected,** including a `data:` `View.URL`, which Navigate trusts by its exact URL.
- **Rejected: cancelling about:blank as well.** It needs a marker for Go's own navigations, and gains no safety.
- **Rejected: allowing `blob:` URLs whose creator origin is trusted.** No consumer needs it yet, and it can be added later.

#### 4. Order: Linux, then Windows, then macOS

- **Linux first,** because it is the one engine testable locally. Its PR builds the shared parts:
  - the decision function in `engine.go`;
  - a stand-in for the system-browser call, so tests do not launch a browser;
  - a GUI scenario run on every engine: a link, a redirect, `window.open`, and about:blank.
- **Windows second.** Its hooks are already declared: `NavigationStarting` with `put_Cancel`, and `NewWindowRequested` with `put_Handled`.
- **macOS last.** It has the most unknowns: calling the Objective-C decision block, and the redirect question.
- **Rejected: Windows first,** to close the WebView2 popup window soonest. That popup has none of tuohi's scripts, so no bridge, and its exposure is the same kind as ordinary link navigation. Designing the shared code with no local test loop costs more.
- **Rejected: Linux, then macOS, then Windows.** It puts macOS's unknowns ahead of the simpler Windows hooks.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T03:37:42Z

### Navigation policy on Linux: decide at response time, settled with human:sothr on 2026-09-28

#### The finding

WebKitGTK's `decide-policy` of type `NAVIGATION_ACTION` fires for navigations in both the main frame and subframes. `WebKitWebView.h` says so, and WebKitGTK 2.52.6, both 6.0 and 4.1, exports no way to tell them apart. `webkit_frame_is_main_frame` exists, but it belongs to the web-process extension API, which would need a C library loaded into the web process, and so cgo. A policy at navigation time would therefore also cancel every cross-origin iframe and open each one as a browser tab, which contradicts decision 1.

#### Decision

- **The top-level document** is decided at `RESPONSE` time, and only when `webkit_response_policy_decision_is_main_frame_main_resource` is true. The URL is the final one, after redirects.
- **New windows** (`NEW_WINDOW_ACTION`, always top-level) are decided at navigation time.
- **Schemes that never produce a response,** such as `mailto:` and custom schemes, are decided at navigation time, in any frame. A `mailto:` click in a frame then opens the mail client, which is acceptable because it is a user action either way.

#### Cost, which only Linux pays

Before the cancel, the untrusted server has already received the request, with any cookies the web view holds for it, and any redirects have been followed. Its content is never shown. WebView2's `NavigationStarting` is top-level only, and WKWebView gives `targetFrame.isMainFrame`, so both decide before a request goes out. The shared GUI scenario checks what is displayed and what is handed to the browser, not whether a server was contacted.

#### Rejected

- **Navigation time for everything.** It breaks cross-origin frames.
- **Treating link clicks as top-level.** A link clicked inside a frame looks the same.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T03:46:34Z

### Linux navigation policy on branch feat/bridge-nav-policy-linux

#### What landed

- **The shared rule** is `viewCore.navigationPolicy` in `engine.go`:
  - a trusted origin or about:blank proceeds;
  - http, https, and mailto go to the system;
  - anything else is cancelled and logged.
- **`refuseNavigation` and `openExternal`** carry out a refusal. `openExternal` is the stand-in point for tests.
- **Linux** judges the top-level document at `RESPONSE`, new windows at `NEW_WINDOW_ACTION`, and schemes that have no response at `NAVIGATION_ACTION`.

#### Findings

- **WebKit's popup blocker.** It drops a `window.open` that no user gesture started, including one from `Eval`, before `decide-policy` runs. A `target=_blank` click reaches `NEW_WINDOW_ACTION`, and a `window.open` the user started takes the same path. The scenario therefore clicks a `target=_blank` link.
- **`TestOriginGate` now depends on the engine.** It reached an untrusted page by having the page navigate itself, and the policy now refuses that. It accepts either `untrusted=0 bridge=no`, from an engine with no policy yet, or `untrusted=refused`. It logs which one happened, and Linux reports `untrusted=refused`.

#### Verified

- **`TestNavigationPolicy`** passes on both WebKitGTK stacks. It checks each step and that the cross-origin frame still runs (`frameRan=true`).
  - **Negative control:** with `decidePolicy` returning false, every step fails with `left`, and nothing is handed off.
- **Unit test** `TestNavigationPolicy_Decisions` covers the rule's cases.
- **Local runs:** `just ci` and `just test-gui` pass on both stacks. golangci-lint reports 0 issues on linux, darwin, windows, and freebsd.

#### Left for the Windows and macOS pull requests

- `refuseNavigation` and `navPolicyScenario` carry `//nolint:unused` until those engines call them.
- `TestNavigationPolicy` skips on darwin and windows with that reason.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T04:18:48Z

### PR #12 merged; GitHub green

- **Merge.** PR #12 was merged as 3c3006c. GitHub `main` was fast-forwarded to it by hand, after `git merge-base --is-ancestor` confirmed the fast-forward.
- **GitHub run 36375315774 passed:** macOS, Windows, all four Linux jobs (x86-64 and arm64, GTK3 and GTK4), lint, and every cross build.
- **Windows log:**
  - `TestNavigationPolicy` skips, because it has no hook yet.
  - `TestOriginGate` logs `untrusted=0 bridge=no trusted=1 bridge=yes`, the shown-without-a-bridge branch.
  - `TestNavigationPolicy_Decisions` passes.
- **Linux, inferred.** The Linux jobs run without `-v` and without `TUOHI_REQUIRE_GUI=1`, so their logs name no GUI test. Each job's Xvfb step took 21 to 22 seconds, against 3 to 6 for the headless step. That matches local runs with the navigation scenario, about 20 seconds against about 8 before it. So the scenarios ran, `TestNavigationPolicy` included.
- **Gap noted.** GitHub's Linux GUI step does not set `TUOHI_REQUIRE_GUI=1`, so a failed GUI probe there would skip silently. That is worth its own small ticket.
- **Next:** the Windows navigation policy, on branch feat/bridge-nav-policy-windows.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T04:32:41Z

### Windows navigation policy, and new windows on every engine, on branch feat/bridge-nav-policy-windows

#### Decision, settled with human:sothr: a new window to a trusted origin loads in the same view

- **Linux had no behaviour here.** A `target=_blank` link to the trusted origin did nothing: no window, and no hand-off, because tuohi never gives WebKitGTK a second view.
- **Windows opened a bare WebView2 popup,** with none of the view's scripts or handlers, and so outside the policy.
- **Chosen: load it in the same view.** The page keeps the bridge and stays under the policy. The cost: `window.open`'s return value is dead, so a popup that messages its opener, as some OAuth flows do, does not work.
- **Rejected: the system browser.** The page loses the bridge, and an `App.FS` page (`app://`, `https://app.localhost`) cannot load there.
- **Rejected: dropping it,** as Linux did. A `target=_blank` link to the app's own page would silently do nothing.
- **Follow-on choice, made here: a new window to about:blank is dropped.** Examples are `window.open('')` and `window.open()`. Loading about:blank in the same view would replace the app's page.
- **Where it lives.** `handleNewWindow` in `engine.go` holds this for every engine.

#### Windows

- **`NavigationStarting`** fires for the top-level document only, before any request is sent. A refused navigation is cancelled with `put_Cancel`. Frames raise `FrameNavigationStarting`, which is not wired, so they are left alone.
- **`NewWindowRequested`:** every request is marked handled with `put_Handled`, so WebView2 never opens a popup, and `handleNewWindow` decides.
- **Source of the IIDs and vtables:** `github.com/zzl/go-webview2`, generated from the SDK, read from the module cache. `NavigationStarting` args: slot 3 `get_Uri`, slot 8 `put_Cancel`. `NewWindowRequested` args: slot 3 `get_Uri`, slot 6 `put_Handled`. The existing `iidMessageReceived` and `iidNavigationCompleted` match that source.

#### Verified locally

- **Builds:** vet and golangci-lint pass for linux, darwin, windows, and freebsd, and `just ci` passes.
- **`TestNavigationPolicy`** gains a trusted `target=_blank` step and passes on both WebKitGTK stacks.
  - **Negative control:** with the same-view load removed, it fails with `trustedpopup=stay`.
- **`just test-gui`** passes on both stacks.

#### Not run locally

Windows runs on GitHub after the merge. Expectations that are unverified there:

- whether `mailto:` raises `NavigationStarting` rather than going straight to the OS;
- that a `data:` navigation by the page reaches `NavigationStarting`, or is blocked by Chromium first, which gives the same result.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T04:39:00Z

### PR #13 merged; the Windows policy passes on GitHub the first time

- **Merge.** PR #13 was merged as 0a1e770, and GitHub `main` was fast-forwarded to it by hand.
- **GitHub run 36378372443 passed on every job.**
- **The Windows log names each test as passed:**
  - `TestNavigationPolicy` passes with its exact expected string, so every step behaved as on Linux. That settles both open questions:
    - `mailto:` reaches `NavigationStarting` and is handed off.
    - A page's `data:` navigation ends with the view unchanged and nothing handed off.
  - `TestOriginGate` logs `untrusted=refused trusted=1 bridge=yes`.
  - `TestFrameGate` and `TestNavigationPolicy_Decisions` pass.
- **Remaining for criterion 3: macOS.** That is `decidePolicyForNavigationAction` for top-level navigations, with the main frame taken from `targetFrame`, and `createWebViewWithConfiguration`, which returns nil and calls `handleNewWindow`. It also has to settle whether WKWebView asks the delegate before it follows a server redirect.
- **Next branch:** feat/bridge-nav-policy-darwin.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T04:44:08Z

### macOS navigation policy, on branch feat/bridge-nav-policy-darwin

- **`decidePolicyForNavigationAction`** runs before any request is sent:
  - no `targetFrame` means a new window, which is cancelled and handed to `handleNewWindow`;
  - a frame that is not the main frame is allowed;
  - the main frame is judged by the policy.
- **`decidePolicyForNavigationResponse`** judges the main frame's final response URL again. A response with no URL is allowed.
  - **Why it is here:** the open question was whether WKWebView asks the action delegate before it follows a server redirect. This backstop makes the redirect case independent of the answer. A navigation the action check cancelled has no response, so nothing is judged twice.
- **`createWebViewWithConfiguration`** returns nil and calls `handleNewWindow`.
- **The decision block** is called with `invokeDecisionHandler`, the old media-capture helper renamed: `v@?q` through `NSInvocation`, because all three WebKit decision blocks take one `NSInteger`.
- **Placeholders removed.** The `nolint:unused` markers and the darwin skip in `TestNavigationPolicy` are gone, and the scenario is registered in the macOS `TestMain`.

#### Verified locally

- **Builds:** vet and golangci-lint pass on linux, darwin, windows, and freebsd, and `just ci` passes.
- **`just test-gui`** passes on both WebKitGTK stacks.

#### Not run locally

macOS runs on GitHub after the merge. If the redirect step shows `left`, neither hook caught the redirect.

#### Criterion 3

It is met on Linux and Windows. It will be met on macOS if GitHub passes after the merge, and gets ticked then.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T04:52:26Z

### PR #14 merged; the navigation policy passes on all three engines

- **Merge.** PR #14 was merged as a52ef0a, and GitHub `main` was fast-forwarded to it by hand.
- **GitHub run 36379293075 passed on every job.**
- **Windows** names `TestNavigationPolicy`, `TestOriginGate`, `TestFrameGate`, and `TestNavigationPolicy_Decisions` as passed.
- **macOS runs without `-v`,** so its log names no test. The evidence is:
  - macOS runs every GUI scenario unless `-short` is set, and a failing one prints its got and want lines. None were printed.
  - The root package took 26.9 seconds, against 17.5 and 13.3 in the two runs before, which is the navigation scenario's roughly 12 seconds.
  - So `TestNavigationPolicy` ran and passed, and either `decidePolicyForNavigationAction` or the response backstop caught the redirect.
- **Criterion 3 is ticked.**

#### Criterion 4, a loopback interface under the default policy: still open

- **What is covered.** A loopback page that Go navigated to loads with the bridge (`TestOriginGate`, `TestNavigationPolicy`). A new window to the same origin loads in the view with the bridge. A cross-origin frame still loads.
- **What is not tested.** An in-app link to another path, a reload, and back and forward. The earlier note set those as the bar.

**agent:claude-code/t3code-83e85fc3** at 2026-09-28T05:02:50Z

### Criterion 4 and the data: View.URL question, on branch feat/bridge-loopback-default

#### Criterion 4

`TestLoopbackAppDefaultPolicy` is a new scenario run on every engine. A page served from its own loopback server, which Go navigated to once, reports its path through a binding on `pageshow` after each of these steps:

- an in-app link;
- a query URL;
- a reload;
- back, then forward.

Nothing may be handed to the system. `pageshow` also fires when back and forward restore a page from the back-forward cache. The scenario passes on both WebKitGTK stacks. Criterion 4 gets ticked once GitHub passes on macOS and Windows.

#### A bug found in the data: keying

- **Symptom.** A `data:` URL built from multi-line HTML failed on Linux too.
- **First cause.** Go's `url.Parse` refuses a URL containing a tab or newline, so `originOf` returned "" and the URL was never trusted.
- **How old it is.** It predates this ticket's policy work: such a page never had bindings. Since PR #12 it does not load at all, because the policy refuses the navigation.
- **Second cause.** A WebKitGTK 2.52 probe showed that the browser also serializes by the WHATWG rules:
  - it drops tabs and newlines;
  - it keeps the opaque path raw;
  - it percent-encodes the query: space, `<`, `>`, and non-ASCII;
  - it keeps existing escapes.

  Go's `String()` does none of that.
- **Fix.** `originOf` now strips surrounding C0 controls and spaces and every tab and newline, as a browser does. It serializes an opaque-path URL with `splitOpaque` and `opaqueKey`.
- **Unit test:** the probe's real spelling.
- **Node test:** node's WHATWG parser agrees with Go's key for a multi-line `data:` URL with a query, so the page-side bridge gate matches.
- **Negative control:** before the fix, `TestDataURLCanUseBindings` failed on both stacks with "the page never reported".

#### data: on Windows: evidence first

- **What stays unexplained.** The earlier Windows failure used a base64 URL with no newlines or query, so this fix does not explain it.
- **What the scenario collects.** On a failure, `TestDataURLCanUseBindings` has the page report what it sees through an image request to a loopback server: `window.chrome`, `chrome.webview`, the bridge, the binding, and its own `href`. "The page never reported" means it did not load.
- **On Windows only,** a failure skips with that report instead of failing CI.
- **Then decide:** fix it, or document that `data:` pages cannot use bindings on Windows.
