---
schema: 4
id: TKT-01M5JR0EA923DT7G38S1XHT6KQ
title: Install portable updates and restart after graceful shutdown
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/api
  - area/release
  - security
assignees: []
milestone: null
parent: TKT-01M5JR0E6ZM7D41HHX2QN1ZK7B
origin: null
dependencies:
  - TKT-01M5JR0E89AAJXVDF2GNK33MTQ
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/self-update
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 3f9fb9f102ae97c8af50849eba4e182f3205ae2d
  session: null
  claimed_at: 2026-10-22T21:28:57Z
  expires_at: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T21:28:57Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

Provide an explicit installation plan for consumer-owned standalone executables and an early-main helper entry point. Never infer package ownership or elevate permissions. Validate target identity/type and replacement integrity, serialize helper lifetime, wait for confirmed parent termination, install using same-directory moves with rollback on failure, and relaunch with explicit arguments/environment policy. Windows must wait on a native process handle instead of a PID poll; Unix uses a stable lifetime handshake. All waits have explicit cancellation/timeouts. Keep existing App defaults and UI thread rules; consumer calls Quit, returns from Wait, releases instance/services and exits normally. Do not start a second overlapping instance before old process termination.

## Acceptance criteria

- [ ] A real subprocess regression performs a portable install/restart only after parent termination and verifies application arguments.
- [ ] Cancellation, stale target, replacement failure, rollback and integrity-change regressions preserve the existing installation.
- [ ] Windows helper/process support compiles cgo-free; platform guarantees and unsupported managed installs are explicit.

## Implementation plan

Implement private helper copy and authenticated lifetime handshake with stable native process termination wait. Verify staged/installed digests and target snapshot. Use same-directory candidate/backup and rollback. Test via real subprocess and failure injection; compile Windows/Darwin.
