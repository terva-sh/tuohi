# Desktop acceptance before release

Run these checks against a disposable installation and test accounts. Record the
tuohi commit, consumer commit, OS/architecture, desktop/runtime version, bundle ID,
install mode, observed result and sanitized evidence for each check. An unexecuted
check stays pending. Do not put credentials, account/room IDs, message bodies or
signing-key material in the record. Publication is a separate release action.

## Established evidence

At tuohi `cebf4c0750bd1cd5618268e0b2e13925e53c350a` (the `v0.2.0` tag target),
[GitHub run 39904111530](https://github.com/terva-sh/tuohi/actions/runs/39904111530)
passed lint, every build, both Linux GTK stacks on x64/ARM, native macOS/Windows
tests and consumer examples with `CGO_ENABLED=0`. The native macOS
`TestElementFullscreenPreference` passed, rather than skipping, in
[job 120252109863](https://github.com/terva-sh/tuohi/actions/runs/39904111530/job/120252109863).
The branch run skipped its release job; the tag's separate workflow gates
publication. This source also includes the real-window-manager maximize/restore
regression on both GTK stacks.

These results cover automated lifecycle assertions, private D-Bus protocol
fixtures, portable updater subprocesses and the macOS relative-dylib helper
fixture. They do not establish visible notification delivery, tray usability,
interactive permission prompts, production signing/notarization or successful
replacement of a production team-signed bundle.

Kuiske owns integration in [PR #34](https://git.local.sothr.com/terva-sh/kuiske/pulls/34)
and its consumer tickets for content-free notifications
(`TKT-01M43TRAJW4ZWVXPK79YXX9R0Q`) and close-to-tray
(`TKT-01M44GTHZY4ZAASWMSBG1VEFHJ`). Keep desktop services at its adapter edge.
Record its exact tested pin and source in the acceptance ticket when they change;
tuohi's CI is not evidence that a particular consumer binary passed these checks.
The initial read-only integration snapshot is Kuiske
`d6c64099f20b7d56501be5ac04670d973c925951`, pinned to tuohi
`v0.1.2-0.20261022233646-d7f113cc07a5`; its final review/checks were reported
clean. This pin is a development pseudo-version, not a new published tuohi release.

Additional consumer acceptance is recorded in Kuiske PR #34 at documentation
source `231a758` (runtime source `a6cc50d`; the same tuohi pin). Its relocated
production Linux amd64 executable passed icon/menu interaction, retained-window
hide/reopen, second-instance focus, panel-loss recovery, no-host close/exit, tray
Quit, and locked/missing credential-provider guidance on both engines. The
disposable Debian 13 X11/Xvfb session used WebKitGTK 2.52.6,
GTK 4.18.6/3.24.49, Xfce panel 4.20.4, Openbox 3.6.1 and GNOME Keyring 48.0.
The profile had no enrolled accounts. This is actual panel/executable acceptance,
without physical-device/Wayland, real-daemon message delivery or tagged-consumer
release acceptance. See Kuiske's
[acceptance procedure](https://git.local.sothr.com/terva-sh/kuiske/src/commit/231a758/docs/desktop-acceptance.md).

## Managed notifications

Use [managednotify](../examples/managednotify/main.go) for a minimal public-API
consumer and Kuiske for multi-account/room routing. Build from `examples/`:

```sh
CGO_ENABLED=0 go build -o "$ACCEPT_DIR/managednotify" ./managednotify
```

Set `ACCEPT_DIR` to a disposable task directory first. On macOS, package the
binary as `ManagedNotify.app/Contents/MacOS/managednotify` with a real
`Contents/Info.plist` declaring `CFBundlePackageType=APPL`, a stable test
`CFBundleIdentifier`, and `CFBundleExecutable=managednotify`. Follow the consumer's
normal signing/launch procedure and keep its native event loop running. A bare
binary's `ErrUnavailable` verifies a guard, not delivery. Do not replace another
library's notification-center delegate.

1. Start without `-request-permission`: query alone must not prompt. Record the
   status. Explicitly choose permission request, then launch with
   `-request-permission` in the same test bundle identity. Test allow and deny in
   separate controlled permission states; denied permission must not repeatedly
   prompt. Do not reset the person's real application permissions.
2. With permission granted, observe generic notification text. The example posts
   and immediately replaces one handle; verify the replacement does not create a
   second independent notification. To inspect both states separately, use a
   consumer that exposes the two actions separately.
3. Hide or defocus the existing window and click the notification. The example
   must reveal that view. In Kuiske, exercise two accounts with distinct rooms;
   each activation must open its correct destination without putting routing
   data in OS payloads or logs. Read the room and verify applicable handles are
   dismissed; unrelated-room notifications must remain.
4. Quit or cancel during a pending operation. It must finish within its deadline
   or return an explicit error, stop admitting callbacks and release resources.
   Already admitted callbacks may finish. Record OS suppression/Focus mode
   separately from API acceptance; successful submission does not prove display.

Linux needs a session bus and an actual notification daemon supporting actions.
Also test daemon disappearance/restart: old handles must end with
`ErrUnavailable`, and a new client must not act on old daemon IDs. Windows managed
notifications currently return `ErrUnsupported`; consumers must handle that
explicitly. Legacy balloons do not satisfy managed click/dismissal acceptance.

## Tray and retained window

Use Kuiske for background policy; [showcase](../examples/showcase/main.go) with
`-tray` (and macOS `-dock`) can check the basic native item/menu.

1. Change unread/highlight state while the tray runs. Verify icon light/dark or
   template variants, tooltip, menu labels, checked/disabled state and callbacks
   update without recreating the tray. Do not expect runtime Dock-policy changes.
2. Close the window using its native button and keyboard shortcut. Cancel/hide
   must preserve the renderer and draft UI state; reopening must reveal the same
   view. Confirm focus/visibility changes reach the adapter and active-room
   notification suppression follows actual application state.
3. On a disposable Linux desktop, test no watcher, watcher without a host, and
   host loss/return. The only window must remain reachable when availability is
   insufficient, including when the host disappears after hiding. Watcher return
   should recover registration. A lost bus requires a new tray lifetime.
4. On Windows, exercise taskbar restart in a disposable session and registration
   recovery. On macOS, inspect menu-bar visibility, Dock and Command-Tab behavior
   with/without `Dock`. Always test normal Quit while a close request is deferred.
   Deferred close decisions must not trap application shutdown.

Record GNOME extension state, WSLg/nested-session host behavior and Steam Frame
desktop mode separately. `Running && Registered && HostPresent && Err == nil`
is a conservative prerequisite for hiding, never proof the user can see or use
the icon. Actual desktop acceptance is still required.

## Update installation and recovery

Use two disposable, normally packaged consumer versions with increasing compiled
release sequences and a test publisher/feed. Follow the
[publisher procedure](../updater/README.md#publisher-procedure). The
[selfupdate example](../examples/selfupdate/main.go) defaults to managed mode;
`-check` verifies staging and shutdown only and never installs anything.

1. For portable installs, select `-install-mode portable` and the persistent
   installed executable as `-target`; do not use `go run`'s temporary binary.
   For macOS bundles use `-install-mode bundle`, an absolute `.app` root and the
   same application/bundle identifier as the two properly team-signed versions.
   Sign/notarize/staple before hashing the complete symlink-preserving archive.
2. Check and stage before install consent. Verify corrupt, expired, wrong-app,
   wrong-platform and wrong-key candidates cannot enable installation. Do not
   relax trust checks to make a fixture installable. A test signature does not
   establish production signing/notarization acceptance.
3. Persist application state, schedule, quit normally and release single-instance
   ownership. Confirm replacement waits for process exit, relaunches the new
   version exactly once, reacquires ownership and restores account/settings/UI
   state as intended. Record the result and backup retention.
4. Exercise cancellation before shutdown and a controlled relaunch failure in the
   disposable fixture. Confirm the original installation remains usable or the
   backup restores it, and the result reports the actual outcome. Ordinary
   rollback tests do not establish power-loss durability. Do not corrupt a real
   installation or remove a backup before application health is established.
5. Verify package/store/administrator-managed installs return `ErrManaged` and
   direct the user to the normal installer, without elevation or self-replacement.

## Clipboard images

`clipboard.PasteImage` (v0.3.0) has GUI scenarios on both WebKitGTK stacks
under Xvfb, in which a separate owner process puts real images on a real X11
clipboard; they run in both CIs at tuohi `debc890`. Forgejo CI ran them on
Debian 13 with WebKitGTK 2.54.0.

Real WSLg, on the owner's machine: Windows build 10.0.26200, WSL 2.7.11, WSLg
1.0.73, Ubuntu 24.04.5 x86_64, WebKitGTK 2.52.6 with both stacks installed,
and a Wayland session. An image copied in a Windows browser is offered as
exactly `text/html image/bmp`; a v0.2.0 probe's paste event carried only
`text/html`. On 2026-10-24 the owner pasted a real Windows browser image into
Kuiske at tuohi `debc890` (pseudo-version
`v0.2.1-0.20261024003306-debc890bea1e`, Kuiske branch
`t3/native-clipboard-image`), and Kuiske's preview opened. The WebKitGTK stack
of that run was not recorded. Kuiske's `scripts/check.sh` and
`scripts/desktop-check.sh` passed at that pin on its default stack under
Xvfb; Kuiske CI had not run there. Separately, a tuohi probe on that machine
read a Wayland `image/bmp` offer on GTK 3.

macOS and Windows return `ErrUnsupported` and have no acceptance to record.

## Remaining acceptance

| Check | Status at the recorded source |
| --- | --- |
| Cgo-free native CI and prescribed cross builds | Passed, run linked above |
| Kuiske routing and background policy | Implementation complete; real Xfce panel and retained-window checks passed on both Linux engines as scoped above |
| Interactive macOS notification query/request/denial/click/dismiss | Pending macOS desktop and packaged test consumer |
| Production team-signed bundle update/rollback/relaunch | Pending signed disposable releases and authorized native environment |
| Real tray usability on macOS, WSLg and Steam Frame | Pending consumer platform acceptance |
| Clipboard image paste under WSLg | A real Windows browser image pasted into Kuiske, stack unrecorded; see Clipboard images |
| Public release/feed publication | Separate owner-controlled action, not performed by this runbook |

Track execution and sanitized evidence in
`TKT-01M5KG5ZYY361QYFCVNET7KA63` (Validate packaged desktop integration before
release). Leave unavailable cells open and continue independent implementation.
