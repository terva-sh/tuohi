package autostart

import (
	"errors"
	"fmt"
	"strings"

	winregistry "golang.org/x/sys/windows/registry"
)

// The Windows autostart backend: a string value under the per-user Run
// registry key
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run, whose command line
// starts with the registered executable. Windows runs every value found
// there at login.

// autostartRunSubKey is the per-user registry key Windows executes at login.
const autostartRunSubKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// registryAutostart implements backend with the HKCU Run key.
type registryAutostart struct {
	// subKey is the registry key read/written (the Run key in production;
	// overridable by tests).
	subKey string
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_windows.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newBackend returns the registry autostart backend.
func newBackend() backend {
	return &registryAutostart{subKey: autostartRunSubKey}
}

func (a *registryAutostart) enable(id string, args []string) error {
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	cmd := quoteWindowsArg(exe)
	for _, arg := range args {
		cmd += " " + quoteWindowsArg(arg)
	}
	key, _, err := winregistry.CreateKey(winregistry.CURRENT_USER, a.subKey, winregistry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	// Remove a stale entry pointing at this binary under a different value
	// name, so Enable never leaves two autostart entries behind.
	if existing, _, ferr := a.find(); ferr == nil && existing != "" && existing != id {
		_ = key.DeleteValue(existing)
	}
	if err := key.SetStringValue(id, cmd); err != nil {
		return fmt.Errorf("autostart: write registry value: %w", err)
	}
	return nil
}

func (a *registryAutostart) disable() error {
	id, _, err := a.find()
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	key, err := winregistry.OpenKey(winregistry.CURRENT_USER, a.subKey, winregistry.SET_VALUE)
	if err != nil {
		if errors.Is(err, winregistry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.DeleteValue(id); err != nil && !errors.Is(err, winregistry.ErrNotExist) {
		return fmt.Errorf("autostart: delete registry value: %w", err)
	}
	return nil
}

func (a *registryAutostart) status() (bool, string, string) {
	id, _, err := a.find()
	if err != nil || id == "" {
		return false, "", ""
	}
	return true, `HKCU\` + a.subKey + `\` + id, backendRegistryRun
}

// find returns the value name and command of the Run entry whose first token
// is the current executable. Empty name means not registered.
func (a *registryAutostart) find() (string, string, error) {
	exe, err := resolvedExecutable()
	if err != nil {
		return "", "", err
	}
	key, err := winregistry.OpenKey(winregistry.CURRENT_USER, a.subKey, winregistry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, winregistry.ErrNotExist) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return "", "", fmt.Errorf("autostart: list registry values: %w", err)
	}
	exeLower := strings.ToLower(exe)
	for _, name := range names {
		val, _, err := key.GetStringValue(name)
		if err != nil {
			continue
		}
		if strings.EqualFold(parseWindowsCommandExe(val), exeLower) {
			return name, val, nil
		}
	}
	return "", "", nil
}

// parseWindowsCommandExe returns the first token of a Windows command line,
// honouring surrounding double quotes for paths with spaces, lower-cased for
// case-insensitive comparison.
func parseWindowsCommandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if cmd[0] == '"' {
		end := strings.IndexByte(cmd[1:], '"')
		if end < 0 {
			return strings.ToLower(cmd[1:])
		}
		return strings.ToLower(cmd[1 : 1+end])
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		return strings.ToLower(cmd[:i])
	}
	return strings.ToLower(cmd)
}

// quoteWindowsArg double-quotes an argument when it contains whitespace or
// quotes and escapes embedded quotes; backslashes preceding a quote are
// doubled per CommandLineToArgvW rules.
func quoteWindowsArg(s string) string {
	if s != "" && !strings.ContainsAny(s, `" `+"\t") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			backslashes++
		case '"':
			// Per CommandLineToArgvW a literal quote preceded by N
			// backslashes must be encoded as 2N+1 backslashes + the quote.
			for j := 0; j < 2*backslashes; j++ {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
			b.WriteByte('"')
			backslashes = 0
		default:
			for j := 0; j < backslashes; j++ {
				b.WriteByte('\\')
			}
			backslashes = 0
			b.WriteByte(c)
		}
	}
	for j := 0; j < backslashes; j++ {
		b.WriteByte('\\')
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}
