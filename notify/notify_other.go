//go:build !darwin && !windows && !linux && !freebsd && !netbsd

// Platforms without a notification backend always report ErrUnsupported.
package notify

func show(_, _, _ string, _ Options) error { return ErrUnsupported }

// beep exists so Beep compiles everywhere; it reports ErrUnsupported.
func beep(_ float64, _ int) error { return ErrUnsupported }

// alertSound is never reached on these platforms: Alert's show call fails
// with ErrUnsupported first.
func alertSound() error { return nil }
