---
schema: 4
id: TKT-01M3R1V2JHJ3GZ95D9FW62QCPE
title: Move the demos into examples/ as tuohi's reference implementation
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-30T02:21:02Z
updated_at: 2026-09-30T02:51:41Z
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

- [x] Every former demo builds from examples/ on every target GitHub's build matrix covers
- [x] examples/loopback serves its page over net/http and reaches a binding from it
- [x] The showcase self test and the loopback program run in Forgejo's GUI job on both WebKitGTK stacks, and in GitHub's Linux, macOS and Windows jobs
- [x] No example imports a tuohi internal package
- [x] make clean removes only build outputs

## Implementation plan

### Layout

- `examples/` is a module, `github.com/terva-sh/tuohi/examples`, requiring tuohi through `replace github.com/terva-sh/tuohi => ../`.
- `examples/showcase` (was `demo/`), `examples/tray`, `examples/notify` and `examples/dialog` (were `<pkg>/demo`), and a new `examples/loopback`.
- `examples/internal/run` lets a test run its example as a program. The example's TestMain hands itself to `run.Main`. `run.Program` re-executes the test binary with the example's arguments, and the child calls `main` on the main goroutine, where macOS needs it. The child gets its own XDG config and data directories. `run.Program` skips unless `TUOHI_REQUIRE_GUI=1`.

### The contract checks

- Go's internal rule goes by path, and `examples/` sits under tuohi's, so an example could import `tuohi/internal/...` and compile; probed and confirmed. `make check-examples` lists every example's imports, tests included, and fails on one. It also runs `go mod tidy -diff` in `examples/`.
- `just` recipes (vet, test, test-gui, cross, ci) loop over `. examples`. Forgejo's check job adds the examples to vet, test and cross, plus a check-examples step, and its gui job runs `examples/` on both stacks. GitHub's lint job lints and tests `examples/` and runs check-examples. Its unix job runs an "Examples under Xvfb" step under dbus-run-session, macOS and Windows gain an Examples step, and the build matrix builds `examples/...` per target.

### Makefile

`clean` drops `rm -rf ./demo` (TKT-01M3QQS4). `demo` becomes `examples`, building into `./build`. `xdemo` is removed: it built release binaries, which releases no longer carry, and the GitHub build matrix already builds every target. `tidy` tidies both modules.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-30T02:21:08Z

The owner asked for this on 2026-09-29: move the demo into an examples directory, as an in-tree reference implementation that shows usage and holds tuohi to its contract. That request is the promotion. TKT-01M3QQS4 (Stop make clean from deleting the demo source directory) is folded in, because the move rewrites the same Makefile targets.

**agent:claude-code/t3code-72958710** at 2026-09-30T02:34:11Z

### What running the showcase found

The showcase's self test had stopped working on main, and nothing ran it. `./demo --selftest` from origin/main 181f715 timed out at "page never became ready" on both WebKitGTK stacks, while the same binary with `-http` passed 23/23. The page was navigated to `app://#selftest`. `originOf("app://")` is `app:`, since the URL has no host, and the bridge was withheld from the page the engine actually showed.

The showcase now navigates to `app://app/index.html`, the form the App.FS documentation gives, and passes 23/23 on both stacks. The library side, whether a bare `app://` should be normalized or refused, is filed as TKT-01M3R23M9 (A bare app:// URL loads its page without the bridge) and left in draft.

### Other changes to the showcase

- `pairValue` and `maximized` were written from binding goroutines unguarded, and now have mutexes.
- `w` was assigned after `App.Show` returned, though the page can call a binding that uses it as soon as Show navigates. It is now assigned first.
- The user-visible names changed: the app is "tuohi showcase" and the autostart id is `tuohi-showcase`. The page's `window.demo*` bindings and `demo:` events keep their names, because renaming them would churn 500 lines of page script for no change in what is exercised.

### Verified locally

- `just ci` passes, now covering `examples/`.
- `just test-gui` passes on webkitgtk-6.0 and webkit2gtk-4.1: the library suite, loopback -check, and showcase 23/23.
- golangci-lint v2.13.1 reports 0 issues in `examples/` for linux, darwin and windows.
- `examples/...` builds for all 19 targets in GitHub's matrix (s390x with Go 1.27.1).
- `make check-examples` fails on a probe example importing `tuohi/internal/toolkit`.
- `make examples` then `make clean` leaves every tracked file in place.

macOS and Windows run the examples only after the merge, on GitHub.

**agent:claude-code/t3code-72958710** at 2026-09-30T02:40:15Z

Dispositions for the Terva review of 894e21de (review 1534):

**high, justfile loops hide a module's failure: no change.** The justfile sets `set shell := ["bash", "-eu", "-o", "pipefail", "-c"]`, and under `-e` a failing `( ... )` in a `for` body ends the recipe; it is not part of an `&&` list in the outer shell. Reproduced with the same shell and a two-module loop whose first module fails: just reported `recipe 'vet' failed ... exit code 1`, and the second module never ran. `cross` had the same pattern before this change and relies on the same thing. `test-gui` accumulates deliberately, so both stacks run, and exits 1 when any fails.

**medium, check-examples masks failures: fixed in the new head.** Make runs a recipe with `sh -c` and no `-e`, so the success line followed a failed `go mod tidy -diff`, and a failed `go list` left nothing for grep to find. Reproduced: a recipe of `cd /nonexistent && true; echo ok` printed ok and exited 0. `go list` now runs on its own with `|| exit 1`, and `go mod tidy -diff` fails the recipe with a message. `check-imports` had the same `go list` masking and gets the same guard. Checked: passes on the tree; fails on an untidy go.mod, on an example whose import cannot resolve, and on one importing `tuohi/internal/toolkit`.

**agent:claude-code/t3code-72958710** at 2026-09-30T02:51:40Z

Merged in PR #54 as 71e3bef, synced to GitHub. GitHub run 36661497421 on that commit passed every job, and each GUI job ran the examples as programs. Showcase self test 23/23 and loopback -check ok=true on macOS, Windows, and ubuntu-latest and ubuntu-24.04-arm on both GTK stacks. Forgejo's gui job ran them on both WebKitGTK stacks for the PR, under TUOHI_REQUIRE_GUI=1, where run.Program has no path that skips.

## Summary

The demos are now examples/, a module of its own that uses tuohi only through its public API, as consumers do. It holds showcase, the former demo, plus tray, notify, dialog, and the new loopback, which is terva's net/http shape. CI builds them for all 19 targets and runs the showcase self test and the loopback program in every GUI job: Forgejo on both WebKitGTK stacks, and GitHub on Linux (x86 and ARM, GTK3 and GTK4), macOS and Windows. make check-examples refuses an import of a tuohi internal package, which Go allows by path, and an untidy examples/go.mod. Running the showcase found that its self test had been broken on main, because a bare app:// page loses its bridge. The showcase now uses the documented URL, and the library side is draft TKT-01M3R23M9 (A bare app:// URL loads its page without the bridge). Landed in PR #54.
