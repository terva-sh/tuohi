---
schema: 4
id: TKT-01M3HWWRVGMZTXBYJ86FCTH0V8
title: Harden the single-instance channel against other local users
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/single-instance
  - security
assignees: []
milestone: v0.1.0
parent: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D
origin: null
dependencies:
  - TKT-01M3HWWRSJC4QVVGPW04H5CQBD
  - TKT-01M3J59M1H9PZ04J2C9JJZ7V13
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:claude-code/t3code-72958710
  branch: fix/instance-unix
  worktree: /home/sothr/.cache/agent-scratch/tuohi/tmp.ajBevkVLCb/wt-inst
  commit: a54a862501af934e2318ff17b8885d8d6e808bf6
  session: null
  claimed_at: 2026-09-29T22:18:41Z
  expires_at: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-29T22:18:41Z
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

Harden the single-instance channel so that only the same user's own launches can reach the running instance, and nothing it forwards is trusted.

### Current behaviour

- **Where the socket lives.** On Unix, a lock file and a socket named after the first 8 bytes of sha256(App.ID) go in `XDG_RUNTIME_DIR`, falling back to `os.TempDir()` (`app_unix.go:40-47`).
- **Who can connect.** Permissions come from the umask, and there is no peer-credential check.
- **What it accepts.** Any JSON `[]string` is read with an unbounded `io.ReadAll` and passed to `App.Exec` (`app_unix.go:108-126`).
- **Windows** uses a named pipe with default security (`app_windows.go:85-99`).

Another local user could squat on the names in a shared `/tmp`. That is inferred, not demonstrated.

### Direction

- no `/tmp` fallback;
- a 0700 directory;
- `SO_PEERCRED` on Linux and `LOCAL_PEERCRED` on macOS;
- a size cap on messages;
- an explicit security descriptor on the Windows pipe;
- documentation that forwarded arguments are untrusted input.

## Acceptance criteria

- [x] The Unix socket lives only in a 0700 per-user directory, with no /tmp fallback
- [x] The primary instance checks the connecting peer's user and caps message size
- [ ] The Windows pipe has an explicit per-user security descriptor
- [x] The documentation says forwarded arguments are untrusted input

## Implementation plan

Two pull requests, one per backend. macOS and Windows are tested on GitHub only after a merge, so each PR is kept small enough to fix quickly if GitHub `main` goes red.

### Unix, macOS included (`fix/instance-unix`)

- **Directory.** The lock and socket live in `$XDG_RUNTIME_DIR/tuohi` when `XDG_RUNTIME_DIR` is an absolute path to a 0700 directory the user owns. Otherwise they live in `tuohi` under `os.UserCacheDir()`.
  - The directory is created 0700 and chmodded past the umask. It is refused if it is a symlink, belongs to another uid, or is open to group or others.
  - There is no fallback to `/tmp`.
- **Peer check.** `SO_PEERCRED` on Linux, and `LOCAL_PEERCRED` via `GetsockoptXucred` on macOS and FreeBSD. A peer of another uid, or one whose uid cannot be read, is closed before anything is read. NetBSD has no call for this in x/sys, so there the 0700 directory is the only guard, and `peercred_other.go` says so.
- **Caps.** A message is at most 1 MiB, and a sender has 5 s to finish; both apply on Unix and Windows. `Send` refuses an oversized message itself.
- **Lock file kept on Release.** Unlinking it lets a launcher that opened the old file and a launcher that created a new one both become primary.
- **`Message.Dir`.** The later launch's working directory, so that a relative path in Args can be resolved.
- **Socket path length.** Checked against `sockaddr_un`, so an over-long path gives a clear error.

### Windows (`fix/instance-windows`)

- **Descriptor.** The pipe gets `O:<user>D:P(A;;GA;;;<user>)` and `PIPE_REJECT_REMOTE_CLIENTS`.
- **Per-user name.** The pipe name carries the user's SID. Pipe names are machine-wide, so without it a second user was told the app was already running.
- **Sender checks.** `Send` checks that the current user owns the pipe, and connects at identification level so the server cannot impersonate the sender.
- **Serving.** Overlapped I/O with a stop event, so a read can time out and Release needs no self-connection. `Send` retries on `ERROR_PIPE_BUSY`.
- **Library.** The backend moves from purego to `x/sys/windows`.
- **CI.** The Windows GitHub job runs `./instance/` tests, which it never ran before.

### Alternatives considered

- **macOS `$TMPDIR` instead of `~/Library/Caches`.** Rejected: the system deletes idle files there, and a deleted lock file lets a second primary start.
- **Hand-written `LOCAL_PEEREID` on NetBSD.** Rejected: it cannot run anywhere this project tests, and a wrong constant would refuse every launch.
- **Several pipe instances on Windows.** Rejected: a single instance keeps the name held from Acquire to Release, so the name stays a lock. Busy senders retry instead.

## Notes

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:26:49Z

### Findings and decisions from TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape)

- **Where it lands.** This lands in the new `tuohi/instance` package (TKT-01M3J59M1H9PZ04J2C9JJZ7V13, Move desktop services out of the root package). The Unix and darwin copies (`app_unix.go:28-141`, `app_darwin.go:106-194`) become one `//go:build unix` file.
- **Unlock race.** `release` unlocks and then unlinks the lock file (`app_unix.go:101-103`). A launcher that opened the old file before the unlink can lock an unlinked inode while a newer one creates a fresh file, which gives two primaries. Do not unlink the lock file, or unlink it while still holding the lock.
- **Dropped launches on Windows.** With `maxInstances=1` and no `WaitNamedPipe`, a launch that arrives while the server is busy is dropped silently (`app_windows.go:85-104`). Any pipe create failure is also read as "already running".
- **Working directory.** It is not forwarded, so relative paths in forwarded arguments resolve against the primary's directory. Forward it.
- **No deadline on Unix.** `io.ReadAll` has no deadline as well as no cap.

**agent:claude-code/t3code-92c88910** at 2026-09-27T19:35:06Z

Supersedes the 'Unlock race' fix in the previous note, which offered 'or unlink it while still holding the lock'. That alternative is wrong. A launcher that opened the old inode before the unlink can still take the lock on it once it is released, while another launcher creates and locks a new file at the same path, which gives two primaries. The fix is to never unlink the lock file: it stays in the per-user runtime directory for good, and only the socket is removed. Found by terva-review on tuohi PR #4.

**agent:claude-code/t3code-72958710** at 2026-09-29T22:18:41Z

Unix branch `fix/instance-unix`, rebased onto main at 669905a. The work was done by a subagent; I reviewed the diff.

### Checks

- **`just ci`** passes. `go vet ./instance/` passes for darwin, windows, freebsd (with the fakecgo flag) and netbsd.
- **The subagent ran:**
  - `just test-gui` on both WebKitGTK stacks.
  - golangci-lint, with 0 issues on linux, darwin, windows, netbsd and freebsd.
  - `go test -race -count=3 ./instance/`.
- **Negative controls.** Twelve, each run once and restored, and each failed as it should:

| # | What was broken | Test that caught it |
|---|---|---|
| 1 | Directory mode check removed | `TestXDGRuntimeDir` and `TestOpenDirRefused` |
| 2 | Owner check removed | `TestCheckDir` |
| 3 | `Lstat` replaced by `Stat` | `TestCheckDir` |
| 4 | `/tmp` fallback restored | `TestDirIsPrivateWithoutTempFallback` |
| 5 | Directory created 0755 | `TestDirIsPrivateWithoutTempFallback` |
| 6 | Release unlinks the lock file | `TestLockFileSurvivesRelease` |
| 7 | Size cap removed | `TestOversizedMessageDropped` |
| 8 | Read deadline removed | `TestStalledSenderTimesOut` |
| 9 | `Dir` not sent | the round-trip test |
| 10 | Peer check allows any uid | `TestPeerAllowed` |
| 11 | Socket length check removed | `TestSocketPathTooLong` |
| 12 | `Send` size refusal removed | `TestSendRefusesOversized` |

### Not covered

- **Cross-user cases** cannot run as a non-root user. They are covered by a unit test of the uid comparison and a fake uid for the owner check.
- **macOS `LOCAL_PEERCRED`** runs on GitHub only after the merge.
- **The Windows write deadline.** I checked that Go 1.26, `go.mod`'s minimum, already detects overlapped handles in `os.NewFile` through `windows.IsNonblock`. The subagent had confirmed this only for 1.27.
