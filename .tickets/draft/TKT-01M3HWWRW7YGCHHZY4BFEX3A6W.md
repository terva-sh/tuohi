---
schema: 4
id: TKT-01M3HWWRW7YGCHHZY4BFEX3A6W
title: Replace pure/ with upstream ebitengine/purego
type: task
status: draft
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
updated_at: 2026-09-27T17:02:39Z
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
