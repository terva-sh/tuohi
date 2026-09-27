---
schema: 4
id: TKT-01M3HWWRVGMZTXBYJ86FCTH0V8
title: Harden the single-instance channel against other local users
type: task
status: draft
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
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-27T16:59:09Z
updated_at: 2026-09-27T16:59:09Z
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

- [ ] The Unix socket lives only in a 0700 per-user directory, with no /tmp fallback
- [ ] The primary instance checks the connecting peer's user and caps message size
- [ ] The Windows pipe has an explicit per-user security descriptor
- [ ] The documentation says forwarded arguments are untrusted input
