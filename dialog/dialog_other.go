//go:build !darwin && !windows && !linux && !freebsd && !netbsd

// Stub for platforms without a native panel implementation (e.g. *BSD, js).
// Every panel reports cancellation so callers degrade gracefully, keeping the
// module building for every GOOS.

package dialog

func open(_ Options) string { return "" }

func openMultiple(_ Options) []string { return nil }

func save(_ Options) string { return "" }

func pickDirectory(_ Options) string { return "" }
