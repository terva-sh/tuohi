package updater

import (
	"context"
	"errors"
	"os"
	"os/exec"
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

// This fixture is compiled only by native macOS tests; no C/cgo code ships in
// the library. Ad-hoc signatures require no signing identity or credentials.
// It tests preservation of dyld-relative layout, independently of the separate
// production TeamIdentifier policy (which intentionally refuses ad-hoc apps).
func TestHelperBundlePreservesExecutableRelativeDylib(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("native fixture needs the macOS command line tools")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Desktop.app")
	macos := filepath.Join(bundle, "Contents", "MacOS")
	frameworks := filepath.Join(bundle, "Contents", "Frameworks")
	for _, p := range []string{macos, frameworks} {
		if err = os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	dep := filepath.Join(dir, "fixture.c")
	main := filepath.Join(dir, "main.c")
	if err = os.WriteFile(dep, []byte("int fixture(void) { return 7; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(main, []byte("#include <stdio.h>\nextern int fixture(void);\nint main(void) { if (fixture() != 7) return 1; puts(\"bundle dependency ready\"); return 0; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(command string, args ...string) {
		t.Helper()
		if out, e := exec.Command(command, args...).CombinedOutput(); e != nil {
			t.Fatalf("%s: %v\n%s", command, e, out)
		}
	}
	lib := filepath.Join(frameworks, "libfixture.dylib")
	executable := filepath.Join(macos, "Desktop")
	run(clang, "-dynamiclib", dep, "-install_name", "@executable_path/../Frameworks/libfixture.dylib", "-o", lib)
	run(clang, main, lib, "-o", executable)
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>test.app</string><key>CFBundleExecutable</key><string>Desktop</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	if err = os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/codesign", "--force", "--sign", "-", lib)
	run("/usr/bin/codesign", "--force", "--deep", "--sign", "-", bundle)
	run("/usr/bin/codesign", "--verify", "--deep", "--strict", bundle)
	copy := filepath.Join(dir, "helper.app")
	if err = cloneBundle(context.Background(), bundle, copy); err != nil {
		t.Fatal(err)
	}
	// The original layout cannot accidentally satisfy the copied executable.
	if err = os.Rename(bundle, filepath.Join(dir, "Original.app")); err != nil {
		t.Fatal(err)
	}
	run("/usr/bin/codesign", "--verify", "--deep", "--strict", copy)
	out, err := exec.Command(filepath.Join(copy, "Contents", "MacOS", "Desktop")).Output()
	if err != nil || string(out) != "bundle dependency ready\n" {
		t.Fatalf("copied helper lost dylib: %v %q", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := filepath.Join(dir, "cancelled.app")
	if err = cloneBundle(ctx, copy, cancelled); err == nil {
		t.Fatal("cancelled copy succeeded")
	}
	if _, err = os.Lstat(cancelled); !os.IsNotExist(err) {
		t.Fatal("partial helper bundle retained", err)
	}
	if err = cloneBundle(context.Background(), copy, cancelled); err != nil {
		t.Fatal("bundle clone cannot be retried", err)
	}
}
