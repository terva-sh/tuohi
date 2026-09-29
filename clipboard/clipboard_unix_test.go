//go:build linux || freebsd || netbsd

package clipboard

import (
	"errors"
	"testing"
)

// TestNoApp checks that Copy and Paste report ErrNoApp in a program that has
// started no tuohi toolkit. This test binary does not link the root package,
// so nothing publishes one. The round trip through a running app is a GUI
// scenario in the root package (clipboard_unix_test.go there).
func TestNoApp(t *testing.T) {
	if err := Copy("x"); !errors.Is(err, ErrNoApp) {
		t.Errorf("Copy without an app = %v, want ErrNoApp", err)
	}
	if got, err := Paste(); !errors.Is(err, ErrNoApp) || got != "" {
		t.Errorf("Paste without an app = %q, %v, want \"\", ErrNoApp", got, err)
	}
}
