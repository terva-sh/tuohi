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
  - TKT-01M3J59M0VJYQ3E652Y0FD90H3
  - TKT-01M3J59M32VGK83J7JKYPKSHJE
  - TKT-01M3J59M3SQBSXV2KVZEFBNBAV
  - TKT-01M3J59M4EJRPMKHWBS7K4XD3S
  - TKT-01M3J59M5V12QW1WRBEJPJ5H38
  - TKT-01M3J1H8CPMZX9EJX8R2CQRA6P
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
blocks_on: none
references:
  - ref: ticket:git-ticket-canvas/TKT-01M3HHJQR9Q3ZSSCJ8HG17EM8J
    path: null
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-30T02:18:43Z
created_by:
  id: agent:claude-code/d3685535
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
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

## Notes

**agent:claude-code/bridge-trust-3** at 2026-09-28T19:08:54Z

The owner asked, 2026-09-28, for a pre-release so another agent can start integrating tuohi before v0.1.0: v0.1.0-alpha.1, cut from main after the bridge trust work, TKT-01M3HWWRT7X1RZZYY6KFEP0ERE, landed. This ticket's v0.1.0 gate is unchanged. The permission policy (TKT-01M3HWWRTV), single-instance hardening (TKT-01M3HWWRVG), and reply-side trust (TKT-01M3JYQWZB) remain open. The release notes name those, the Linux goroutine-safety bug (TKT-01M3J1H8CP), the unguarded App.FS server (TKT-01M3J59M4E), and the coming API changes as limits of the alpha. The GitHub release job now publishes a tag with a pre-release part as a GitHub pre-release.

**agent:claude-code/t3code-72958710** at 2026-09-30T02:18:43Z

The owner asked, 2026-09-29, for the binaries on the GitHub pre-release v0.1.0-alpha.1 to be deleted, following the decision that releases publish notes only. They were the 19 tuohi-demo_<os>_<arch> builds of ./demo, attached by the release job before that decision. The release, its tag, and its notes stay. Forgejo carries no release for the tag, so nothing was deleted there. The tag's source is what integrators consume, through the module proxy.
