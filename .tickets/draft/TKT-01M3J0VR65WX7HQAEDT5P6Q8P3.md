---
schema: 4
id: TKT-01M3J0VR65WX7HQAEDT5P6Q8P3
title: just sync-github ignores --yes and never pushes
type: bug
status: draft
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
updated_at: 2026-09-27T18:08:29Z
created_by:
  id: agent:claude-code/t3code-92c88910
  name: ""
updated_by:
  id: agent:claude-code/t3code-92c88910
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

- [ ] just sync-github --yes fast-forwards the forge that is behind
- [ ] just sync-github with an unknown flag exits 2
