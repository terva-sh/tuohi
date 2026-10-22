# Desktop updates

`github.com/terva-sh/tuohi/updater` is an optional, cgo-free desktop service.
The window package never imports it. It does no polling, UI, automatic exit,
elevation, key discovery or network transport. Applications own those policies.

Create a `Client` with an application identifier, channel, compiled current
release sequence, pinned Ed25519 keys and a `Fetch` function. `Check(ctx)` returns
an immutable authenticated `Release`; `Release.Stage(ctx, progress)` streams and
verifies the artifact in a private temporary directory. Always defer
`Staged.Close()`. Methods return errors and never log URLs or payloads.

The consumer's transport must honor cancellation while opening **and reading**,
accept only successful responses, bound redirects and timeouts, and require
HTTPS through the entire redirect chain. Authentication belongs in transport
headers, not URLs or manifests. Tuohi preserves its existing independence from
Go's HTTP/TLS stack. Consumers may use their own existing HTTP client.

## Signed release protocol

The feed is JSON with exactly `key_id`, `payload`, and `signature`. The last two
are standard padded base64 strings (Go's `[]byte` JSON encoding). The signature
is Ed25519 over the bytes `tuohi-update-manifest-v1` followed by one zero byte
and then the **exact decoded payload bytes**. There is no JSON canonicalization.
Sign offline in the publisher's release pipeline. Private keys never go into
tuohi configuration, the application binary, tickets, examples, or update feeds.

The payload has this shape:

```json
{
  "schema": 1,
  "application": "com.example.desktop",
  "channel": "stable",
  "version": "1.2.0",
  "sequence": 42,
  "expires": "2026-12-01T00:00:00Z",
  "assets": [{
    "os": "linux",
    "arch": "amd64",
    "kind": "executable",
    "url": "https://downloads.example/desktop-1.2.0-linux-amd64",
    "size": 12345678,
    "sha256": "<64 lowercase hexadecimal characters>"
  }]
}
```

`sequence` must increase on a channel, including when rolling back a bad release
to older source. Compile it into each release. Version is display text, not an
ordering rule. Consumers that support manual executable downgrades should persist
the highest successfully installed sequence and use the maximum of that and the
compiled value. Persisting merely *offered* sequences can prevent recovery from
an unusable update, so the library does not do that automatically.

Metadata is limited to 1 MiB and 64 distinct platform/kind assets. Duplicate JSON
keys, unknown fields, trailing values and duplicate asset identities fail closed.
The selected asset must match the actual process's OS, architecture and configured
kind, have an exact size, and match its SHA-256 digest. The default artifact limit
is 256 MiB; consumers can configure a bound up to 1 TiB. Expiry is checked using
the local wall clock at check, stage and install time. An expired release must be
republished with a fresh signed manifest before installation.

## Trust and recovery

Pinned keys identify the release publisher; never trust a key supplied by a feed.
Multiple pre-pinned key IDs allow planned rotation. This protocol does not provide
threshold signatures, revocation recovery after every pinned key is compromised,
trusted time, transparency, or protection from a compromised publisher. Feed
unavailability or replay of expired metadata fails closed, but a server can deny
updates. A non-expired older release above the consumer's installed sequence can
still be offered; this is not a full TUF repository protocol.

Local installation directories and staging parents must be controlled by the
current user. Same-user malware, administrator compromise and hostile filesystems
are outside the trust boundary. The updater never elevates privileges. Installation
and crash-recovery semantics are described below as each supported mode is added.

## Portable installation and restart

A consumer-owned standalone executable uses `PortableInstall`. The target must
be the actual running executable, a regular non-symlink file, with no set-ID
bits, in a writable installation directory. On macOS this mode refuses binaries
inside `.app` bundles. `ManagedInstall` always returns `ErrManaged`: package
ownership cannot be inferred safely from paths. Your packaging code must select
this mode for distro packages, Flatpak/Snap, managed MSI installs, and other
externally owned installations, and offer their normal update mechanism.

Call `HandleHelper(context.Background())` before parsing flags, acquiring single
instance, starting servers, or opening windows. If it returns `handled=true`,
report any error and return from main (with a nonzero exit code on error). It
recognizes only its exact internal invocation and reads a bounded plan on stdin.

After consent and a successful stage, call `Staged.Schedule(ctx, RestartOptions{...})`.
It revalidates expiry and integrity, prepares a candidate beside the target,
and starts a copy of the **current** executable as a helper. It returns only when
the helper has acquired an installation lock and a stable parent-process watch.
Linux requires pidfd support; older kernels return `ErrUnsupported`. macOS/BSD
use kqueue process-exit events; Windows uses a process handle. No PID polling or
fixed shutdown delay is used. Separate helpers serialize through a persistent
sibling lock file; do not remove it while updates might be running.

Save application state, call `App.Quit`, let `App.Wait` return, stop services,
release the single-instance lock, and return from main normally. The helper waits
for actual process termination before replacing or relaunching anything. Keep
UI calls on their documented threads; staging/progress run on the calling worker.
`Schedule` does not call Quit or enforce application policy. Calling it with a
program that omits the early helper handler fails readiness after at most ten
seconds. A successful schedule transfers stage ownership, so deferred
`Staged.Close` is harmless. Use `Pending.Cancel` if shutdown is abandoned; it
kills the waiting helper and removes preparation files while the parent is still
alive. Context cancellation after successful scheduling does not abort the helper.
The helper's parent-exit deadline defaults to five minutes and can be configured
from one second to one hour.

`RestartOptions.Args` are explicit, excluding argv[0]; do not blindly replay
one-shot actions or credential arguments. No shell is involved. The environment
is inherited. The working directory defaults to the installation's parent.

The helper rechecks the installed target and candidate before changing them.
It moves the old installation to a private `backup`, moves the candidate into
place, then launches it. Ordinary replacement/launch failures restore and launch
the old executable. A changed target is left alone and is not launched. A helper
that times out while the parent remains alive leaves the installation untouched.
A successful process start is **not** an application health check: the old backup
is retained for consumer-controlled recovery. There is no automatic health-based
rollback, elevated installer, or power-loss transaction across filesystem moves.
A crash between moves can leave the target absent; restore `backup` from the
reported recovery directory. Filesystems must support ordinary local locking and
same-directory rename semantics. Network-mounted installations are unsupported.

`Pending.ResultPath` identifies a retained private JSON record containing outcome,
release version/sequence and recovery directory; `ReadResult` reads it. It contains
no arguments, environment or credentials. Consumers should store this path before
shutdown and inspect it after a subsequent launch. Result persistence is best
effort (disk errors can prevent it); installation backups remain available even
then. After validating the new app, the consumer owns removing the recovery
directory. Windows also retains the private stage, because a running helper
cannot unlink its own executable; reap obsolete stages only after helper exit.
Compilation of the Windows/BSD code does not establish native installation behavior.

## Complete macOS bundles

Use `Config.Kind=AppBundle` and `BundleInstall` with the absolute installed `.app`
root. The signed artifact is a ZIP containing exactly one top-level `.app`, not
just its executable. Application must equal `CFBundleIdentifier`. The current and
replacement bundles must have valid code signatures and the same nonempty Apple
team identifier and executable name. Ad-hoc/unsigned bundles are unsupported.
The stage must be outside the installed bundle, including through symlink and
case aliases. Filesystem identities along both ancestor chains are checked before
the native helper clone; overlapping trees or paths that cannot be inspected fail.
Publisher pipelines must retain normal platform signing/notarization obligations;
Ed25519 feed authentication does not replace them. The updater checks code-signature
integrity/team identity; it is not a notarization service or Gatekeeper bypass.

Package ZIPs with symlinks preserved (for example, `zip -r -y` on a finished,
signed bundle), with no `__MACOSX` sibling, resource-fork extras, or surrounding
release directory. The archive is limited to 20,000 entries and 1 GiB expanded.
Paths must be relative, NFC Unicode, and free of case collisions, backslashes,
colons, duplicate entries and traversal. Regular file executable modes survive
extraction. Relative framework symlinks are supported if they resolve entirely
inside the bundle; dangling, cyclic and escaping links or archive entries below
links are rejected. The ZIP digest and complete prepared tree digest are checked;
code signatures, app identity and team equality are checked before scheduling and
again before replacement.

The helper runs from a complete private copy of the current `.app`, preserving
`@executable_path`/relative loader dependencies, bundle resources and native metadata.
It replaces the whole installed `.app`, retains `backup.app`, then starts the
installed bundle executable. This preserves the bundle location and process bundle
identity. Executable-name changes are unsupported, so ordinary rollback can use
the same executable path. Power loss between the two directory moves can require
restoring `backup.app` manually, as for portable updates. `Pending.ResultPath` is
outside the bundle so it survives replacement. App Store/MDM/package-owned bundles
must use managed mode instead. The filesystem must be writable by the current
user; the updater never requests administrator rights.

Extraction/security regressions run on Linux. The macOS-native plist/codesign and
process-watch paths are cross-compiled here; native macOS update execution is not
available in this workspace and is not claimed as tested runtime behavior.
