//go:build !darwin && !windows && !linux && !freebsd && !netbsd

// Platforms without a tray backend: macOS, Windows and Linux are the
// supported trio; anything else reports ErrUnsupported from Set and Run.
package tray

func set(_ Config) error { return ErrUnsupported }

func remove() {}

func run(_ Config) error { return ErrUnsupported }

func stop() {}

func bounds() (x, y, w, h int) { return 0, 0, 0, 0 }
