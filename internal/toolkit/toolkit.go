// Package toolkit hands what the root package knows about the native toolkit
// it has started to the desktop-service packages, which must not import the
// root. The root publishes a Toolkit with Set once its first window exists; a
// service reads it with Get. Get returns nil in a program that has not opened
// a window, and in one that does not link the root at all, so a service can
// tell that no toolkit is there to reach.
package toolkit

import "sync/atomic"

// Toolkit describes the toolkit the root package has started.
type Toolkit struct {
	// Call runs f on the UI thread and waits for it to return. It runs f in
	// place when the caller is on the UI thread already. Elsewhere it returns
	// an error, and f never runs, when no loop is running to serve it.
	Call func(f func()) error

	// GTK4 reports that the Unix engine loaded GTK 4 rather than GTK 3.
	GTK4 bool
	// GTK and GLib are the handles of the libgtk and libglib the Unix engine
	// loaded, for resolving more of their symbols with purego.Dlsym. They are
	// zero on other platforms.
	GTK, GLib uintptr
}

var current atomic.Pointer[Toolkit]

// Set publishes t for the services to read. The root calls it once its
// toolkit has started.
func Set(t *Toolkit) { current.Store(t) }

// Get returns the Toolkit the root published, or nil when none is published.
func Get() *Toolkit { return current.Load() }
