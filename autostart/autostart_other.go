//go:build !linux && !freebsd && !netbsd && !darwin && !windows

package autostart

// newBackend returns nil: this platform has no autostart backend, so Enable
// and Disable report ErrUnsupported and the queries report nothing
// registered.
func newBackend() backend { return nil }
