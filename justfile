# tuohi dev tasks. `just` lists them.
#
# `just ci` runs the same steps as .forgejo/workflows/ci.yml, the gate for
# internal pull requests. That workflow keeps its commands inline because its
# container does not install just, so change both together. The Makefile is
# inherited from appkit and still works; these recipes are the ones this
# repository maintains.

set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

# Everything is built and tested the way consumers build it: without cgo.
export CGO_ENABLED := "0"

# Every GOOS/GOARCH pair that must build. Only Linux is tested here; the
# GitHub workflow runs the macOS and Windows engines on hosted runners.
targets := "linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64 netbsd/amd64"

default:
    @just --list

# go vet for this platform, darwin and windows, since most code is per-OS.
vet:
    go vet ./...
    GOOS=darwin go vet ./...
    GOOS=windows go vet ./...

# Rewrite sources with gofmt.
fmt:
    gofmt -w .

# Fail if gofmt would change a file.
fmt-check:
    @diff=$(gofmt -l .); if [ -n "$diff" ]; then echo "gofmt issues in:"; echo "$diff"; exit 1; fi

# Fail if net/http or crypto/tls is imported outside the allowed files.
check-imports:
    make --no-print-directory check-imports

# Every package but pure/, whose suite is upstream purego's and needs a C
# compiler for its fixtures. Forgejo CI tests exactly this set.
packages := `go list ./... | grep -v '/pure' | tr '\n' ' '`

# The headless tests. GUI scenarios skip themselves without a display.
test:
    go test {{packages}}

# purego's own suite in pure/. Needs a C compiler; GitHub CI runs it.
test-pure:
    go test ./pure/...

# The GUI scenarios on both WebKitGTK stacks, under Xvfb and a private bus.
# TUOHI_REQUIRE_GUI=1 fails a scenario that would skip, so a green run means
# the scenarios ran. Both stacks run even when the first fails.
test-gui:
    failed=""; \
    for backend in webkitgtk-6.0 webkit2gtk-4.1; do \
        echo "== $backend"; \
        TUOHI_REQUIRE_GUI=1 APPKIT_BACKEND=$backend dbus-run-session -- xvfb-run -a -s '-screen 0 1600x1000x24' go test -count=1 {{packages}} || failed="$failed $backend"; \
    done; \
    if [ -n "$failed" ]; then echo "test-gui failed on:$failed" >&2; exit 1; fi

# Build every package for every target.
cross:
    for t in {{targets}}; do \
        echo "== $t"; \
        GOOS=${t%/*} GOARCH=${t#*/} go build ./...; \
    done

# Validate the ticket store the way CI does.
tickets-check:
    git ticket check --fix --dry-run --strict

# Parse the JavaScript embedded in Go strings with node, and run its tests.
js-check:
    make --no-print-directory js-check

# What Forgejo CI runs, in order.
ci: vet fmt-check check-imports js-check test cross tickets-check

# Never force-pushes, and stops if the two have diverged.
# Fast-forward whichever of Forgejo and GitHub main is behind; --yes to push.
[positional-arguments]
sync-github *flags:
    #!/usr/bin/env bash
    set -euo pipefail
    yes=false
    for f in "$@"; do
        case "$f" in
            --yes) yes=true ;;
            *) echo "sync-github: unknown flag $f" >&2; exit 2 ;;
        esac
    done
    git fetch --quiet origin main
    if ! git fetch --quiet github main 2>/dev/null; then
        echo "github/main does not exist yet; push it with: git push github origin/main:refs/heads/main" >&2
        exit 1
    fi
    forgejo=$(git rev-parse origin/main)
    github=$(git rev-parse github/main)
    if [ "$forgejo" = "$github" ]; then
        echo "in sync at ${forgejo:0:12}"
        exit 0
    fi
    if git merge-base --is-ancestor "$github" "$forgejo"; then
        from=origin; to=github; head=$forgejo
    elif git merge-base --is-ancestor "$forgejo" "$github"; then
        from=github; to=origin; head=$github
    else
        echo "diverged: origin/main ${forgejo:0:12}, github/main ${github:0:12}" >&2
        echo "merge them on a branch and land it through a Forgejo PR" >&2
        exit 1
    fi
    echo "fast-forward $to/main to $from/main ${head:0:12}:"
    git log --oneline "$to/main..$head"
    if [ "$yes" != true ]; then
        echo "dry run; pass --yes to push"
        exit 0
    fi
    git push "$to" "$head:refs/heads/main"
