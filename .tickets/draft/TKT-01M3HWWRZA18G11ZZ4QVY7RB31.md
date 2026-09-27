---
schema: 4
id: TKT-01M3HWWRZA18G11ZZ4QVY7RB31
title: Release tuohi v0.1.0
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/release
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRRXTAR4T01SK79Z4BSM
  - TKT-01M3HWWRT7X1RZZYY6KFEP0ERE
  - TKT-01M3HWWRTVWVYSEDPRKSDPE783
  - TKT-01M3HWWRVGMZTXBYJ86FCTH0V8
  - TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
  - TKT-01M3HWWRWX3D9XTA5RTW5AC26R
  - TKT-01M3HWWRXMN56AG2GNC3M92GWZ
  - TKT-01M3HWWRYD7GNZEZA2JCGWGDJS
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-27T16:59:09Z
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

Tag and publish tuohi v0.1.0 as the first terva-sh release, so git-ticket-canvas and terva can depend on a version rather than a commit.

### Before tagging

- **Security.** The bridge trust boundary, permission policy, and single-instance hardening are done.
- **Tests.** The GUI scenarios run and pass on Linux, and GitHub CI is green on macOS and Windows.
- **Attribution.** NOTICE is current.
- **Release process.** Follow the organisation's, as git-ticket does: land on Forgejo `main`, run `just sync-github --yes`, wait for GitHub CI, then push the annotated tag to both forges separately, not with `--follow-tags`. Confirm proxy.golang.org serves the version.

## Acceptance criteria

- [ ] v0.1.0 is tagged on Forgejo and GitHub, and proxy.golang.org serves it
- [ ] git-ticket-canvas and terva are told the version exists, through their tickets
