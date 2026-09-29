package tuohi

import (
	"log"
	"strconv"
	"strings"
)

// Permission names something a page may use only when the application allows
// it for the view, in View.Permissions. Everything else a page can ask the web
// engine for, such as geolocation or notifications, is always denied.
type Permission int

const (
	// PermissionCamera lets a trusted page open the camera with
	// navigator.mediaDevices.getUserMedia.
	PermissionCamera Permission = iota + 1

	// PermissionMicrophone lets a trusted page open the microphone with
	// navigator.mediaDevices.getUserMedia.
	PermissionMicrophone

	// PermissionClipboard lets a trusted page read the clipboard from
	// script, with navigator.clipboard.readText or read. WebKitGTK and
	// WKWebView allow a read only on a user gesture, and WKWebView then
	// shows the system's Paste button whatever the view lists, so on macOS
	// this changes nothing. Copying on a click needs no permission on any
	// engine, and no engine lets a page paste without a user gesture.
	PermissionClipboard
)

// String returns the permission's name.
func (p Permission) String() string {
	switch p {
	case PermissionCamera:
		return "camera"
	case PermissionMicrophone:
		return "microphone"
	case PermissionClipboard:
		return "clipboard"
	}
	return "permission(" + strconv.Itoa(int(p)) + ")"
}

// permissionDecided, when set, is told of every decision permits makes. Only
// tests set it, before any view exists.
var permissionDecided func(requester string, perms []Permission, granted bool)

// permissionSet turns View.Permissions into the set viewCore.permits reads.
func permissionSet(perms []Permission) map[Permission]bool {
	set := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return set
}

// permits decides a page's request for perms, all of which it needs at once
// (a camera-and-microphone capture asks for both), made by a page at
// requester: granted only when the application listed every one for the view
// and requester's origin is one the view trusts. Every engine's permission
// handler asks it, so the rule is the same everywhere. A denial is logged,
// because a page only sees NotAllowedError.
func (c *viewCore) permits(requester string, perms ...Permission) bool {
	o := originOf(requester)
	c.mu.Lock()
	granted := o != "" && c.origins[o] && len(perms) > 0
	for _, p := range perms {
		granted = granted && c.permissions[p]
	}
	c.mu.Unlock()
	if permissionDecided != nil {
		permissionDecided(requester, perms, granted)
	}
	if !granted {
		names := make([]string, len(perms))
		for i, p := range perms {
			names[i] = p.String()
		}
		log.Printf("tuohi: %s for %q denied: not in View.Permissions, or not a trusted origin", strings.Join(names, "+"), requester)
	}
	return granted
}
