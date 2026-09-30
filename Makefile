.PHONY: all build vet test test-short cover lint lint-bsd fmt tidy check-imports check-examples cross cross-bsd js-check examples clean

# Default target: runs the CI checks for the library.
all: build vet test check-imports cross js-check

build:
	go build ./...

vet:
	go vet ./...

# FreeBSD needs purego's fakecgo compiled with -std when cgo is off: it
# exports environ and __progname with a directive the compiler otherwise
# refuses. golangci-lint builds through GOFLAGS, so it gets the flag there.
FAKECGO_STD := -gcflags=github.com/ebitengine/purego/internal/fakecgo=-std

# The per-OS engine files only compile on their native OS, so CI must
# cross-build the Tier-1 matrix - every OS/arch pair the README ships (the
# purego bindings and the net/http/crypto-tls-free code cross-compile
# everywhere) - plus the Tier-2 BSD targets (see cross-bsd). linux/amd64 is
# covered by `build`.
cross: cross-bsd
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...
	GOOS=darwin GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=linux GOARCH=arm64 go build ./...

# cross-bsd builds the Tier-2 FreeBSD/NetBSD targets with cgo disabled.
cross-bsd:
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go build $(FAKECGO_STD) ./...
	CGO_ENABLED=0 GOOS=netbsd GOARCH=amd64 go build ./...

# The injected JS (bridge, events, bind script) lives inside Go raw
# strings, where a syntax error is only caught by a parser. The behavioral
# node harnesses skip when node is absent, so this target makes the PARSE
# check mandatory: it fails when node is missing, and otherwise runs the
# parse + behavioral Script/Bridge tests that node gates.
js-check:
	@command -v node >/dev/null 2>&1 || { echo "js-check: node is required (the injected JS is parsed and behavior-tested with it)"; exit 1; }
	go test -run 'Script|Bridge' -count=1 .

# check-examples ensures the examples use only tuohi's public API. Go's
# internal rule goes by import path, and examples/ sits under tuohi's, so the
# compiler would let an example import tuohi/internal/...; this refuses it,
# test files included. The examples' own examples/internal is theirs to use.
# It also fails when examples/go.mod or go.sum is not tidy.
check-examples:
	@imports="$$(cd examples && go list -f '{{range .Imports}}{{println .}}{{end}}{{range .TestImports}}{{println .}}{{end}}{{range .XTestImports}}{{println .}}{{end}}' ./...)" || exit 1; \
	forbidden="$$(printf '%s\n' "$$imports" | sort -u | grep -E '^github\.com/terva-sh/tuohi/internal(/|$$)')"; \
	if [ -n "$$forbidden" ]; then \
		echo "check-examples: examples import tuohi internals:"; \
		echo "$$forbidden"; \
		exit 1; \
	fi; \
	(cd examples && go mod tidy -diff) || { echo "check-examples: examples/go.mod is not tidy; run make tidy"; exit 1; }; \
	echo "check-examples: ok (public API only, go.mod tidy)"

# check-imports ensures the module does not import net/http or crypto/tls.
# tuohi must remain independent of the stdlib HTTP/TLS stack: its per-view
# loopback server (always on macOS, under App.HTTP on Linux and Windows) and
# the custom-scheme serving are hand-rolled on package net. This scans the
# imports of every package, test files included, for the current GOOS, so
# prose comments mentioning these packages cannot trigger it.
check-imports:
	@imports="$$(go list -f '{{range .Imports}}{{println .}}{{end}}{{range .TestImports}}{{println .}}{{end}}{{range .XTestImports}}{{println .}}{{end}}' ./...)" || exit 1; \
	forbidden="$$(printf '%s\n' "$$imports" | sort -u | grep -E '^(net/http|crypto/tls)$$')"; \
	if [ -n "$$forbidden" ]; then \
		echo "check-imports: forbidden stdlib imports found:"; \
		echo "$$forbidden"; \
		exit 1; \
	fi; \
	echo "check-imports: ok (no net/http or crypto/tls imports)"

# Full test suite, including OS-specific GUI scenarios. Runs with a timeout so
# a stalled GUI test fails quickly rather than waiting for the default 10m go
# test timeout. On Linux, GUI tests require a display; run them under xvfb-run.
test:
	go test -timeout 180s ./...

# Fast, headless execution: TestMain respects -short and skips GUI scenarios.
test-short:
	go test -short ./...

# Coverage over the headless-safe tests (GUI scenarios skip without a display);
# prints the total and leaves coverage.out for `go tool cover -html`.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

# lint-bsd type-checks the Tier-2 FreeBSD/NetBSD targets (see FAKECGO_STD).
lint-bsd:
	CGO_ENABLED=0 GOOS=freebsd GOFLAGS='$(GOFLAGS) $(FAKECGO_STD)' golangci-lint run ./...
	CGO_ENABLED=0 GOOS=netbsd golangci-lint run ./...

fmt:
	gofmt -w .

# The library and examples/ are separate modules; tidy both.
tidy:
	go mod tidy
	cd examples && go mod tidy

# Build the reference programs in examples/ (see each one's doc for how to
# run it). The binaries go to ./build.
examples:
	mkdir -p ./build
	cd examples && go build -o ../build/ ./...

clean:
	rm -rf ./build
	rm -rf ./coverage.out
