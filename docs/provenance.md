# Provenance

tuohi began as `github.com/malivvan/appkit` v0.1.0. This page records where
that source came from, how it was verified, and what was found in it, because
the upstream repository no longer exists and its history cannot be recovered.

## The imported source

The first commit in this repository, `Import malivvan/appkit v0.1.0
unmodified`, is the module archive exactly as proxy.golang.org served it on
2026-09-27.

| | |
|---|---|
| Module | `github.com/malivvan/appkit` |
| Version | v0.1.0, tagged 2026-09-10T11:17:02Z |
| Commit | `39a4df248b7497a67a4bcd4cd781877de568eded` |
| sum.golang.org | `h1:EZvax1GNe3UyrviYb3baxdbXNVfypz8S3f5ytYFQLD0=` |

The archive's dirhash was recomputed and matches the checksum database, so the
bytes are the ones the author published. The proxy also holds a pseudo-version
from four minutes earlier, `v0.0.0-20260910111248-3dfdd1693ada`, which differs
only by the `.gitattributes` file v0.1.0 adds.

Nothing else survives. The GitHub repository returns 404 with no redirect, so
it was deleted or made private rather than renamed or moved. No other forge,
Software Heritage, or the Wayback Machine holds a copy.

## Why it disappeared

Unknown. There was no DMCA notice and no public discussion. The author's
account is active, and it appears to delete or privatise older repositories
and republish their pieces under new names. The author was not contacted,
by decision.

## What the review found

The terva-sh safety and provenance review is terva-sh/meta
TKT-01M3HS2HGRNZ7E7VF5FNKDQ0ZS (Review appkit's safety and provenance before
adopting it), which records each finding with its file and line.

- **No malicious code.** No telemetry, hidden network use, obfuscation,
  credential access, or bundled binaries.
- **Attribution gaps, now filled in [NOTICE](../NOTICE):**
  - the autostart helpers were copied from Wails v3 without credit. Wails
    v3.0.0-beta.10, published 2026-08-19, already contains the code, which
    settles the direction;
  - the WebView2 loader is a Go translation of webview/webview's;
  - `pure/` is a modified copy of Ebitengine's `purego`, missing Apache-2.0
    change notices and the Go BSD licence its fakecgo headers refer to.
- **Security weaknesses to fix before a release,** each a ticket here:
  - every page in a view can call every Go binding;
  - macOS grants camera and microphone automatically;
  - the single-instance channel can fall back to `/tmp` and trusts whatever
    connects.

## Licences

- tuohi as a whole: MIT, in [LICENSE](../LICENSE), keeping appkit's copyright
  line.
- purego: a module dependency, `github.com/ebitengine/purego`, under
  Apache-2.0, not a copy in this repository. appkit's modified copy in `pure/`
  was replaced by upstream v0.11.1 under TKT-01M3HWWRW7YGCHHZY4BFEX3A6W, which
  also retired the copy's missing change notices.
- Wails and webview: MIT, credited in [NOTICE](../NOTICE) and at the code.
