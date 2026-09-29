//go:build !darwin && !windows && !linux && !freebsd && !netbsd

// Platforms without a clipboard backend report ErrUnsupported.

package clipboard

func copyText(string) error { return ErrUnsupported }

func paste() (string, error) { return "", ErrUnsupported }
