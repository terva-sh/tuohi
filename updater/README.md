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
