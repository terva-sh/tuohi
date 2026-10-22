package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleIdentityRefusesUnsignedOrWrongApplication(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Desktop.app")
	if err := os.MkdirAll(filepath.Join(dir, "Contents/MacOS"), 0755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>test.app</string><key>CFBundleExecutable</key><string>Desktop</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(dir, "Contents/Info.plist"), []byte(plist), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Contents/MacOS/Desktop"), []byte("not a signed Mach-O"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := bundleExecutable(context.Background(), dir, "other.app"); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong identity accepted", err)
	}
	if _, err := signatureTeam(context.Background(), dir); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsigned app accepted", err)
	}
}
