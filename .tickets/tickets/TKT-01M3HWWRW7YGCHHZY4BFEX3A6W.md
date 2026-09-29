---
schema: 4
id: TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
title: Replace pure/ with upstream ebitengine/purego
type: task
status: ready
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
updated_at: 2026-09-29T21:13:07Z
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

- [ ] tuohi depends on github.com/ebitengine/purego and pure/ is removed
- [ ] Every target in just cross still builds, and the GUI scenarios pass
- [ ] NOTICE and docs/provenance.md reflect the change

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
