package tray

import "testing"

// TestBoundsIdle checks that Bounds reports zeros when no tray is active on
// every platform. It never touches the OS tray, so it runs headless.
func TestBoundsIdle(t *testing.T) {
	x, y, w, h := Bounds()
	if x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("Bounds() with no active tray = (%d,%d,%d,%d), want (0,0,0,0)", x, y, w, h)
	}
}

// TestSentinels pins the error contract of the declarative API.
func TestSentinels(t *testing.T) {
	if ErrUnsupported == nil || ErrAlreadyRunning == nil {
		t.Fatal("ErrUnsupported and ErrAlreadyRunning must be non-nil")
	}
	if ErrUnsupported == ErrAlreadyRunning {
		t.Fatal("ErrUnsupported and ErrAlreadyRunning must be distinct")
	}
}

// TestConfigSurface locks the declarative Config/Item field surface so the
// documented API cannot drift silently; compiling this test is the check.
func TestConfigSurface(t *testing.T) {
	cfg := Config{
		Icon:          []byte{1},
		DarkModeIcon:  []byte{2},
		TemplateIcon:  []byte{3},
		AppName:       "app",
		Title:         "title",
		Tooltip:       "tooltip",
		OnClick:       func() {},
		OnDoubleClick: func() {},
		OnRightClick:  func() {},
		Items: []Item{{
			Label:     "item",
			Icon:      []byte{4},
			Checkbox:  true,
			Checked:   true,
			Disabled:  false,
			Separator: false,
			Submenu:   []Item{{Label: "child", OnClick: func() {}}},
			OnClick:   func() {},
		}},
	}
	if cfg.AppName != "app" || cfg.Items[0].Submenu[0].Label != "child" {
		t.Fatal("config surface mismatch")
	}
}
