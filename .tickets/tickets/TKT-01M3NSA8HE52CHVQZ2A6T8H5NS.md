---
schema: 4
id: TKT-01M3NSA8HE52CHVQZ2A6T8H5NS
title: Honour TUOHI_DEBUG only when the program opts in
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - security
  - area/api
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-6bca1629
  branch: feat/debug-opt-in
  worktree: /home/sothr/.t3/worktrees/tuohi/t3code-6bca1629
  commit: 5ef1617383a4a83695f7671d19e030ce25a13a5a
  session: null
  claimed_at: 2026-10-01T05:34:07Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:13:34Z
updated_at: 2026-10-01T05:34:07Z
created_by:
  id: agent:claude-code/t3code-72958710
  name: ""
updated_by:
  id: agent:claude-code/t3code-6bca1629
  name: ""
extensions: {}
---

## Description

### What

Make the `TUOHI_DEBUG=1` environment variable turn a view's dev tools on only when the program opts in. The variable was called `APPKIT_DEBUG` until TKT-01M3HWWRWX (Finish renaming appkit to tuohi).

### Current behaviour

`App.Debug` ORs in `TUOHI_DEBUG=1` (`envDebug` in `app.go`), and `View.Debug` ORs in `App.Debug`. So whoever launches the program can open the web inspector on its pages, even in a release build, and nothing the program sets can prevent it.

### Direction

Decided 2026-10-01; see the note. The architecture review leaned against the launcher's override. `View.Debug` is the application's decision, and an environment variable belongs to whoever launched the program. The options were:
- drop the variable;
- honour it only when the program opts in, through an `App` field (chosen);
- keep it and document it.

Split out of TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the app allows them), whose acceptance criteria do not cover it.

## Acceptance criteria

- [x] TUOHI_DEBUG=1 turns the dev tools on only when an App field opts in, and the field is off by default
- [x] The field and the variable are documented, and the examples use the field where they relied on the variable
- [x] A test covers the variable with and without the opt-in

## Implementation plan

Add App.AllowEnvDebug (bool, off by default). snapshotConfig commits Debug as App.Debug || (AllowEnvDebug && TUOHI_DEBUG=1). Update the App.Debug, View.Debug, README and architecture docs. A table test on snapshotConfig covers the variable with and without the opt-in. The name AllowEnvDebug was chosen over EnvDebug and DebugEnv because it reads as a permission the program grants, which is what the field is.

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: honour TUOHI_DEBUG only when the program opts in

The owner decided, 2026-10-01. The variable was renamed from APPKIT_DEBUG by TKT-01M3HWWRWX (Finish renaming appkit to tuohi), and `envDebug` in app.go now reads `TUOHI_DEBUG`. The title is updated to match.

Add an `App` field (its name is settled in the plan, for example `AllowEnvDebug`). TUOHI_DEBUG=1 turns the dev tools on only when the field is set, and it is off by default. `View.Debug` and `App.Debug` stay the application's decision, as the architecture review leaned.

The alternatives lost for these reasons. Dropping the variable removes a convenience a consumer would otherwise rebuild for itself. Keeping it as it is leaves the launcher able to open the inspector in a release build with nothing the program can do about it.

It blocks v0.1.0, because changing the public API is cheaper before the first tag. The examples are updated in the same PR, per AGENTS.md.

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:34:07Z

Implemented on feat/debug-opt-in. No example set or documented TUOHI_DEBUG: the showcase sets View.Debug itself, and the other examples never open the inspector. So criterion 2's 'the examples use the field where they relied on the variable' needs no example change, and the field is documented on App, in the README, and in docs/architecture.md. A consumer that relied on TUOHI_DEBUG must now set App.AllowEnvDebug. The README states this.
