---
schema: 4
id: TKT-01M3HWWRXMN56AG2GNC3M92GWZ
title: Decide tuohi's support tiers and make CI match them
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/t3code-72958710
  branch: ci/tiers-and-gui
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-ci
  commit: eb946e72a7dad4a7992c859249d3cb6efb04d8f6
  session: null
  claimed_at: 2026-09-29T21:41:52Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-29T21:55:04Z
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

- [x] Support tiers are documented in the README
- [x] Each question in this ticket has a recorded decision
- [ ] Both workflows match the decisions

## Implementation plan

Apply the decisions recorded on 2026-09-30 in one pull request, together with TKT-01M3K439622PRJ1KHC91X4GCMQ, which edits the same Linux job.

1. **GitHub.**
   - Set workflow-level `CGO_ENABLED=0`, and drop `go test -race`.
   - Set `TUOHI_REQUIRE_GUI=1` and `-v` on the Linux Xvfb step, the macOS tests, and the Windows tests. Make `requireGUI` on macOS and Windows honour the variable.
   - Remove the artifact upload and download, so a tag's release carries notes and no binaries.
2. **Forgejo.**
   - Add a `gui` job in `golang:1.27-trixie`, through the pull-through path, running the steps of `just test-gui`.
   - Drop terva-review's `pure/**` exclusion.
3. **Go minimum.** Set `go.mod` to `go 1.26.0` and make the code build on 1.26.
4. **Docs.**
   - README: support tiers, the Go minimum, and glibc.
   - architecture.md: the settled decisions.
   - AGENTS.md: the `gui` job, and the no-PR-branches-on-the-mirror rule.

## Notes

**agent:claude-code/d3685535** at 2026-09-27T17:02:39Z

Forgejo CI runs in golang:1.27-alpine and needs gcompat: every binary that reaches pure/ requests glibc's loader (/lib64/ld-linux-x86-64.so.2) even with CGO_ENABLED=0. That was found when the first CI run failed on 2026-09-27, and reproduced in an Alpine 3.24 minirootfs under bubblewrap. With gcompat, the tuohi, dialog, notify, and tray tests pass there. The same fact means a consumer's binary runs only on glibc desktops. The support-tier decision should say so, and decide whether a glibc CI image is better than gcompat.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

**Go minimum: 1.26.** The owner chose it on 2026-09-27. The source needs about Go 1.23 (`reflect.TypeFor`, range over int, `structs.HostLayout`). The real floor is `golang.org/x/sys` v0.48.0, which declares 1.26.0. The inherited `go 1.27` is not needed. git-ticket-canvas moves to 1.26 when it adopts tuohi.

Rejected: pinning an old `x/sys` to keep 1.25. It trades a one-time move by one consumer for holding a security-relevant dependency back indefinitely.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:36:38Z

Tier decision from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape). Tier 1 is Linux (WebKitGTK 4.1 and 6.0, amd64 and arm64), macOS, and Windows: built, and tested on the real engine on every change to main. Tier 2 is FreeBSD and NetBSD: they must cross-build, and nothing runs them. This keeps appkit's claim, which matches what CI does now that the Linux GUI scenarios run. Rejected: dropping the BSDs, because cross-building them costs one CI step and the owner asked for functionality kept; and promoting them, because no runner exists. This ticket still owns the CI questions: testing macOS and Windows before a merge rather than after, the inherited GitHub workflow, action pinning, and GUI scenarios on Forgejo.

**agent:claude-code/t3code-92c88910** at 2026-09-27T20:10:52Z

Owner decision, 2026-09-27: macOS and Windows keep being tested after merge only. Pull-request branches are not pushed to the GitHub mirror, so work in progress stays off the public mirror. The cost is accepted: a macOS or Windows regression turns GitHub main red until a fix lands on Forgejo. To keep that window short, new native code for macOS and Windows lands in small pull requests, one engine per pull request where possible. The rejected option was pushing each branch to GitHub and merging only after its macOS and Windows jobs pass.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:13:06Z

### Owner decisions, 2026-09-30, on the remaining CI questions

- **Test without cgo: yes.** GitHub's Linux jobs drop `go test -race`, which builds with cgo, and test with `CGO_ENABLED=0`, the way consumers build. The race detector is lost on those jobs. This is accepted because the path cgo exercises is not the one that ships.
- **Stop publishing binaries: yes.** A `v*` tag no longer attaches demo binaries to a GitHub release. A library publishes source. The demo matrix still builds on every run, so a demo that stops compiling is still caught.
- **GUI scenarios on Forgejo: yes.** Add a Debian job to `.forgejo/workflows/ci.yml` that runs the Linux GUI scenarios under Xvfb and D-Bus on both WebKitGTK stacks, so they gate a pull request before it merges. The image is `container.local.sothr.com/docker/library/golang:*-trixie`, from the registry's Docker Hub pull-through path. terva-sh/tuwunel's CI uses that path for its Debian image, and installs `nodejs` for the checkout action.
- **Pin actions by SHA: not chosen.** Actions stay pinned by tag.
- **Pre-merge macOS and Windows testing: stays post-merge only.** The owner reaffirmed the 2026-09-27 decision. Pull-request branches are not pushed to the GitHub mirror. Earlier in this session an agent had pushed some branches there for CI, against that decision. That stops.
- **glibc image versus gcompat for the Alpine job: keep gcompat.** This is the agent's call. The Alpine job tests only the headless paths. The new Debian job gives the glibc and WebKitGTK coverage. Switching the main job's image would change what it runs for no gain.

TKT-01M3K439622PRJ1KHC91X4GCMQ (Fail GitHub CI when the Linux GUI scenarios do not run) lands with this ticket's workflow changes, since both edit the same Linux job.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:41:53Z

### Go 1.26 needed a code change

go.mod said 1.27, and the code had come to need 1.27 in one place. Each engine's constructor set `bindings` and `serve` in the `webview` literal, and those fields are promoted from the embedded `viewCore`. Only Go 1.27 accepts promoted fields in a composite literal. `GOTOOLCHAIN=go1.26.0` found it:

- `go vet` failed on linux (`lib_unix.go:1959`), darwin (`lib_darwin.go:853`), and windows (`lib_windows.go:2427`);
- `go test -c` failed on four Windows test literals.

The constructors now assign the fields, and the Windows tests name `viewCore`. With `go 1.26.0` in go.mod, even the Go 1.27 toolchain applies 1.26 language rules, so the floor holds everywhere. GitHub's jobs take their Go from go.mod, so they now run on 1.26. golangci-lint v2.13.1 declares `go 1.26.0`, so its install on 1.26 works.

### The Forgejo gui job was run before it was pushed

The job's install and test script ran under podman in `container.local.sothr.com/docker/library/golang:1.27-trixie`, as root in the container, with bubblewrap installed. Both stacks passed: webkitgtk-6.0 in 27.4s, webkit2gtk-4.1 in 34.9s. The image pulls anonymously through the pull-through path.

Forgejo's Docker runner may confine user namespaces differently from rootless podman. If WebKit's bubblewrap sandbox fails there, the first run will show it.

### Checked locally

- `just ci` and `just test-gui` pass on both stacks.
- golangci-lint reports 0 issues for linux, darwin, windows, and netbsd.
- actionlint passes on `.github/workflows/ci.yml`. For the Forgejo files it reports only the unknown `docker` runner label.
- Go 1.26: vet passes for linux, darwin, and windows; `go test -c` builds for darwin and windows; all eight `just cross` targets build.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:52:24Z

### terva-review disposition, PR #37, first round (c2cbe5f)

**Declined: "Disable cgo in the Forgejo GUI job" (medium).** The finding says that job environments are separate, so the check job's `CGO_ENABLED=0` does not reach the gui job. That setting is not the check job's. It sits in the workflow-level `env:` block of `.forgejo/workflows/ci.yml`, above `jobs:`, and Actions applies a workflow-level env to every job. The gui job therefore runs with `CGO_ENABLED=0` already. Repeating it on the job would only answer a misreading.

The gui job passed on the real runner in 2m38s, on both stacks.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:55:04Z

### terva-review disposition, PR #37, second round (fc4bab4)

The first-round finding shows as declined.

**Declined: "Remove old assets when rerunning a release" (medium).** The finding: rerunning the release job for a tag released under the old workflow leaves that release's demo binaries attached. That is true, and it is deliberate. The only such release is v0.1.0-alpha.1, a GitHub pre-release with 19 assets.

Deleting assets from a published release is an outward-facing change, and a CI rerun should not make it silently. The no-binaries decision governs releases cut from now on. Whether alpha.1 keeps its binaries is left for the owner to decide and do by hand, for example with `gh release delete-asset`.
