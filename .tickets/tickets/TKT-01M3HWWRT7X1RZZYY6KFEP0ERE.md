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
  actor: agent:claude-code/t3code-92c88910
  branch: feat/bridge-origin-gate
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-92c88910
  commit: 9f7a0af129d0cb3b3d51f03c20ff3315750ea679
  session: null
  claimed_at: 2026-09-27T20:10:52Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-28T02:21:18Z
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

- [ ] A page from an origin the application did not allow cannot call any binding, on all three engines
- [ ] Every engine's message handler checks the sender's origin
- [ ] Top-level navigation away from allowed origins is refused or opened in the system browser
- [ ] A loopback-served interface works with the default policy

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
