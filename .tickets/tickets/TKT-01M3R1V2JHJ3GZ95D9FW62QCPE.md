---
schema: 4
id: TKT-01M3R1V2JHJ3GZ95D9FW62QCPE
title: Move the demos into examples/ as tuohi's reference implementation
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ci
  - area/api
assignees: []
milestone: v0.1.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: feat/examples
  worktree: /home/sothr/.cache/agent-scratch/tuohi/examples.0tzR/wt
  commit: b0f60312c63325b1da72b86a99fdb5e0e0fefc36
  session: null
  claimed_at: 2026-09-30T02:21:08Z
  expires_at: null
archive: null
created_at: 2026-09-30T02:21:02Z
updated_at: 2026-09-30T02:21:08Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

Move the demo applications into `examples/`, and make them the in-tree reference implementation: programs written only against tuohi's public API, the way a consumer writes them, which CI builds for every target and runs where a GUI is available. A change that breaks how a consumer uses tuohi then fails in tuohi's own CI rather than in terva.

Today the showcase lives at `demo/`, the service demos at `tray/demo`, `notify/demo` and `dialog/demo`, and nothing runs the showcase's self test: GitHub builds `./demo` and stops there. The package documentation's loopback example, a program serving its own interface over `net/http`, which is how terva uses tuohi, exists only as a comment and cannot live in the module, whose import check forbids `net/http`.

### Why examples/ is its own module

The examples get `examples/go.mod`, requiring tuohi through `replace github.com/terva-sh/tuohi => ../`.

- They see tuohi from another module, as every consumer does.
- They may import `net/http`, which the loopback reference needs, while the library's rule against it stays whole.
- The library's module zip stops carrying the demo's page and icon.

The cost is that the root's `./...` no longer reaches them, so vet, test, the cross builds and GitHub's matrix each gain an explicit step in `examples/`.

A single module with `examples/` excluded from the import check was the alternative. It keeps `./...` covering everything, but the examples would compile against tuohi from inside the module, which is not what a consumer does, and the exclusion would be a hole in a rule this repository states without exceptions.

### Scope

- `demo/` moves to `examples/showcase/`, and `tray/demo`, `notify/demo` and `dialog/demo` move to `examples/tray`, `examples/notify` and `examples/dialog`.
- `examples/loopback/` is new: the package doc's loopback program, serving an embedded page and a JSON endpoint with `net/http` and binding one function.
- The showcase and loopback programs get tests that run them as a consumer's binary would, in a child process, when `TUOHI_REQUIRE_GUI=1` promises a GUI. Otherwise they skip.
- The showcase's unguarded shared state gets a mutex, because a reference implementation should not model a data race.
- The Makefile's `clean` stops deleting source (folds in TKT-01M3QQS4, "Stop make clean from deleting the demo source directory"), and its demo targets follow the move.
- The justfile, both CI workflows, README, AGENTS.md and docs point at `examples/`.

## Acceptance criteria

- [ ] Every former demo builds from examples/ on every target GitHub's build matrix covers
- [ ] examples/loopback serves its page over net/http and reaches a binding from it
- [ ] The showcase self test and the loopback program run in Forgejo's GUI job on both WebKitGTK stacks, and in GitHub's Linux, macOS and Windows jobs
- [ ] No example imports a tuohi internal package
- [ ] make clean removes only build outputs

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-30T02:21:08Z

The owner asked for this on 2026-09-29: move the demo into an examples directory, as an in-tree reference implementation that shows usage and holds tuohi to its contract. That request is the promotion. TKT-01M3QQS4 (Stop make clean from deleting the demo source directory) is folded in, because the move rewrites the same Makefile targets.
