//go:build linux || freebsd || netbsd

package tuohi

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKitLinuxPlatform(t *testing.T) {}

// On Linux the backend shells out to xdg-open. A fake xdg-open on PATH records
// its argument, so the real exec path (and the URL/folder it is given) is
// exercised on CI without launching a browser or file manager.
func TestLinuxInvokesXdgOpen(t *testing.T) {
	// Open/Reveal go through App.begin, which loads the GTK/WebKitGTK stack;
	// skip where it is not installed (headless lint CI) rather than failing.
	if err := ensureInit(); err != nil {
		t.Skipf("GTK/WebKitGTK unavailable (init error: %v)", err)
	}
	dir := t.TempDir()
	argFile := filepath.Join(dir, "arg")
	fake := filepath.Join(dir, "xdg-open")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + argFile + "\n"
	err := os.WriteFile(fake, []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	readArg := func() string {
		b, err := os.ReadFile(argFile)
		if err != nil {
			t.Fatalf("read recorded arg: %v", err)
		}
		return string(b)
	}

	// Open hands the URL to xdg-open unchanged.
	err = testApp().Open("https://example.com/x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := readArg()
	if got != "https://example.com/x" {
		t.Fatalf("xdg-open arg = %q, want the URL", got)
	}

	// Reveal opens the file's containing folder.
	sub := filepath.Join(dir, "sub")
	err = os.MkdirAll(sub, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "file.txt")
	err = os.WriteFile(file, []byte("x"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	err = testApp().Reveal(file)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	got = readArg()
	if got != sub {
		t.Fatalf("xdg-open arg = %q, want the folder %q", got, sub)
	}
}

// TestSetAppIconRejectsGarbage pins the decode/validation boundary of the
// Linux application icon: garbage is an error before anything touches GTK, so
// the check is deterministic even on headless CI without an engine.
func TestSetAppIconRejectsGarbage(t *testing.T) {
	if err := setAppIcon(nil, "test", false); err == nil {
		t.Fatal("setAppIcon(nil) must fail (empty icon)")
	}
	if err := setAppIcon([]byte("this is not a png"), "test", false); err == nil {
		t.Fatal("setAppIcon with non-PNG bytes must fail")
	}
}

// TestDesktopID pins the desktop-id sanitization rules.
func TestDesktopID(t *testing.T) {
	for in, want := range map[string]string{
		"appkit demo":  "appkit-demo",
		"AppKit Demo!": "appkit-demo",
		"  My  App  ":  "my-app",
		"123app":       "123app",
		"a++b":         "a-b",
		"---":          "app",
		"":             "", // falls back to the executable name; not deterministic in tests
	} {
		if want == "" {
			continue
		}
		if got := desktopID(in); got != want {
			t.Errorf("desktopID(%q) = %q, want %q", in, got, want)
		}
	}
	if got := desktopID(""); got == "" {
		t.Error("desktopID(\"\") must fall back to the executable name or 'app'")
	}
}

// TestInstallWaylandIdentity verifies the per-user .desktop + themed-icon
// install against a temporary XDG_DATA_HOME: files land in the standard
// locations, are idempotent on re-run, and the returned id matches the file
// name stem the compositor resolves the window app_id against.
func TestInstallWaylandIdentity(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	// Simulate an older install that left the icon in a different hicolor
	// size directory (e.g. the previous 256px demo glyph). Icon loaders pick
	// the closest size, so such a stale copy would shadow the fresh install.
	stale := filepath.Join(data, "icons", "hicolor", "256x256", "apps", "appkit-demo.png")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old icon"), 0o644); err != nil {
		t.Fatal(err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	id := installWaylandIdentity("AppKit Demo", img)
	if id != "appkit-demo" {
		t.Fatalf("install returned id %q, want appkit-demo", id)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale icon copy at %s must be removed after install (err=%v)", stale, err)
	}
	icon := filepath.Join(data, "icons", "hicolor", "8x8", "apps", id+".png")
	desktop := filepath.Join(data, "applications", id+".desktop")
	for _, p := range []string{icon, desktop} {
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Fatalf("expected %s to exist and be non-empty (err %v)", p, err)
		}
	}
	got, err := os.ReadFile(desktop)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Icon=appkit-demo") {
		t.Errorf(".desktop content missing Icon key:\n%s", got)
	}

	// Idempotent: same id on re-run, files not rewritten (mtime unchanged).
	before, _ := os.Stat(icon)
	if id2 := installWaylandIdentity("AppKit Demo", img); id2 != id {
		t.Fatalf("second install returned %q, want %q", id2, id)
	}
	after, _ := os.Stat(icon)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("identical icon file was rewritten; install must be idempotent")
	}

	// Non-square icons are skipped (no runtime scaling).
	wide := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	if id3 := installWaylandIdentity("Wide", wide); id3 != "" {
		t.Errorf("non-square icon install returned %q, want \"\"", id3)
	}
}

// TestEmbeddedIconIsValidPNG pins the fallback icon asset: app.png is embedded
// as _icon and used whenever the consumer leaves App.Icon unset, so it must
// always be a decodable, square PNG.
func TestEmbeddedIconIsValidPNG(t *testing.T) {
	if len(_icon) == 0 {
		t.Fatal("embedded app.png must be non-empty")
	}
	img, err := png.Decode(bytes.NewReader(_icon))
	if err != nil {
		t.Fatalf("embedded app.png does not decode as PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() != b.Dx() {
		t.Fatalf("embedded app.png must be a square PNG, got %dx%d", b.Dx(), b.Dy())
	}
}

// TestInstallWaylandIdentitySizes pins the multi-size install: a 512px source
// must land at every standard hicolor size (22..512), because icon loaders
// scan those canonical directories and would never see a 512-only install.
func TestInstallWaylandIdentitySizes(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 0xE8, 0x7D, 0x1E, 0xFF
	}
	if got := targetIconSizes(512); len(got) != 7 || got[0] != 512 || got[len(got)-1] != 22 {
		t.Fatalf("targetIconSizes(512) = %v, want [512 256 128 64 48 32 22]", got)
	}
	id := installWaylandIdentity("Big App", img)
	if id != "big-app" {
		t.Fatalf("install returned id %q, want big-app", id)
	}
	for _, s := range []int{512, 256, 128, 64, 48, 32, 22} {
		p := filepath.Join(data, "icons", "hicolor", fmt.Sprintf("%dx%d", s, s), "apps", id+".png")
		st, err := os.Stat(p)
		if err != nil || st.Size() == 0 {
			t.Errorf("expected %s to exist and be non-empty (err=%v)", p, err)
		}
	}
	// A stale non-standard leftover (e.g. an old "96x96" copy) is removed.
	stale := filepath.Join(data, "icons", "hicolor", "96x96", "apps", id+".png")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err == nil {
		if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if id2 := installWaylandIdentity("Big App", img); id2 != id {
		t.Fatalf("second install returned %q", id2)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale icon copy at %s must be removed (err=%v)", stale, err)
	}
}

// TestWaylandIdentityOptIn checks that a GTK3 app on Wayland writes nothing
// under the user's data directory unless it set App.DesktopEntry, and that
// setting it is what writes the entry.
func TestWaylandIdentityOptIn(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("PATH", t.TempDir()) // no kbuildsycoca to start
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))

	if id := waylandIdentity(false, "Opt App", img); id != "" {
		t.Fatalf("without DesktopEntry: id %q, want \"\"", id)
	}
	if n := countFiles(t, data); n != 0 {
		t.Fatalf("without DesktopEntry: %d files written under XDG_DATA_HOME, want 0", n)
	}

	id := waylandIdentity(true, "Opt App", img)
	if id != "opt-app" {
		t.Fatalf("with DesktopEntry: id %q, want opt-app", id)
	}
	entry, err := os.ReadFile(filepath.Join(data, "applications", "opt-app.desktop"))
	if err != nil {
		t.Fatalf("with DesktopEntry: no entry written: %v", err)
	}
	if !strings.Contains(string(entry), generatedKey) {
		t.Errorf("written entry lacks %s:\n%s", generatedKey, entry)
	}

	// Outside Wayland it writes nothing even when asked.
	t.Setenv("WAYLAND_DISPLAY", "")
	other := t.TempDir()
	t.Setenv("XDG_DATA_HOME", other)
	if id := waylandIdentity(true, "Opt App", img); id != "" || countFiles(t, other) != 0 {
		t.Fatalf("outside Wayland: id %q and %d files, want none", id, countFiles(t, other))
	}
}

// TestWaylandIdentityLeavesForeignEntry checks that an entry of the same name
// that tuohi did not write is neither rewritten nor given icons, while its id
// is still returned so the window matches it.
func TestWaylandIdentityLeavesForeignEntry(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(data, "applications", "packaged.desktop")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("[Desktop Entry]\nType=Application\nName=Packaged\nExec=/usr/bin/packaged\nIcon=packaged\n")
	if err := os.WriteFile(path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}

	if id := installWaylandIdentity("Packaged", image.NewNRGBA(image.Rect(0, 0, 8, 8))); id != "packaged" {
		t.Fatalf("id %q, want packaged", id)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, foreign) {
		t.Fatalf("foreign entry was rewritten:\n%s", got)
	}
	if n := countFiles(t, filepath.Join(data, "icons")); n != 0 {
		t.Fatalf("%d icon files written beside a foreign entry, want 0", n)
	}
}

// countFiles counts the regular files under dir; a missing dir holds none.
func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
