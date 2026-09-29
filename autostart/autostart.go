// Package autostart registers an application to launch when the user logs
// in, cgo-free.
//
// New returns the controller for one application, named by an id:
//
//	a := autostart.New("com.example.app")
//	if err := a.Enable("--hidden"); err != nil {
//		log.Print(err)
//	}
//
// The mechanism is per platform: a .desktop file under the XDG autostart
// directory on Linux, FreeBSD and NetBSD; an HKCU Run registry value on
// Windows; a launchd LaunchAgent plist on macOS, or SMAppService for a
// bundled .app on macOS 13 and later. Every registration points at the
// running executable, resolved through symlinks, and takes effect at the next
// login, not immediately. On a platform without a backend every method
// reports ErrUnsupported or nothing registered.
//
// The id is the name the registration is stored under: the .desktop file
// name, the registry value name, or the LaunchAgent label and plist name. It
// must be 1 to 200 characters from A-Za-z0-9._-, so a reverse-DNS name such
// as "com.example.app" works everywhere.
//
// The implementation is derived from Wails v3's autostart support; see NOTICE.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrUnsupported is returned by Enable and Disable on a platform with no
// autostart backend.
var ErrUnsupported = errors.New("autostart: not supported on this platform")

// Backend names, reported by Autostart.Backend for an existing registration
// (empty when nothing is registered).
const (
	backendSMAppService = "smappservice"  // macOS 13+, bundled .app
	backendLaunchAgent  = "launchagent"   // macOS ~/Library/LaunchAgents plist
	backendRegistryRun  = "registry-run"  // Windows HKCU …\Run value
	backendXDGAutostart = "xdg-autostart" // freedesktop .desktop autostart
)

// Autostart controls whether the application starts at user login. Get one
// from New.
//
// A registration points at the running executable and takes effect on the
// next login, not immediately. Re-enabling overwrites the registration; each
// platform keeps at most one entry per executable, so a previous registration
// under a different id is replaced, not duplicated.
//
// Enabled, Path and Backend report the CURRENT registration: they scan the
// platform's registration store for an entry whose command is the running
// executable, so they work regardless of the id used at Enable time.
type Autostart struct {
	id   string
	impl backend
}

// backend is one platform's registration mechanism. Implementations live in
// the per-OS files.
type backend interface {
	// enable registers the running executable under the given identifier
	// with the given login arguments.
	enable(id string, args []string) error
	// disable removes the registration for the running executable, if any.
	disable() error
	// status reports whether a registration for the running executable
	// exists, the path of its artefact (registry sub-key path on Windows,
	// the bundle identifier for SMAppService) and the backend name.
	status() (enabled bool, path, backend string)
}

// New returns the autostart controller for the application identified by id,
// the name a registration is stored under. id must be 1-200 characters from
// A-Za-z0-9._- ; Enable returns an error for any other id.
func New(id string) *Autostart {
	return &Autostart{id: id, impl: newBackend()}
}

// Enabled reports whether a registration for the running executable exists.
// It does not verify that a registered entry still points at the running
// binary; Disable and Enable always reconcile that themselves.
func (a *Autostart) Enabled() bool {
	if a == nil || a.impl == nil {
		return false
	}
	enabled, _, _ := a.impl.status()
	return enabled
}

// Enable registers the application to launch at login with the given command
// line arguments (appended after the executable path). It is safe to call
// repeatedly: an existing registration is overwritten, and a stale entry
// pointing at this executable under a different id is removed first. An id
// that is empty, longer than 200 characters, or holds a character outside
// A-Za-z0-9._- is rejected before anything is written.
func (a *Autostart) Enable(args ...string) error {
	if a == nil || a.impl == nil {
		return ErrUnsupported
	}
	if err := validateIdentifier(a.id); err != nil {
		return err
	}
	return a.impl.enable(a.id, args)
}

// Disable removes the autostart registration for the running executable. It
// is a no-op (nil error) when nothing is registered.
func (a *Autostart) Disable() error {
	if a == nil || a.impl == nil {
		return ErrUnsupported
	}
	return a.impl.disable()
}

// Path returns the path of the registration artefact for the current
// registration: the .desktop file path on Linux, the registry sub-key path
// (HKCU\…\Run\<id>) on Windows, the LaunchAgent plist path on macOS, or the
// bundle identifier for an SMAppService registration. Empty when nothing is
// registered.
func (a *Autostart) Path() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, path, _ := a.impl.status()
	return path
}

// Backend returns the name of the mechanism the current registration uses:
// "xdg-autostart", "registry-run", "launchagent" or "smappservice". Empty
// when nothing is registered.
func (a *Autostart) Backend() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, _, backend := a.impl.status()
	return backend
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// validateIdentifier rejects identifiers that would be unsafe as a filename,
// registry value name or launchd Label, and the empty identifier.
func validateIdentifier(id string) error {
	if id == "" {
		return errors.New("autostart: empty id")
	}
	if len(id) > 200 {
		return fmt.Errorf("autostart: identifier too long (max 200): %q", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("autostart: identifier %q contains invalid character %q (allowed: A-Za-z0-9._-)", id, r)
		}
	}
	return nil
}

// resolvedExecutable returns os.Executable() after resolving symlinks, so
// registrations don't break when the binary is installed through a symlink
// farm (Homebrew, Scoop). Falls back to the unresolved path when symlink
// resolution fails.
func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("autostart: get executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}
