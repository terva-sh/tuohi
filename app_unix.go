//go:build linux || freebsd || netbsd

// Unix app-scope backends (Linux, FreeBSD, NetBSD): Open/Reveal via
// xdg-open, and the runtime application icon (App.Icon), installed into the
// GTK stack by lib_unix.go.

package tuohi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/terva-sh/tuohi/internal/desktopentry"
)

// setAppIcon installs the application icon (App.Icon, PNG bytes) when the
// app scope opens - before the first window exists (App.Show runs start
// before newView). The PNG is decoded here; installing it into the running
// GTK stack happens in lib_unix.go:
//
//   - GTK3: the pixels become a GdkPixbuf that is set as the default window
//     icon (gtk_window_set_default_icon) and handed to each owned window at
//     its first show (gtk_window_set_icon), so every window carries it.
//   - GTK4: the pixbuf window-icon APIs were removed, so the pixels become a
//     GdkTexture list (gdk_toplevel_set_icon_list) applied to each toplevel
//     surface at its first show (webview.applySurfaceIcon).
//
// The icon is best-effort like everywhere else. X11 window managers show the
// runtime icon directly (the pixbuf above becomes the _NET_WM_ICON). Wayland
// has no per-window icon protocol, so the taskbar/switcher icon comes from
// the .desktop entry the compositor matches to the window's app_id: GTK
// 4.20+ can push pixels through the xdg-toplevel-icon protocol (GTK4 path),
// but GTK3 cannot, so on a Wayland session, when the app asks with
// App.DesktopEntry, the GTK3 path additionally installs a per-user .desktop
// entry and a themed icon keyed to the app's name (installWaylandIdentity)
// and advertises the matching program name - that is the only channel a GTK3
// window has on Wayland. Failure at any step is reported but never fatal
// (App.start ignores the error).
func setAppIcon(pngData []byte, name string, desktopEntry bool) error {
	if len(pngData) == 0 {
		return errors.New("tuohi: the application icon is empty")
	}
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("tuohi: application icon: %w", err)
	}
	b := src.Bounds()
	// Normalize to straight-alpha NRGBA, the byte layout both the GdkPixbuf
	// (GTK3) and the premultiplying texture path (GTK4) consume. NRGBA.Pix
	// has no row padding, so stride is exactly 4*width.
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	if gtk4 {
		return gtk4InstallAppIcon(img.Pix, b.Dx(), b.Dy())
	}
	// GTK3 on Wayland: give the compositor a desktop entry to derive the icon
	// from, and make this process's program name match its id so the window's
	// app_id resolves to that entry. g_set_prgname must run before gtk_init
	// (the app_id is captured when the first window's surface is created) -
	// gtk_init happens later, in windowInit/newView.
	if id := waylandIdentity(desktopEntry, name, img); id != "" && gSetPrgname != nil {
		gSetPrgname(id)
	}
	return gtk3InstallAppIcon(img.Pix, b.Dx(), b.Dy())
}

// --- Wayland application identity (GTK3) -----------------------------------

// waylandIdentity installs the GTK3 Wayland identity when the app asked for
// it with App.DesktopEntry and the session is Wayland, and returns its
// desktop id; otherwise it writes nothing and returns "".
func waylandIdentity(desktopEntry bool, name string, img *image.NRGBA) string {
	if !desktopEntry || os.Getenv("WAYLAND_DISPLAY") == "" {
		return ""
	}
	return installWaylandIdentity(name, img)
}

// installWaylandIdentity gives the compositor a desktop entry to derive the
// application icon from. It writes a per-user .desktop file and a themed PNG
// named after the app into the XDG data home (~/.local/share by default) -
// the standard location Wayland compositors (KDE Plasma, GNOME) consult when
// they resolve a window's app_id to an icon. It returns the desktop id (the
// .desktop file name stem, "" when nothing could be installed).
//
// The id doubles as the program name the caller advertises with
// g_set_prgname, so window app_id == <id>.desktop stem and the compositor
// finds the entry. The icon is installed at every standard hicolor size the
// source allows (see iconSizes), downscaled with a box filter - icon loaders
// resolve a name to the closest size they scan, so a single non-standard copy
// (e.g. a 512px source in hicolor/512x512) can be invisible to them. Writes
// are idempotent - identical existing files are left untouched, so repeated
// launches do not churn the icon-theme or desktop-file caches.
func installWaylandIdentity(appName string, img *image.NRGBA) string {
	id := desktopID(appName)
	if id == "" {
		return ""
	}
	dataHome, err := xdgDataHome()
	if err != nil {
		return ""
	}
	// Only square sources are installed.
	sz := img.Bounds().Dx()
	if img.Bounds().Dy() != sz || sz <= 0 {
		return ""
	}
	sizes := targetIconSizes(sz)
	if len(sizes) == 0 {
		return ""
	}
	// An entry of this id that tuohi did not write belongs to someone else,
	// such as a package that installs the application properly. Leave it and
	// its icon alone: advertising the id is enough for the compositor to use
	// it. That covers another user entry with the id, in a subdirectory of
	// applications, and a system entry, which a user entry of the same id
	// would shadow. An entry tuohi wrote before the other one arrived is
	// removed, with its icons, so the other one is the entry the desktop
	// uses.
	desktopPath := filepath.Join(dataHome, "applications", id+".desktop")
	old, err := os.ReadFile(desktopPath)
	ours := err == nil && isGenerated(old)
	if err == nil && !ours {
		return id
	}
	if hasEntry(dataHome, id, desktopPath) || systemEntry(id) {
		if ours {
			_ = os.Remove(desktopPath)
			removeOtherIconSizes(dataHome, id, nil)
			refreshDesktopDatabase()
		}
		return id
	}
	// Drop copies an older install left in hicolor size directories outside
	// the target set (e.g. the old 256px demo glyph): loaders pick the closest
	// available size, so a stale copy would keep shadowing the new icon.
	removeOtherIconSizes(dataHome, id, sizes)
	changed := false
	for _, s := range sizes {
		out := img
		if s != sz {
			out = boxDownscale(img, s)
		}
		pngBytes := encodeAppIconPNG(out)
		if len(pngBytes) == 0 {
			return ""
		}
		iconPath := filepath.Join(dataHome, "icons", "hicolor",
			fmt.Sprintf("%dx%d", s, s), "apps", id+".png")
		wrote, err := writeFileIfChanged(iconPath, pngBytes)
		if err != nil {
			return ""
		}
		changed = changed || wrote
	}
	exe, _ := os.Executable()
	displayName := strings.TrimSpace(appName)
	if displayName == "" {
		displayName = id
	}
	// The desktop file only needs to exist for icon resolution (the name, the
	// Icon key and the matching file name are what the compositor reads), so
	// Exec pointing at the current binary is a best-effort convenience.
	desktop := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=" + desktopentry.String(strings.ReplaceAll(displayName, "\n", " ")) + "\n" +
		"Icon=" + id + "\n" +
		"Exec=" + desktopentry.Exec(exe) + "\n" +
		"Terminal=false\n" +
		"Hidden=true\n" +
		"NoDisplay=true\n" +
		generatedKey + "\n"
	wroteDesktop, err := writeFileIfChanged(desktopPath, []byte(desktop))
	if err != nil {
		return ""
	}
	// KDE keeps its .desktop database (ksycoca) cached: a desktop entry that
	// appears or changes while Plasma runs is not seen until the database is
	// rebuilt, so the window's app_id would not resolve to the new entry (and
	// the taskbar keeps whatever icon it cached before). Kick a rebuild in the
	// background whenever this install actually changed something on disk.
	if changed || wroteDesktop {
		refreshDesktopDatabase()
	}
	return id
}

// refreshDesktopDatabase rebuilds KDE's desktop-file database (ksycoca) in the
// background, best-effort. It is a no-op when no ksycoca tool exists (GNOME
// and other desktops resolve .desktop files live and need no rebuild). The
// rebuild itself is asynchronous; the icon-theme half of the cache is
// invalidated by the directory changes themselves, which KIconLoader watches.
func refreshDesktopDatabase() {
	for _, tool := range []string{"kbuildsycoca6", "kbuildsycoca5", "kbuildsycoca"} {
		bin, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		// Run in the background: the app must not block on (or fail because
		// of) the desktop's cache rebuild. Wait reaps it when it exits, so it
		// does not linger as a zombie until the app does.
		cmd := exec.Command(bin, "--noincremental")
		if cmd.Start() == nil {
			go func() { _ = cmd.Wait() }()
		}
		return
	}
}

// desktopID turns an application name into a desktop-id (and icon-name) stem:
// ASCII lowercase letters, digits and '-' only - anything else collapses to
// '-'. An empty name falls back to the executable's base name, then to "app".
func desktopID(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	if id == "" {
		if exe, err := os.Executable(); err == nil {
			id = strings.ToLower(filepath.Base(exe))
		}
	}
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = r == '-'
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	return out
}

// xdgDataHome returns the per-user data directory (XDG_DATA_HOME, or
// ~/.local/share) where the desktop entry and themed icon are installed.
func xdgDataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// xdgDataDirs returns the system data directories (XDG_DATA_DIRS, or
// /usr/local/share and /usr/share), where packages install desktop entries.
// Relative entries are ignored, as the Base Directory Specification says.
func xdgDataDirs() []string {
	v := os.Getenv("XDG_DATA_DIRS")
	if v == "" {
		v = "/usr/local/share:/usr/share"
	}
	var dirs []string
	for _, d := range filepath.SplitList(v) {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// systemEntry reports whether a system data directory holds a desktop entry
// with this id.
func systemEntry(id string) bool {
	for _, d := range xdgDataDirs() {
		if hasEntry(d, id, "") {
			return true
		}
	}
	return false
}

// hasEntry reports whether dataDir's applications tree holds a desktop entry
// with this id, other than the file at skip. The Desktop Entry Specification
// forms an id from the file's path below applications, with each separator
// turned into a hyphen, so applications/foo/bar.desktop has the id foo-bar
// and shadows or is shadowed like applications/foo-bar.desktop.
func hasEntry(dataDir, id, skip string) bool {
	root := filepath.Join(dataDir, "applications")
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == skip {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		stem, ok := strings.CutSuffix(filepath.ToSlash(rel), ".desktop")
		if ok && strings.ReplaceAll(stem, "/", "-") == id {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// encodeAppIconPNG re-encodes the normalized icon as PNG bytes.
func encodeAppIconPNG(img *image.NRGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// writeFileIfChanged writes data to path, creating parent directories, unless
// an identical file already exists. It reports whether it actually wrote.
func writeFileIfChanged(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// iconSizes are the standard hicolor sizes appkit installs the application
// icon at (largest first, each capped by the source size). Icon loaders such
// as KDE's KIconLoader scan these canonical directories and pick the closest
// match to the size they need - a 512px-only install would never be found.
var iconSizes = []int{512, 256, 128, 64, 48, 32, 22}

// targetIconSizes returns the hicolor sizes to install for a square source of
// src pixels: every standard size <= src (largest first), always including
// src itself so the original pixels are never thrown away.
func targetIconSizes(src int) []int {
	if src <= 0 {
		return nil
	}
	sizes := make([]int, 0, len(iconSizes)+1)
	for _, s := range iconSizes {
		if s <= src {
			sizes = append(sizes, s)
		}
	}
	if len(sizes) == 0 || sizes[0] != src {
		sizes = append([]int{src}, sizes...)
	}
	return sizes
}

// removeOtherIconSizes deletes every previously installed copy of the icon
// that lives in a hicolor size directory outside the sizes being installed
// now; with no sizes it deletes every copy. Icon loaders resolve a name to the closest available size, so an
// outdated copy (written by an earlier appkit version, e.g. the old 256px
// glyph) would keep being picked over the freshly installed icon.
func removeOtherIconSizes(dataHome, id string, sizes []int) {
	keep := make(map[string]bool, len(sizes))
	for _, s := range sizes {
		keep[fmt.Sprintf("%dx%d", s, s)] = true
	}
	root := filepath.Join(dataHome, "icons", "hicolor")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() || keep[d.Name()] {
			continue
		}
		p := filepath.Join(root, d.Name(), "apps", id+".png")
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
		}
	}
}

// generatedName is the key that marks a desktop entry installWaylandIdentity
// wrote, so a later start rewrites it and never touches an entry without it;
// generatedKey is the line it writes.
const (
	generatedName = "X-Tuohi-Generated"
	generatedKey  = generatedName + "=true"
)

// isGenerated reports whether a desktop entry sets generatedName to true in
// its [Desktop Entry] group. The same text in a comment, in another key's
// value or in another group does not count.
func isGenerated(entry []byte) bool {
	group := ""
	for _, line := range strings.Split(string(entry), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			group = line[1 : len(line)-1]
		case group == "Desktop Entry" && !strings.HasPrefix(line, "#"):
			key, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(key) == generatedName && strings.TrimSpace(value) == "true" {
				return true
			}
		}
	}
	return false
}

func openURL(rawurl string) error {
	return runXdgOpen(rawurl)
}

// revealFile opens the folder containing absPath. Selecting the specific
// file is file-manager specific on Linux (nautilus --select,
// dolphin --select, ...) and not portable, so the containing directory is
// opened instead.
func revealFile(absPath string) error {
	return runXdgOpen(filepath.Dir(absPath))
}

func runXdgOpen(arg string) error {
	err := exec.Command("xdg-open", arg).Run()
	if err != nil {
		return fmt.Errorf("open: xdg-open %q: %w", arg, err)
	}
	return nil
}
