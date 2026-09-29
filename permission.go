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
	// script, with navigator.clipboard.readText or read, without the user
	// confirming each read. WebKitGTK allows a read only on a user gesture.
	// WKWebView never reads silently: it shows the system's Paste button,
	// and a read the user confirms there is the user's own paste, like
	// Command-V, whatever the view lists. So on macOS this permission changes
	// nothing, and an empty list does not stop a paste the user confirms.
	// Copying on a click needs no permission on any engine.
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

// originURL writes a security origin's parts as a URL whose origin is that
// one, for originOf: scheme://host, with the port when it is not 0 (0 means
// the scheme's default). An IPv6 host is bracketed, whether or not the engine
// gave it with brackets. An empty scheme gives "".
func originURL(scheme, host string, port int) string {
	if scheme == "" {
		return ""
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := scheme + "://" + host
	if port != 0 {
		u += ":" + strconv.Itoa(port)
	}
	return u + "/"
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
