---
schema: 4
id: TKT-01M3NSA8HE52CHVQZ2A6T8H5NS
title: Honour TUOHI_DEBUG only when the program opts in
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-29T05:13:34Z
updated_at: 2026-10-01T05:14:34Z
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

Decide whether the `APPKIT_DEBUG=1` environment variable may turn a view's dev tools on in a release build.

### Current behaviour

`App.Debug` ORs in `APPKIT_DEBUG=1` (`app.go`), and `View.Debug` ORs in `App.Debug`. So whoever launches the program can open the web inspector on its pages, and nothing the program sets can prevent it.

### Direction

The architecture review leaned no. `View.Debug` is the application's decision, and an environment variable belongs to whoever launched the program. Options:
- drop the variable;
- honour it only when the program opts in, for example through an `App` field;
- keep it and document it.

Split out of TKT-01M3HWWRTVWVYSEDPRKSDPE783 (Deny media and clipboard permissions unless the app allows them), whose acceptance criteria do not cover it.

## Acceptance criteria

- [ ] TUOHI_DEBUG=1 turns the dev tools on only when an App field opts in, and the field is off by default
- [ ] The field and the variable are documented, and the examples use the field where they relied on the variable
- [ ] A test covers the variable with and without the opt-in

## Notes

**agent:claude-code/t3code-6bca1629** at 2026-10-01T05:14:21Z

### Decision: honour TUOHI_DEBUG only when the program opts in

The owner decided, 2026-10-01. The variable was renamed from APPKIT_DEBUG by TKT-01M3HWWRWX (Finish renaming appkit to tuohi), and `envDebug` in app.go now reads `TUOHI_DEBUG`. The title is updated to match.

Add an `App` field (its name is settled in the plan, for example `AllowEnvDebug`). TUOHI_DEBUG=1 turns the dev tools on only when the field is set, and it is off by default. `View.Debug` and `App.Debug` stay the application's decision, as the architecture review leaned.

The alternatives lost for these reasons. Dropping the variable removes a convenience a consumer would otherwise rebuild for itself. Keeping it as it is leaves the launcher able to open the inspector in a release build with nothing the program can do about it.

It blocks v0.1.0, because changing the public API is cheaper before the first tag. The examples are updated in the same PR, per AGENTS.md.
