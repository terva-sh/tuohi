---
schema: 4
id: TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
title: Replace pure/ with upstream ebitengine/purego
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ffi
  - area/provenance
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
updated_at: 2026-09-29T21:28:47Z
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

Replace `pure/`, a modified copy of purego v0.11.0 (7,472 lines), with a dependency on upstream `github.com/ebitengine/purego`.

### Why

- **Upstream fixes.** The copy misses upstream v0.11.1's errno clearing in every `sys_*.s` and its 386 64-bit callback results.
- **Licence obligations.** It carries Apache-2.0 obligations that are only partly met: section 4(b) change notices are missing, and `pure/unsupported.go` has the wrong copyright line.
- **Maintenance.** Upstream is maintained by the Ebitengine project, which is the most sustainable home for per-architecture assembly.
- **It is feasible.** The engine code uses only purego's standard entry points: `RegisterLibFunc`, `RegisterFunc`, `Dlsym`, `Dlopen`, and `NewCallback`.

### What to check

- **The FreeBSD `cgo_export_dynamic` change.** appkit dropped these directives, and says FreeBSD builds are compile-only as a result. Decide whether upstream's behaviour is acceptable for a compile-only tier.
- **Removed platforms.** Android and iOS support was removed from the copy. Upstream keeping them costs nothing.
- **Linker symbols.** They were renamed so both could link into one binary. Once `pure` is gone that no longer matters.
- **NOTICE.** Update it: the purego entry becomes an ordinary dependency, and `pure/LICENSE-GO` goes with the directory.

## Acceptance criteria

- [x] tuohi depends on github.com/ebitengine/purego and pure/ is removed
- [x] Every target in just cross still builds, and the GUI scenarios pass
- [x] NOTICE and docs/provenance.md reflect the change

## Implementation plan

1. Rewrite the imports `github.com/terva-sh/tuohi/pure` and `.../pure/objc` to `github.com/ebitengine/purego` and `.../purego/objc`, and the `pure.` qualifier to `purego.`. Only `pure/` itself imports the internal packages, so nothing else changes.
2. Delete `pure/`, then run `go get github.com/ebitengine/purego@v0.11.1` and `go mod tidy`.
3. Give FreeBSD the fakecgo `-std` flag in `just cross`, the Makefile (`cross-bsd`, `lint-bsd` through GOFLAGS, and the demo build), the Forgejo cross step, and the GitHub demo matrix.
4. Drop the `pure/` exceptions: the Forgejo test grep, `just test-pure`, the `packages` filter, the golangci exclusion, and `.gitattributes`.
5. Update NOTICE, docs/provenance.md, docs/architecture.md, the README's platform section, and AGENTS.md.
6. Verify locally: `just ci`, `just test-gui` on both stacks, all 19 demo targets, and golangci-lint for all five GOOS values. macOS and Windows get tested on GitHub after merge.

## Notes

**agent:claude-code/d3685535** at 2026-09-27T17:02:39Z

Forgejo CI skips ./pure/... (added 2026-09-27 in the scaffolding pull request). Its tests are upstream purego's: they compile C fixtures with the toolchain's C compiler, which the Alpine image lacks, and GitHub CI runs them on Ubuntu. Removing pure/ removes the exception, so delete the grep in .forgejo/workflows/ci.yml's test step as part of this ticket.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Confirmed by TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

A diff against upstream v0.11.1 in the module cache shows the copy is v0.11.0 plus:

- the package rename and the matching symbol renames;
- Android and iOS stripped;
- panic message prefixes.

Non-`pure` code uses `RegisterLibFunc` (155), `SyscallN` (73, Windows COM), `NewCallback` (23), `RegisterFunc` (21), `Dlopen` (16), `Dlsym` (14), and `pure/objc` on darwin. Upstream `objc` has the same API, so the switch should be mechanical. Decision: replace it.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:13:06Z

### Owner decision, 2026-09-30: upstream purego, with the FreeBSD flag

Replace `pure/` with `github.com/ebitengine/purego` and delete the directory.

A measurement settled it. In a scratch module on upstream v0.11.1, a `CGO_ENABLED=0` build for `GOOS=freebsd` fails, because `internal/fakecgo/freebsd.go` uses `//go:cgo_export_dynamic`, which the compiler accepts only in cgo-generated code. With `-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std` it builds. `GOOS=netbsd` builds without the flag.

The copy builds FreeBSD without the flag only because it dropped those two directives. Its own comment, in `pure/internal/fakecgo/freebsd.go`, says the result "may fail to resolve libc's references at runtime". So the copy's one advantage was a FreeBSD binary that may not work, where upstream plus the flag gives one that does.

- `just cross`, the Makefile, and both workflows pass the flag for FreeBSD.
- The README tells FreeBSD consumers to pass it.

Rejected: keeping `pure/` and resyncing it to v0.11.1. That keeps 7,400 lines of per-architecture code in this repository for no behaviour anyone needs.

**agent:claude-code/t3code-72958710** at 2026-09-29T21:20:29Z

### Verified before the pull request, at c198cb8

- `just ci` passes.
- `just test-gui` passes on webkitgtk-6.0 and webkit2gtk-4.1.
- All 19 demo targets in the GitHub matrix build with `CGO_ENABLED=0`. FreeBSD amd64 and arm64 build with the fakecgo flag.
- golangci-lint v2.13.1 reports 0 issues for linux, darwin, windows, freebsd, and netbsd.

For FreeBSD, golangci-lint also needs the flag, because it type-checks through the build: without it, it fails with the same `cgo_export_dynamic` error. `lint-bsd` therefore passes the flag through GOFLAGS, appended to the workflow's own GOFLAGS so `-trimpath` and the linker flags survive. That was checked with GitHub's exact GOFLAGS value.

The Forgejo test-step exception is gone, as the 2026-09-27 note asked. So is `just test-pure`: with no copy in the tree, purego's own suite is upstream's to run.

macOS and Windows are not yet run on the real engines. GitHub CI covers them after the merge, following the post-merge rule.

## Summary

Merged as PR #35 (d2d532f). tuohi depends on `github.com/ebitengine/purego` v0.11.1, and `pure/` is gone.

- **The FreeBSD flag.** A FreeBSD build without cgo passes `-gcflags=github.com/ebitengine/purego/internal/fakecgo=-std`. `just cross`, the Makefile, and both workflows pass it, and the README documents it. golangci-lint needs the flag too, through GOFLAGS.
- **Verified after the merge.** GitHub run 36633160487 on main passed: the macOS and Windows engine tests, all four Linux GUI jobs, golangci-lint, and all 19 demo builds, FreeBSD amd64 and arm64 included.
- **Review.** It first overflowed its context on the deleted assembly. It ran clean once `pure/**` was excluded; the exclusion was dispatched from the branch. That `REVIEW_EXCLUDE_PATHS` line now matches nothing. TKT-01M3HWWRXMN56AG2GNC3M92GWZ's workflow changes remove it.
