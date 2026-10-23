---
schema: 4
id: TKT-01M5KG5ZYY361QYFCVNET7KA63
title: Validate packaged desktop integration before release
type: task
status: blocked
status_reason: Interactive macOS notification/tray acceptance needs a native desktop; production bundle update acceptance needs disposable team-signed releases and authorized signing/native environments; WSLg and Steam Frame consumer hardware not available here. Runbook and automated CI evidence recorded; independent selected work continues.
priority: high
due_on: null
labels:
  - area/release
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3MV9EBAGG435E6V7JFQHYA2
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim:
  actor: agent:codex/t3-b903096c
  branch: t3/desktop-acceptance
  worktree: /home/sothr/.t3/worktrees/tuohi/t3-b903096c
  commit: 68fc3ac7539712deb1cabe405142b38d317e67bb
  session: null
  claimed_at: 2026-10-23T04:28:03Z
  expires_at: null
archive: null
created_at: 2026-10-23T04:26:52Z
updated_at: 2026-10-23T04:55:55Z
created_by:
  id: agent:codex/t3-b903096c
  name: ""
updated_by:
  id: agent:codex/t3-b903096c
  name: ""
extensions: {}
---

## Description

The owner selected packaged-app acceptance and Kuiske integration before the GTK3 maximize fix, Windows navigation investigation and fullscreen documentation. Track remaining release acceptance without reopening completed implementation tickets or claiming hosted CI proves interactive packaged-app behavior.

Kuiske integration belongs to its existing session 564d5d21-1210-473a-aa2b-918183ce9764 and consumer tickets TKT-01M43TRAJW4ZWVXPK79YXX9R0Q (Show content-free desktop notifications) and TKT-01M44GTHZY4ZAASWMSBG1VEFHJ (Show a tray icon and optionally keep running when closed). Its PR #34 reports implementation complete; request exact pin, checks and pending acceptance there rather than editing Kuiske here.

Provide a repeatable acceptance record/runbook covering bundled macOS managed notifications (query/request/denial/activation/replacement/dismissal and shutdown), actual tray host absence/return and hidden-window reachability, retained-window state, and production team-signed whole-bundle update/rollback/relaunch. Record existing CGO_ENABLED=0 CI evidence at 68fc3ac, distinguish private-bus/helper fixtures from desktop delivery, and leave unexecuted platform cells open. Do not use or publish production credentials, release tags, feeds or artifacts. Signing/hardware-dependent execution requires those environments; continue other selected work while unavailable.

## Acceptance criteria

- [x] Publish a repeatable acceptance runbook and precise source/platform evidence.
- [x] Record Kuiske integration source, tuohi pin and remaining consumer acceptance.
- [ ] Execute bundled macOS notification and production-signed bundle update acceptance, or leave these explicitly pending with environment requirements.
- [ ] Record actual tray host and retained-window acceptance on selected consumer platforms without treating registration as visibility.

## Implementation plan

Add docs/desktop-acceptance.md with exact tested-source evidence, consumer source/pin, runnable example setup and observable platform acceptance steps. Keep interactive packaged-app and production signing cells pending unless exercised on the required desktop. Coordinate through the existing Kuiske thread; no Kuiske edits or publication here. Record environment constraints and continue the other owner-selected tickets.

## Notes

**agent:codex/t3-b903096c** at 2026-10-23T04:28:26Z

Created docs/desktop-acceptance.md with runnable example entry points and observable acceptance cases. Verified Kuiske read-only at d6c64099f20b7d56501be5ac04670d973c925951, pin v0.1.2-0.20261022233646-d7f113cc07a5; session reported PR #34 clean final review/all 11 checks. Its notification/tray implementation is complete; Steam Frame, WSLg and macOS manual acceptance remains open. Tuohi mirror run 39873829324 at 68fc3ac passed every native job/build. This Linux workspace cannot execute macOS permission dialogs, inspect native menu-bar usability or install production team-signed bundles; no signing credentials or target hardware provided. These are execution gaps, not failed library tests. Keep criteria 3 and 4 pending; continue other selected implementation while awaiting native acceptance.

**agent:codex/t3-b903096c** at 2026-10-23T04:28:26Z

in-progress to blocked: Interactive macOS notification/tray acceptance needs a native desktop; production bundle update acceptance needs disposable team-signed releases and authorized signing/native environments; WSLg and Steam Frame consumer hardware not available here. Runbook and automated CI evidence recorded; independent selected work continues.

**agent:codex/t3-b903096c** at 2026-10-23T04:28:59Z

Runbook validation: CGO_ENABLED=0 just ci passed both modules, prescribed platform cross builds and strict ticket validation. Commands/flags checked against managednotify, selfupdate and showcase sources. No API/code changed; native delivery/signing/manual host observations remain pending as recorded.

**agent:codex/t3-b903096c** at 2026-10-23T04:55:55Z

Additional consumer evidence read from Kuiske docs/desktop-acceptance.md at documentation source 231a758, runtime a6cc50d, same tuohi pin v0.1.2-0.20261022233646-d7f113cc07a5. Actual relocated production executable passed Xfce panel interaction, native close hide/reopen retaining settings, second-instance focus, panel-loss recovery, no-host close/exit, tray Quit and locked/missing GNOME Keyring guidance on GTK4 and GTK3. Environment: disposable Debian 13 amd64 X11/Xvfb, WebKitGTK 2.52.6, GTK 4.18.6/3.24.49, Xfce panel 4.20.4, Openbox 3.6.1, GNOME Keyring 48.0; empty account profile. This is actual Linux panel acceptance, not physical-device/Wayland, WSLg/Steam Frame, enrolled-account notification delivery, tagged consumer or packaged macOS/signing acceptance. AC4 remains open for selected unavailable platforms. Owner has now requested a tuohi source release; publication task TKT-01M5KHNGVKW2Y5ABMBV8MZ5SCC (Release tuohi v0.2.0) will disclose these limits rather than close this acceptance ticket. Tuohi main f022d8c native GitHub run 39902385484 passed all jobs.
