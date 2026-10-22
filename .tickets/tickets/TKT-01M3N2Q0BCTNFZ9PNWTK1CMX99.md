---
schema: 4
id: TKT-01M3N2Q0BCTNFZ9PNWTK1CMX99
title: Let a consumer's test see links handed to the system browser
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
  - area/engine-darwin
  - area/engine-windows
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT
    path: null
  - ref: t3-thread://v1/564d5d21-1210-473a-aa2b-918183ce9764
    path: null
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/prepare-library-changes
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: cf945c1c5152b33b4bf8f76b7cf91c4e1015d1be
  session: null
  claimed_at: 2026-10-22T20:12:41Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:38:34Z
updated_at: 2026-10-22T20:13:25Z
created_by:
  id: agent:claude-code/fe5548cb
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

### What

A consumer cannot see, in its own tests, that a view handed an outside link to the system browser on macOS.

### Found by

git-ticket-canvas, working ticket:git-ticket-canvas/TKT-01M3N0ZZFZKWNRCWQQ3GY2AYJT (Run the desktop window's GUI scenarios on macOS). On Linux its GUI scenario puts a fake `xdg-open` on PATH, clicks a link to another origin, and checks what the fake was given. On macOS, `openURL` (app_darwin.go:231) calls `NSWorkspace openURL:`, which has no PATH to stand in on. So the canvas skips that test on macOS, since following the link would open a real browser tab.

tuohi's own tests have a hook for this: the unexported `openExternal` variable (engine.go:704), replaced on the UI thread before a view exists. A consumer cannot reach it.

### Things to settle

- **The shape of the hook.** It could be a field on `App`, such as a function that receives each URL handed outside and replaces the system call when set. Or it could be an exported variable like `openExternal`. A field is scoped to one app and needs no rule about when it may be replaced.
- **Whether it covers `App.Open` as well** as a refused navigation, since both reach `openURL`.
- **Windows.** It has the same problem, so one hook should serve all three engines.

## Acceptance criteria

- [ ] A consumer's test can record every URL a view hands outside, without a real browser opening, on all three engines
- [ ] The hook's shape and when it may be set are documented

## Implementation plan

### Approach

Add App.OpenExternal func(string) error to the committed appConfig snapshot and capture it in every viewCore during App.Show. App.Open validates its URL before either path; native navigation and bridge interception keep the existing navigation policy and call an engine-scoped refusal method. A configured hook runs for outside links in a goroutine so the UI thread cannot block, and App.Open calls it synchronously and returns its error. Nil keeps the system opener. Remove the test-only global opener and migrate the shared native navigation scenarios to per-App hooks.

Add headless tests for scheme refusal, isolation, committed settings and error propagation, plus a public consumer example check using a link to an external origin and App.Open without launching a browser. Run just ci and both WebKitGTK GUI stacks with CGO_ENABLED=0. Native macOS/Windows execution is unavailable here; cross builds and shared scenarios are compilation evidence only until hosted CI runs after an authorized merge.

### Alternatives

An exported package variable would introduce global mutable state and cross-app interference. A navigation-only hook would leave App.Open untestable. A synchronous navigation callback could block the UI; a callback on a worker lets consumers record or open a link without blocking native event dispatch. Like other App fields, configure it before the first App method; later assignments do not change the committed scope.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: an App field that covers App.Open too

The owner decided, 2026-10-01. Add an `App` field, for example `OpenExternal func(url string) error`. When set, it replaces the system call for every URL handed outside: refused navigations and `App.Open` alike, on all three engines. It is scoped to one App, so it needs no rule about when it may be replaced, unlike the unexported package variable `openExternal`.

The alternatives lost for these reasons. A navigations-only field leaves `App.Open` untestable for the same reason. An exported package variable is global and has to be set before any view exists.

It does not block v0.1.0.

**agent:codex/t3-b903096c** at 2026-10-22T20:12:41Z

The owner selected this existing ticket on 2026-10-22 as one of five Kuiske upstream improvements. Reuse the committed App-scoped OpenExternal decision, covering App.Open and outside navigation on all engines; no exported mutable global. Source Kuiske thread: t3-thread://v1/564d5d21-1210-473a-aa2b-918183ce9764
