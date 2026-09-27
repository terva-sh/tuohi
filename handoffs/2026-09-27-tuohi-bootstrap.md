# 2026-09-27: tuohi bootstrap

Owning ticket: TKT-01M3HWWRQGGD6BBZ02GFXQEG4D (Make tuohi ours: review, harden, and release v0.1.0).
Baseline: `main` after the scaffolding pull request, terva-sh/tuohi PR #1,
on top of `76309d9 Import malivvan/appkit v0.1.0 unmodified`. Verify it with
`git log --oneline -3` before relying on anything here.

This is orientation for the session that picks up tuohi. The tickets are the
record: read them rather than trusting this page.

## What tuohi is, and why it exists

- **What it does.** tuohi opens native windows over the operating system's
  own web engine with no cgo: WebKitGTK on Linux, WKWebView on macOS, and
  WebView2 on Windows. terva and other terva-sh tools will embed it to offer a
  desktop app over the web interface each already serves on loopback, instead
  of an Electron package.
- **Where it came from.** It is a fork of `malivvan/appkit` v0.1.0, whose
  GitHub repository was deleted after release. It came out of the
  git-ticket-canvas desktop spike, TKT-01M3HHJQPA5ZFC09BZ0SGYAR57 (Measure a
  Linux webview window build of the canvas), whose `docs/desktop-shell.md`
  measured four bindings.
- **Why this binding.** The owner ruled cgo out on 2026-09-27: a cgo build
  needs a macOS builder and a Windows C toolchain. That removed webview_go and
  both Wails lines, and left appkit's run-time binding.
- **Organisation-level history.** The adoption, the safety and provenance
  review, and the naming are in terva-sh/meta under
  TKT-01M3HS2HER41MVD76K65FVEGDY (Adopt tuohi, a no-cgo desktop webview library
  forked from appkit). `docs/provenance.md` summarises the review.

## Decisions already made (do not reopen without the owner)

- **Scope.** An embedded library, not a hosting application. It stays
  separate from terva's still-unnamed suite, which is in
  `terva/docs/plans/product-split-and-identity.md`.
- **Name and paths.** The name is tuohi (birch bark). The module is
  `github.com/terva-sh/tuohi`, following the pattern for importable
  libraries, and the public mirror is `github.com/terva-sh/tuohi`.
- **Keep the functionality,** and review the architecture on the way in to
  make the library ours.
- **No cgo in anything that ships.** Build and test with `CGO_ENABLED=0`.
- **Do not contact the original author.** That is the owner's call, for now.

## Verified state at handoff

- **Headless tests.** `CGO_ENABLED=0 go test ./...` passes. Every target in
  `just cross` builds, and `just vet`, `just check-imports`, and
  `just js-check` pass.
- **The Linux GUI scenarios have never really run.** The availability probe
  is broken, and with it fixed the binary crashes in `Destroy`. This is the
  most important thing to know, and it is TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG and TKT-01M3HWWRRXTAR4T01SK79Z4BSM.
- **macOS and Windows** cross-build but have never been run by us. They are
  tested only by `.github/workflows/ci.yml` on the GitHub mirror.
- **Carried by the scaffold:**
  - attribution for purego, Wails, and webview in NOTICE and at the code;
  - Go's BSD licence at `pure/LICENSE-GO`;
  - the module and package rename. Prose, `APPKIT_*` variables, and the
    `appkit-app` slug still say appkit, and TKT-01M3HWWRWX3D9XTA5RTW5AC26R owns that.

## Where to start

Every ticket is a draft. Promotion is the owner's. The ones that make sense
first:

1. **TKT-01M3HWWRR6V1YA9Q01NKW6E6ZG (GUI tests skip wherever bubblewrap is installed).** It has no
   dependencies, and every Linux test result is untrustworthy until it lands.
2. **TKT-01M3HWWRSJC4QVVGPW04H5CQBD (Review tuohi's architecture and write down its target shape).** It
   waits on the probe fix, because the review should be able to run the GUI
   scenarios. It is the big one, and it decides the shape the security tickets
   land in.
3. **TKT-01M3HWWRRXTAR4T01SK79Z4BSM (Linux GUI scenarios crash in view teardown on both WebKitGTK
   stacks).** It follows the probe fix. It may run alongside the review, or
   the review may decide the teardown changes shape.

TKT-01M3HWWRWX3D9XTA5RTW5AC26R (Finish renaming appkit to tuohi in prose, env vars, and names) has no
dependencies either. It is safe busywork, but it churns every file, so do it
after the review or it will conflict with everything.

## Environment on the owner's workstation

- **The GUI suite** needs `xvfb-run`, `dbus-run-session`, and the WebKitGTK
  runtime libraries, all installed on 2026-09-27. Run it with
  `just test-gui`. Without `dbus-run-session`, GTK4 waits out a 25-second
  D-Bus timeout on each portal query.
- **Playwright's WebKit** works there too, with `libevent-2.1-7t64` and
  `libwoff1`, for git-ticket-canvas's browser specs.
- **The preserved upstream archives** are in
  `~/.local/state/agent-handoffs/git-ticket-canvas/appkit-preservation/`, with
  a SHA256SUMS file and the sum.golang.org lookups.

## Process

Follow `docs/pr-reviews.md`:

1. Work on Forgejo pull requests.
2. Request a Terva review when a pull request is ready. Carry the review
   forward for commits that touch only `.tickets/`.
3. Merge, then run `just sync-github --yes`, and watch the GitHub run for
   macOS and Windows.

Name yourself on every `git ticket` write as `--actor agent:TOOL/SESSION`.
