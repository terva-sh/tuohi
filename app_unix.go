//go:build linux || freebsd || netbsd

// Unix app-scope backends (Linux, FreeBSD, NetBSD): single-instance lock and
// hand-off socket (flock + Unix socket), Open/Reveal via xdg-open, and the
// runtime application icon (App.Icon), installed into the GTK stack by
// lib_unix.go.

package tuohi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// runtimeDir picks a per-user directory for the lock and socket.
// XDG_RUNTIME_DIR is the right place on Linux; elsewhere the temp dir is the
// portable fallback. A set-but-unusable runtime dir falls back to the temp dir
// too: containers and sandboxes can mount XDG_RUNTIME_DIR read-only, and the
// single-instance lock must not fail the whole app start because the runtime
// dir cannot hold a file. The choice is cached for the process lifetime so all
// instances of one application agree on where the lock lives.
var (
	runtimeDirOnce sync.Once
	runtimeDirPath string
)

func runtimeDir() string {
	runtimeDirOnce.Do(func() {
		runtimeDirPath = os.TempDir()
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && isWritableDir(d) {
			runtimeDirPath = d
		}
	})
	return runtimeDirPath
}

// isWritableDir reports whether dir accepts new files right now, by creating
// and removing a probe file inside it. CreateTemp fails on a missing or
// read-only directory.
func isWritableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".appkit-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func instancePaths(id string) (lock, sock string) {
	stem := filepath.Join(runtimeDir(), "native-si-"+instanceKey(id))
	return stem + ".lock", stem + ".sock"
}

func acquire(id string, onMessage func([]string)) (*instanceLock, error) {
	lockPath, sockPath := instancePaths(id)
	// lockPath is runtimeDir() + a sha256 hex of id, so it cannot traverse out.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304
	if err != nil {
		return nil, err
	}
	// A file descriptor is a small non-negative int, so the conversion is safe.
	fd := int(f.Fd()) // #nosec G115
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errAlreadyRunning
		}
		return nil, err
	}

	// We hold the lock: we are the primary. A previous primary that crashed may
	// have left a stale socket file; since we hold the lock, removing it is safe.
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	go serveInstance(ln, onMessage)

	return &instanceLock{release: func() error {
		_ = ln.Close() // unblocks the Accept loop and unlinks the socket
		_ = os.Remove(sockPath)
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		err := f.Close()
		_ = os.Remove(lockPath)
		return err
	}}, nil
}

func serveInstance(ln net.Listener, onMessage func([]string)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on Release
		}
		go func() {
			defer func() { _ = conn.Close() }()
			data, err := io.ReadAll(conn)
			if err != nil {
				return
			}
			var args []string
			if json.Unmarshal(data, &args) == nil && onMessage != nil {
				onMessage(args)
			}
		}()
	}
}

func send(id string, args []string) error {
	_, sockPath := instancePaths(id)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return err // no instance listening (dial refused / socket missing)
	}
	defer func() { _ = conn.Close() }()
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

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
// but GTK3 cannot, so on a Wayland session the GTK3 path additionally
// installs a per-user .desktop entry and a themed icon keyed to the app's
// name (installWaylandIdentity) and advertises the matching program name -
// that is the only channel a GTK3 window has on Wayland. Failure at any step
// is reported but never fatal (App.start ignores the error).
func setAppIcon(pngData []byte, name string) error {
	if len(pngData) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("appkit: application icon: %w", err)
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
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if id := installWaylandIdentity(name, img); id != "" && gSetPrgname != nil {
			gSetPrgname(id)
		}
	}
	return gtk3InstallAppIcon(img.Pix, b.Dx(), b.Dy())
}

// --- Wayland application identity (GTK3) -----------------------------------

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
		"Name=" + strings.ReplaceAll(displayName, "\n", " ") + "\n" +
		"Icon=" + id + "\n" +
		"Exec=" + desktopQuote(exe) + "\n" +
		"Terminal=false\n" +
		"Hidden=true\n" +
		"NoDisplay=true\n"
	desktopPath := filepath.Join(dataHome, "applications", id+".desktop")
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
		// Run detached: the app must not block on (or fail because of) the
		// desktop's cache rebuild.
		cmd := exec.Command(bin, "--noincremental")
		_ = cmd.Start()
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
// now. Icon loaders resolve a name to the closest available size, so an
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

// desktopQuote quotes a value for a .desktop Exec key when it contains
// whitespace or quotes (the desktop-entry Exec quoting rules).
func desktopQuote(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r <= ' ' || r == '"' || r == '\'' || r == '\\' {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
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

// --- Autostart -------------------------------------

// The Unix autostart backend: a freedesktop.org autostart .desktop file in
// $XDG_CONFIG_HOME/autostart (defaulting to ~/.config/autostart). Desktop
// environments that honour the XDG autostart spec run every .desktop file
// found there at login.

// xdgAutostart implements autostartBackend with .desktop files.
type xdgAutostart struct {
	name string // display name for the Name= key (App.Name, or the executable base)
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_linux.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newAutostartBackend returns the XDG autostart backend for the committed
// App settings.
func newAutostartBackend(cfg appConfig) autostartBackend {
	name := cfg.Name
	if name == "" {
		if exe, err := os.Executable(); err == nil {
			name = filepath.Base(exe)
		}
	}
	return &xdgAutostart{name: name}
}

func (a *xdgAutostart) enable(id string, args []string) error {
	// A newline inside the executable path or an argument would break the
	// .desktop line-based key=value format and could inject Desktop Entry
	// keys when the arguments are user-influenced.
	if err := validateDesktopExecToken(id); err != nil {
		return fmt.Errorf("appkit: autostart identifier: %w", err)
	}
	for i, arg := range args {
		if err := validateDesktopExecToken(arg); err != nil {
			return fmt.Errorf("appkit: autostart argument %d: %w", i, err)
		}
	}
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	if err := validateDesktopExecToken(exe); err != nil {
		return fmt.Errorf("appkit: autostart executable path: %w", err)
	}
	dir, err := a.autostartDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("appkit: autostart: create autostart dir: %w", err)
	}
	path := filepath.Join(dir, id+".desktop")
	// Remove a stale .desktop file pointing at this binary under a different
	// identifier (e.g. from a renamed App.Name), so Enable never leaves two
	// entries behind.
	if existing, ferr := a.findDesktopFile(dir); ferr == nil && existing != "" && existing != path {
		_ = os.Remove(existing)
	}
	body := buildDesktopEntry(a.name, exe, args)
	if err := writeFileAtomic(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("appkit: autostart: write desktop file %s: %w", path, err)
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
		return fmt.Errorf("appkit: autostart: remove desktop file: %w", err)
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
	return true, path, autostartBackendXDGAutostart
}

// autostartDir returns the XDG autostart directory ($XDG_CONFIG_HOME/
// autostart, defaulting to ~/.config/autostart).
func (a *xdgAutostart) autostartDir() (string, error) {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("appkit: autostart: %w", err)
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
		return "", fmt.Errorf("appkit: autostart: read autostart dir: %w", err)
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
	fmt.Fprintf(&b, "Name=%s\n", escapeDesktopValue(appName))
	b.WriteString("Exec=" + quoteExec(exe))
	for _, a := range args {
		b.WriteString(" " + quoteExec(a))
	}
	b.WriteString("\n")
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

// quoteExec quotes a single Exec field token per the freedesktop.org spec:
// reserved characters (" ` $ \) are backslash-escaped, and the token is
// double-quoted when it contains any reserved character or whitespace.
func quoteExec(s string) string {
	needQuote := false
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
			needQuote = true
		case ' ', '\t':
			// Newlines are rejected by validateDesktopExecToken before we
			// get here, so any remaining whitespace is safely quotable.
			b.WriteRune(r)
			needQuote = true
		default:
			b.WriteRune(r)
		}
	}
	if needQuote {
		return `"` + b.String() + `"`
	}
	return b.String()
}

// escapeDesktopValue replaces characters that are not allowed raw in Desktop
// Entry values (newlines) and trims surrounding whitespace.
func escapeDesktopValue(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// desktopExecPath returns the first Exec= token (quotes honoured) of a
// .desktop file's contents; empty when the file has no Exec= line.
func desktopExecPath(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		if strings.HasPrefix(val, `"`) {
			end := strings.Index(val[1:], `"`)
			if end < 0 {
				return ""
			}
			return unescapeDesktopToken(val[1 : 1+end])
		}
		if i := strings.IndexAny(val, " \t"); i >= 0 {
			return val[:i]
		}
		return val
	}
	return ""
}

// unescapeDesktopToken removes the backslash escapes quoteExec adds.
func unescapeDesktopToken(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
