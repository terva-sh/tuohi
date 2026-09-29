---
schema: 4
id: TKT-01M3J0VR65WX7HQAEDT5P6Q8P3
title: just sync-github ignores --yes and never pushes
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T18:08:29Z
updated_at: 2026-09-29T05:17:36Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-72958710
  name: ""
extensions: {}
---

## Description

### What

`just sync-github --yes` never pushes. The recipe is a shebang script that reads its flags from `"$@"`, but the justfile does not `set positional-arguments`, so just passes the script no arguments. `--yes` is dropped and every run is a dry run that prints "dry run; pass --yes to push". The other half of the recipe is broken the same way: `just sync-github --bogus` does not reject the unknown flag.

### Evidence (2026-09-27)

After merging Forgejo PR #2, `just sync-github --yes` printed the fast-forward and "dry run; pass --yes to push". GitHub `main` was fast-forwarded by hand from 3e4df6b to 3adf498 with the push the recipe would have run, `git push github <origin/main sha>:refs/heads/main`, after checking with `git merge-base --is-ancestor github/main origin/main` that it was a fast-forward. `just sync-github` then printed "in sync at 3adf4984da43".

### Fix

Pass the variadic parameter into the script explicitly, for example `set -- {{flags}}`, or enable `positional-arguments` for this recipe with `[positional-arguments]`. Check that `--yes` pushes and that an unknown flag exits 2.

## Acceptance criteria

- [x] just sync-github --yes fast-forwards the forge that is behind
- [x] just sync-github with an unknown flag exits 2

## Implementation plan

Add just's [positional-arguments] attribute to the sync-github recipe, so the shebang script's "$@" receives the flags. The alternative, set -- {{flags}}, lost because just would splice the flags into the script text unquoted. Verify in a scratch clone whose origin and github remotes are local bare repos, so neither real forge is touched.

## Notes

**agent:claude-code/t3code-72958710** at 2026-09-29T05:17:36Z

### Verified in a sandbox (just 1.58.0)

The test used a scratch clone whose `origin` and `github` remotes were local bare repositories, with `origin` one commit ahead. It ran against the justfile from `origin/main` (old) and from this branch (fixed).

**Old recipe:**
- `--bogus`: exits 0, dry run.
- `--yes`: exits 0, prints "dry run; pass --yes to push", and GitHub stays at base.

**Fixed recipe:**
- `--bogus`: "sync-github: unknown flag --bogus", exit 2.
- No flags: dry run, exit 0.
- `--yes`: pushes `fad9698..5cad819` to github main, exit 0. A second `--yes` reports "in sync at 5cad8190194b".
- GitHub ahead, `--yes`: fast-forwards `origin` main, exit 0.
- Diverged, `--yes`: "diverged: ...", exit 1, and neither side moves.

## Summary

The sync-github recipe now has just's [positional-arguments] attribute, so --yes pushes and an unknown flag exits 2. Verified against local bare remotes in both directions, and with diverged branches, which it still refuses. Once this merges, just sync-github --yes replaces the hand-run git push github origin/main:refs/heads/main.
