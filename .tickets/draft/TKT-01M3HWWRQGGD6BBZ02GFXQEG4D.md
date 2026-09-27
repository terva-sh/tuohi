---
schema: 4
id: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
title: "Make tuohi ours: review, harden, and release v0.1.0"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/api
assignees: []
milestone: v0.1.0
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:meta/TKT-01M3HS2HER41MVD76K65FVEGDY
    path: null
  - ref: handoff:2026-09-27-tuohi-bootstrap
    path: handoffs/2026-09-27-tuohi-bootstrap.md
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:08Z
updated_at: 2026-09-27T16:59:37Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
extensions: {}
---

## Description

### What

Make tuohi terva-sh's own library rather than a copy of appkit it carries: review the architecture, fix what the safety review found, and reach a first release that terva and git-ticket-canvas can embed.

### Why

The owner's direction, 2026-09-27, is to keep appkit's functionality but review the architecture on the way in. Adopting a library means maintaining it, and the source has weak points: a machine-generated feel, borrowed code, security gaps, and a test harness that never ran its GUI scenarios.

### Background

- The adoption initiative is terva-sh/meta TKT-01M3HS2HER41MVD76K65FVEGDY (Adopt tuohi, a no-cgo desktop webview library forked from appkit).
- Its safety and provenance review is TKT-01M3HS2HGRNZ7E7VF5FNKDQ0ZS in the same store, and `docs/provenance.md` summarises it.
- The consumer that motivated the fork is git-ticket-canvas: TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J (Open the loopback canvas in a native window) waits on tuohi's first release, and its `docs/desktop-shell.md` measured the alternatives.

### Order

1. **Make the tests trustworthy.** Fix the GUI availability probe, then triage the crashes it hides.
2. **The architecture review.** It decides the shape the security work lands in.
3. **Security:** the bridge trust boundary, the permission policy, and the single-instance channel.
4. **Replace `pure` with upstream `purego`,** rename appkit's leftovers, and settle CI and support tiers.
5. **Release v0.1.0.**
