//go:build linux || freebsd || netbsd

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/terva-sh/tuohi/internal/desktopentry"
)

// The Unix autostart backend: a freedesktop.org autostart .desktop file in
// $XDG_CONFIG_HOME/autostart (defaulting to ~/.config/autostart). Desktop
// environments that honour the XDG autostart spec run every .desktop file
// found there at login.

// xdgAutostart implements backend with .desktop files.
type xdgAutostart struct {
	name string // display name for the Name= key: the executable's base name
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_linux.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newBackend returns the XDG autostart backend. The entry's display name is
// the executable's base name.
func newBackend() backend {
	name := ""
	if exe, err := os.Executable(); err == nil {
		name = filepath.Base(exe)
	}
	return &xdgAutostart{name: name}
}

func (a *xdgAutostart) enable(id string, args []string) error {
	// A newline inside the executable path or an argument would break the
	// .desktop line-based key=value format and could inject Desktop Entry
	// keys when the arguments are user-influenced.
	if err := validateDesktopExecToken(id); err != nil {
		return fmt.Errorf("autostart: identifier: %w", err)
	}
	for i, arg := range args {
		if err := validateDesktopExecToken(arg); err != nil {
			return fmt.Errorf("autostart: argument %d: %w", i, err)
		}
	}
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	if err := validateDesktopExecToken(exe); err != nil {
		return fmt.Errorf("autostart: executable path: %w", err)
	}
	dir, err := a.autostartDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("autostart: create autostart dir: %w", err)
	}
	path := filepath.Join(dir, id+".desktop")
	// Remove a stale .desktop file pointing at this binary under a different
	// identifier (e.g. from an earlier id), so Enable never leaves two
	// entries behind.
	if existing, ferr := a.findDesktopFile(dir); ferr == nil && existing != "" && existing != path {
		_ = os.Remove(existing)
	}
	body := buildDesktopEntry(a.name, exe, args)
	if err := writeFileAtomic(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("autostart: write desktop file %s: %w", path, err)
	}
	return nil
}

func (a *xdgAutostart) disable() error {
	dir, err := a.autostartDir()
	if err != nil {
		return err
	}
	path, err := a.findDesktopFile(dir)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("autostart: remove desktop file: %w", err)
	}
	return nil
}

func (a *xdgAutostart) status() (bool, string, string) {
	dir, err := a.autostartDir()
	if err != nil {
		return false, "", ""
	}
	path, err := a.findDesktopFile(dir)
	if err != nil || path == "" {
		return false, "", ""
	}
	return true, path, backendXDGAutostart
}

// autostartDir returns the XDG autostart directory ($XDG_CONFIG_HOME/
// autostart, defaulting to ~/.config/autostart).
func (a *xdgAutostart) autostartDir() (string, error) {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("autostart: %w", err)
		}
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "autostart"), nil
}

// findDesktopFile looks for a .desktop file in dir whose Exec= entry points
// at the current executable. Returns an empty path with no error when none
// matches; this survives identifier changes between Enable calls.
func (a *xdgAutostart) findDesktopFile(dir string) (string, error) {
	exe, err := resolvedExecutable()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("autostart: read autostart dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if desktopExecPath(string(data)) == exe {
			return full, nil
		}
	}
	return "", nil
}

// buildDesktopEntry renders the .desktop autostart file for the given display
// name, executable and login arguments.
func buildDesktopEntry(appName, exe string, args []string) string {
	if appName == "" {
		appName = filepath.Base(exe)
	}
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	fmt.Fprintf(&b, "Name=%s\n", desktopentry.String(escapeDesktopValue(appName)))
	b.WriteString("Exec=" + desktopentry.Exec(append([]string{exe}, args...)...) + "\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	b.WriteString("Hidden=false\n")
	b.WriteString("NoDisplay=true\n")
	b.WriteString("Terminal=false\n")
	return b.String()
}

// validateDesktopExecToken rejects control characters that would break the
// .desktop format or allow Desktop Entry key injection once interpolated into
// an Exec= line. Spaces and tabs are allowed (quoteExec double-quotes them);
// all other ASCII control characters, including CR/LF, are rejected.
func validateDesktopExecToken(s string) error {
	for _, r := range s {
		if r == '\t' || r == ' ' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("control character %U not allowed in Exec field", r)
		}
	}
	return nil
}

// escapeDesktopValue replaces characters that are not allowed raw in Desktop
// Entry values (newlines) and trims surrounding whitespace.
func escapeDesktopValue(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// desktopExecPath returns the program of a .desktop file's Exec key, the
// first argument as desktopentry.SplitExec reads it; empty when the file has
// no Exec key or its value does not parse.
func desktopExecPath(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		args, err := desktopentry.SplitExec(strings.TrimPrefix(line, "Exec="))
		if err != nil || len(args) == 0 {
			return ""
		}
		return args[0]
	}
	return ""
}
