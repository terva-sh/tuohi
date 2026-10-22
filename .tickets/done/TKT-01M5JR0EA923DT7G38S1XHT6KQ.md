---
schema: 4
id: TKT-01M5JR0EA923DT7G38S1XHT6KQ
title: Install portable updates and restart after graceful shutdown
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-22T21:24:24Z
updated_at: 2026-10-22T21:43:33Z
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

- [x] A real subprocess regression performs a portable install/restart only after parent termination and verifies application arguments.
- [x] Cancellation, stale target, replacement failure, rollback and integrity-change regressions preserve the existing installation.
- [x] Windows helper/process support compiles cgo-free; platform guarantees and unsupported managed installs are explicit.

## Implementation plan

Implement private helper copy and authenticated lifetime handshake with stable native process termination wait. Verify staged/installed digests and target snapshot. Use same-directory candidate/backup and rollback. Test via real subprocess and failure injection; compile Windows/Darwin.

## Notes

**agent:codex/t3-b903096c** at 2026-10-22T21:35:30Z

Implemented explicit portable/managed installation modes, early-main helper handling, same-directory prepared candidates/backups, stable native parent watchers (Linux pidfd, BSD/macOS kqueue, Windows process handle), cross-process install locks, cancellation/readiness deadlines, target/candidate digest rechecks, ordinary replacement/launch rollback, explicit argv/environment/working directory semantics and retained private result/recovery records. Real Linux subprocess tests demonstrate no replacement before parent termination, exact argument preservation and cancellation. Fault tests cover changed target/candidate, replacement failure, launch failure, failed backup restoration, serialized installs and native-watch cancellation. CGO_ENABLED=0 focused tests passed five repetitions and full just ci passed, including all eight cross targets and ticket strict check. Native macOS/Windows/BSD execution remains unavailable; compilation is not claimed as runtime validation. Backups remain until the consumer confirms health; Windows cannot unlink its running helper and retains its stage for later cleanup. These limits and power-loss recovery are documented. Existing App defaults are untouched. Integration/producer example and final model reviews remain under the dependent integration ticket.

**agent:codex/t3-b903096c** at 2026-10-22T21:42:23Z

done to in-progress: Address PR #86 review 2734 partial helper copy prevents retry

**agent:codex/t3-b903096c** at 2026-10-22T21:43:33Z

PR #86 full review 2734, run 7b882fd5-44f0-4bb5-a0ba-146a3aa851fe (Actions #584), head d36d5efbb4098c36ec5c0ddf0bdc55d251a89c0d against base 1d8d40e487df3c60a8d96d97ff8fd50eaa287259: accepted medium finding that a partially copied helper could survive cancellation/write/sync failure and prevent retry via O_EXCL. Fixed copyFile to remove any destination it created unless the complete write/chmod/sync/close succeeds. This also hardens every prepared copy, not just the helper call. A deterministic regression cancels after the actual helper destination has received bytes, asserts it is gone and the signed staged artifact remains, retries Schedule on that exact Staged handle successfully, then cancels the waiting helper. CGO_ENABLED=0 just ci/all eight target cross builds pass. Fresh full review will be requested after pushing. The preceding #85 case-alias fix now has a clean full review, run daedd397-f228-445d-b4da-67fc25850afd / Actions #580 at 1d8d40e against dc693b2.

## Summary

Portable helper install/restart implemented with stable lifetime watches, per-install locks, integrity checks, rollback and recovery records; real Linux subprocess regressions and cgo-free just ci/all cross targets pass. Native non-Linux execution unavailable.
