//go:build linux || freebsd || netbsd

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newForTest returns an *Autostart whose XDG config home points at a temp
// dir, so tests never touch the real ~/.config/autostart. The registration
// executable is the test binary itself.
func newForTest(t *testing.T, id string) *Autostart {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return New(id)
}

func TestXDGRoundTrip(t *testing.T) {
	a := newForTest(t, "test-app")
	if a.Enabled() {
		t.Fatal("expected disabled before Enable")
	}
	if err := a.Enable("--hidden"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !a.Enabled() {
		t.Fatal("expected enabled after Enable")
	}
	if got := a.Backend(); got != backendXDGAutostart {
		t.Errorf("Backend = %q, want %q", got, backendXDGAutostart)
	}
	path := a.Path()
	if filepath.Base(path) != "test-app.desktop" {
		t.Errorf("Path = %q, want .../test-app.desktop", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registration path: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=" + filepath.Base(exe),
		"Hidden=false",
		"X-GNOME-Autostart-enabled=true",
		"--hidden",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in desktop file:\n%s", want, body)
		}
	}
	if err := a.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if a.Enabled() {
		t.Error("still enabled after Disable")
	}
}

func TestIdentifierNamesTheFile(t *testing.T) {
	// The id names the registration artefact verbatim.
	a := newForTest(t, "com.example.test")
	if err := a.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if filepath.Base(a.Path()) != "com.example.test.desktop" {
		t.Fatalf("Path = %q, want com.example.test.desktop", a.Path())
	}
}

func TestEnableOverwritesStaleIdentifier(t *testing.T) {
	// Re-enabling under a different id must remove the earlier registration,
	// keeping exactly one .desktop entry.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	if err := New("old-name").Enable(); err != nil {
		t.Fatalf("first Enable: %v", err)
	}
	if err := New("new-name").Enable(); err != nil {
		t.Fatalf("second Enable: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".config", "autostart"))
	if err != nil {
		t.Fatalf("read autostart dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "new-name.desktop" {
		t.Fatalf("autostart dir after re-enable = %v, want exactly [new-name.desktop]", entries)
	}
}

func TestQuoteExec(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/foo":          "/usr/bin/foo",
		"/path with spaces/foo": `"/path with spaces/foo"`,
		`/has"quote`:            `"/has\"quote"`,
		`/has\back`:             `"/has\\back"`,
	}
	for in, want := range cases {
		if got := quoteExec(in); got != want {
			t.Errorf("quoteExec(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDesktopExecPath(t *testing.T) {
	cases := map[string]string{
		"Exec=/usr/bin/foo\n":                        "/usr/bin/foo",
		"Exec=/usr/bin/foo --flag\n":                 "/usr/bin/foo",
		`Exec="/path with spaces/foo" --flag` + "\n": "/path with spaces/foo",
		"[Desktop Entry]\nExec=/x/y\n":               "/x/y",
		"NoExec=/x\n":                                "",
	}
	for in, want := range cases {
		if got := desktopExecPath(in); got != want {
			t.Errorf("desktopExecPath(%q) = %q, want %q", in, got, want)
		}
	}
}
