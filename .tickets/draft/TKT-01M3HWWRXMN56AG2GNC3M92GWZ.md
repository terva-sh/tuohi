---
schema: 4
id: TKT-01M3HWWRXMN56AG2GNC3M92GWZ
title: Decide tuohi's support tiers and make CI match them
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - policy
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-27T19:36:38Z
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

Decide tuohi's support tiers and make CI match them.

### Questions

- **Tiers.** Which platforms are tier 1 (built, and tested on real engines), and which are compile-only? appkit claimed Linux, macOS, and Windows as tier 1 and FreeBSD and NetBSD as tier 2.
- **Where macOS and Windows are tested.** Only on GitHub's hosted runners, through `.github/workflows/ci.yml`, after `just sync-github`. So a macOS or Windows regression is found after a merge, not before it. Decide whether that is acceptable, or whether pull requests should be pushed to GitHub for testing before they merge.
- **Is the GitHub workflow right for us?** It was inherited from appkit:
  - it lints with golangci-lint v2.13.1 across three GOOS values;
  - it runs `go test -race`, which builds with cgo and so exercises a different path from what ships;
  - it builds 19 demo targets;
  - on a `v*` tag it publishes demo binaries to a GitHub release. Decide whether a library should publish binaries at all.
- **Pin actions by SHA.** They are pinned by tag today.
- **The Go minimum.** `go.mod` says 1.27, while git-ticket-canvas is at 1.25. Decide whether tuohi can require 1.25 or consumers move to 1.27.
- **GUI scenarios on Forgejo CI.** They cannot run in the Alpine container. Decide whether a Debian-based job with Xvfb and WebKitGTK should run them.

## Acceptance criteria

- [ ] Support tiers are documented in the README
- [ ] Each question in this ticket has a recorded decision
- [ ] Both workflows match the decisions

## Notes

**agent:claude-code/d3685535** at 2026-09-27T17:02:39Z

Forgejo CI runs in golang:1.27-alpine and needs gcompat: every binary that reaches pure/ requests glibc's loader (/lib64/ld-linux-x86-64.so.2) even with CGO_ENABLED=0. That was found when the first CI run failed on 2026-09-27, and reproduced in an Alpine 3.24 minirootfs under bubblewrap. With gcompat, the tuohi, dialog, notify, and tray tests pass there. The same fact means a consumer's binary runs only on glibc desktops. The support-tier decision should say so, and decide whether a glibc CI image is better than gcompat.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

**Go minimum: 1.26.** The owner chose it on 2026-09-27. The source needs about Go 1.23 (`reflect.TypeFor`, range over int, `structs.HostLayout`). The real floor is `golang.org/x/sys` v0.48.0, which declares 1.26.0. The inherited `go 1.27` is not needed. git-ticket-canvas moves to 1.26 when it adopts tuohi.

Rejected: pinning an old `x/sys` to keep 1.25. It trades a one-time move by one consumer for holding a security-relevant dependency back indefinitely.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:36:38Z

Tier decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape). Tier 1 is Linux (WebKitGTK 4.1 and 6.0, amd64 and arm64), macOS, and Windows: built, and tested on the real engine on every change to main. Tier 2 is FreeBSD and NetBSD: they must cross-build, and nothing runs them. This keeps appkit's claim, which matches what CI does now that the Linux GUI scenarios run. Rejected: dropping the BSDs, because cross-building them costs one CI step and the owner asked for functionality kept; and promoting them, because no runner exists. This ticket still owns the CI questions: testing macOS and Windows before a merge rather than after, the inherited GitHub workflow, action pinning, and GUI scenarios on Forgejo.
