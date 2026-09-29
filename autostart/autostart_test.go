package autostart

import (
	"strings"
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	good := []string{"com.example.app", "my-app_1.0", "x", strings.Repeat("a", 200)}
	for _, id := range good {
		if err := validateIdentifier(id); err != nil {
			t.Errorf("validateIdentifier(%q) = %v, want nil", id, err)
		}
	}
	bad := []string{"", "has space", "has/slash", "emoji😀", strings.Repeat("a", 201)}
	for _, id := range bad {
		if err := validateIdentifier(id); err == nil {
			t.Errorf("validateIdentifier(%q) = nil, want error", id)
		}
	}
}

// TestEnableRejectsBadID pins that Enable refuses an empty or invalid id
// before the backend writes anything: the id is used verbatim as a file name,
// registry value name or launchd label.
func TestEnableRejectsBadID(t *testing.T) {
	for _, id := range []string{"", "bad id", "../escape"} {
		a := &Autostart{id: id, impl: failingBackend{t}}
		if err := a.Enable(); err == nil {
			t.Errorf("Enable with id %q = nil, want error", id)
		}
	}
}

// failingBackend fails the test if Enable reaches it.
type failingBackend struct{ t *testing.T }

func (b failingBackend) enable(id string, _ []string) error {
	b.t.Errorf("backend enable reached with id %q", id)
	return nil
}
func (failingBackend) disable() error                 { return nil }
func (failingBackend) status() (bool, string, string) { return false, "", "" }

// TestNilSafety: a nil *Autostart, or one on a platform without a backend,
// degrades to "not enabled" / "not registered" instead of panicking;
// Enable/Disable report the platform as unsupported.
func TestNilSafety(t *testing.T) {
	for name, a := range map[string]*Autostart{"nil": nil, "no backend": {id: "x"}} {
		if a.Enabled() {
			t.Errorf("%s: Enabled = true", name)
		}
		if a.Path() != "" || a.Backend() != "" {
			t.Errorf("%s: Path = %q, Backend = %q, want empty", name, a.Path(), a.Backend())
		}
		if err := a.Enable("--flag"); err != ErrUnsupported {
			t.Errorf("%s: Enable = %v, want ErrUnsupported", name, err)
		}
		if err := a.Disable(); err != ErrUnsupported {
			t.Errorf("%s: Disable = %v, want ErrUnsupported", name, err)
		}
	}
}
