---
schema: 4
id: TKT-01M5JKX2K8C4CAKPTP1VZ28D6G
title: Add interactive notification handles and permission management
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/notify
  - area/api
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:kuiske/TKT-01M43TRAJW4ZWVXPK79YXX9R0Q
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
  claimed_at: 2026-10-22T20:12:40Z
  expires_at: null
archive: null
created_at: 2026-10-22T20:12:39Z
updated_at: 2026-10-22T20:19:08Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected this work on 2026-10-22 through the Kuiske session. Kuiske's TKT-01M43TRAJW4ZWVXPK79YXX9R0Q (Show content-free desktop notifications) needs notification activation to open the right account and room and dismissal when a room is read. Add a public managed notification abstraction with opaque application correlation IDs, activation, replacement, dismissal, capabilities, permissions and cancellation/shutdown. Investigate and implement modern macOS UserNotifications with checks before touching a center in an unbundled process. Preserve Show/ShowOpts defaults. Keep message content and credentials out of routing payloads and logs; composition/privacy policy stays in the consumer. Inline replies are outside scope.

## Acceptance criteria

- [ ] A managed notification returns a correlation handle and supports activation, replacement and dismissal where available, with explicit capability/errors elsewhere.
- [ ] Permission status/request and shutdown/cancellation are explicit; modern macOS notification access checks app bundle identity before native calls.
- [ ] Routing carries only opaque IDs, with lifecycle/concurrency and privacy behavior documented and exercised by meaningful tests and a public-API example.
- [ ] CGO_ENABLED=0 checks and all prescribed cross builds pass; native runtime evidence and unavailable platform validation are recorded separately.

## Implementation plan

### Public API and lifetime

Add an opt-in notify.Client constructed with a lifetime context and Config containing the display name and an activation callback. Client.Post returns a Handle with an opaque application correlation ID, Replace and Dismiss, and completion/error state. Capabilities distinguish activation, replacement, dismissal and permission prompting. Permission and RequestPermission are explicit, never requested merely by posting. Close and context cancellation stop activation delivery, dismiss tracked notifications best-effort and release backend resources. Callbacks run serially off native UI threads; callers may safely focus a tuohi View from them. Existing Show, ShowOpts, Alert and Beep retain their defaults and platform paths.

The application ID remains only in Go. OS correlation uses independent random identifiers (macOS) or daemon-assigned IDs (D-Bus). Neither errors nor diagnostic logs contain notification title/body or routing IDs. Message composition/privacy and account/room lookup remain in the consumer. Replacement retains application identity; a duplicate live application ID is an error. Cancellation stops waiting but cannot promise to undo an OS operation already accepted; Close reports cleanup errors.

### Backends

Linux uses a dedicated session-bus connection with signal matches installed before posting, GetCapabilities, Notify with the default action and replaces_id, and CloseNotification. Bind to the unique daemon owner so a daemon restart cannot alias an old ID to another client's notification. Service loss closes outstanding handles and requires a new client. Permission is explicitly not required by this protocol; there is no permission dialog API. Test against a private D-Bus notification service, including activation, replacement, dismissal, service loss and spoofed/unknown signal IDs.

macOS uses UNUserNotificationCenter with Objective-C blocks through purego, a retained delegate and random request identifiers. Before asking for a center, verify NSBundle identifies an APPL .app with an identifier and the executable inside it; unbundled clients return ErrUnavailable. Check for an existing center delegate rather than stealing it. Query and request authorization explicitly, handle add completion/cancellation, default activation and foreground presentation, and remove pending/delivered requests on dismiss/close. Managed icon attachments may report ErrUnsupported initially rather than move a caller's image file; existing icon-capable helpers are unchanged. A headless native macOS test checks that an unbundled test process fails without crashing. Native bundled-app delivery/permission validation is unavailable on this Linux host and must be recorded, not inferred from compilation.

Windows' existing shared Shell_NotifyIcon balloon has no durable independent notification identity and no managed message-pump ownership. The managed constructor returns ErrUnsupported instead of inventing unreliable correlation/replacement or silently using a read-only fallback. Existing helpers remain supported. Other unsupported targets return the same explicit sentinel.

### Validation and alternatives

Use shared fake-backend tests for state, concurrency, cancellation, duplicate IDs, replacement and shutdown; private D-Bus tests for real wire/signals; a public-API managed-notification example; docs with exact capability and bundle requirements. Run CGO_ENABLED=0 just ci and both Linux GUI stacks. An exported global callback was rejected because notifications are scoped and consumers need deterministic cleanup. A best-effort CLI fallback was rejected for managed clients because it cannot promise activation/identity/dismissal. Replacing the legacy Show path was rejected to preserve existing consumers and defaults.
