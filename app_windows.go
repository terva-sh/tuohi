//go:build windows

// Windows app-scope backends: the runtime application icon (App.Icon),
// applied per window, Open/Reveal (ShellExecuteW), and autostart (the HKCU
// Run registry key). Windows has no dlopen, so shell32 symbols are resolved
// with LoadLibrary/GetProcAddress and bound with purego.RegisterFunc.
package tuohi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
	winregistry "golang.org/x/sys/windows/registry"
)

// ensureInit is the one-time platform initialization App.begin runs. Windows
// has nothing to load at that point: the engine loads user32, kernel32 and
// WebView2 when it creates the first window (ensureWinInit, ensureCOMInit).
func ensureInit() error { return nil }

// avoid "unused" linter error
var _ = writeFileAtomic

// --- Runtime application icon (App.Icon) ------------------------------------

// appWindowIconPNG is the PNG most recently handed to setAppIcon (App.Icon,
// or the embedded default when it is unset). Windows has no process-wide
// runtime icon - the executable's own resources set the default - so the icon
// is applied per top-level window instead.
var appWindowIconPNG []byte

// setAppIcon remembers the application icon for the windows the engine shows
// next. It cannot fail for icon reasons (a PNG that cannot be decoded is
// simply never applied), so it always stores the bytes and returns nil; the
// caller runs it before the first window exists.
func setAppIcon(png []byte, _ string) error {
	if len(png) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	appWindowIconPNG = png
	return nil
}

var (
	windowIconOnce  sync.Once
	windowIconBig   uintptr // 32px HICON (ICON_BIG), cached for the process
	windowIconSmall uintptr // 16px HICON (ICON_SMALL)
)

// windowIconHandles returns the cached 32px and 16px HICONs derived from
// appWindowIconPNG (0 when they could not be built). CreateIconFromResourceEx
// decodes PNG payloads on Vista+, but does not rescale them, so the PNG is
// downscaled to the exact icon size first.
func windowIconHandles() (uintptr, uintptr) {
	windowIconOnce.Do(func() {
		src, err := png.Decode(bytes.NewReader(appWindowIconPNG))
		if err != nil {
			return
		}
		b := src.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			return
		}
		// Normalize to straight-alpha NRGBA for the box downscaler.
		img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
		for _, sz := range []int{32, 16} {
			pngBytes := encodeWindowIconPNG(boxDownscale(img, sz))
			if len(pngBytes) == 0 {
				continue
			}
			h := createIconFromResourceEx(&pngBytes[0], uint32(len(pngBytes)), 1, 0x00030000, int32(sz), int32(sz), 0)
			if h == 0 {
				continue
			}
			if sz == 32 {
				windowIconBig = h
			} else {
				windowIconSmall = h
			}
		}
	})
	return windowIconBig, windowIconSmall
}

// applyWindowAppIcon sets the cached application icon on a top-level window:
// WM_SETICON with the big (32px) and small (16px) HICONs is what the taskbar
// button, the title bar and Alt-Tab display. The engine calls it for every
// owned window right before the window is first shown. The handles are cached
// for the process lifetime and never destroyed (the OS reclaims them at exit),
// so every window shares them safely.
func applyWindowAppIcon(hwnd uintptr) {
	if hwnd == 0 || len(appWindowIconPNG) == 0 || createIconFromResourceEx == nil {
		return
	}
	big, small := windowIconHandles()
	if big != 0 {
		sendMessageW(hwnd, wmSetIcon, iconBig, big)
	}
	if small != 0 {
		sendMessageW(hwnd, wmSetIcon, iconSmall, small)
	}
}

// encodeWindowIconPNG encodes an icon image back to PNG bytes for
// CreateIconFromResourceEx.
func encodeWindowIconPNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

const swShowNormal = 1 // SW_SHOWNORMAL

var (
	openInitOnce sync.Once
	openInitErr  error

	shellExecuteW func(hwnd uintptr, op, file, params, dir *uint16, showCmd int32) uintptr
)

func openEnsureInit() error {
	openInitOnce.Do(func() {
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			openInitErr = fmt.Errorf("open: load shell32.dll: %w", err)
			return
		}
		addr, err := syscall.GetProcAddress(shell32, "ShellExecuteW")
		if err != nil {
			openInitErr = fmt.Errorf("open: resolve ShellExecuteW: %w", err)
			return
		}
		purego.RegisterFunc(&shellExecuteW, addr)
	})
	return openInitErr
}

func openURL(rawurl string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	op, _ := syscall.UTF16PtrFromString("open") // constant, never contains NUL
	file, err := syscall.UTF16PtrFromString(rawurl)
	if err != nil {
		return fmt.Errorf("open: %q: %w", rawurl, err)
	}
	// ShellExecuteW returns a value > 32 on success, an error code otherwise.
	r := shellExecuteW(0, op, file, nil, nil, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("open: ShellExecuteW(%q) failed (code %d)", rawurl, r)
	}
	return nil
}

func revealFile(absPath string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	file, _ := syscall.UTF16PtrFromString("explorer.exe") // constant
	// "explorer /select,<path>" opens the containing folder and highlights the
	// file. The path is quoted so spaces don't split it into two arguments.
	params, err := syscall.UTF16PtrFromString(`/select,"` + absPath + `"`)
	if err != nil {
		return fmt.Errorf("open: %q: %w", absPath, err)
	}
	r := shellExecuteW(0, nil, file, params, nil, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("open: reveal %q failed (code %d)", absPath, r)
	}
	return nil
}

// --- Autostart (Windows: the HKCU Run registry key) ------------------------

// The Windows autostart backend: a string value under the per-user Run
// registry key
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run, whose command line
// starts with the registered executable. Windows runs every value found
// there at login.

// autostartRunSubKey is the per-user registry key Windows executes at login.
const autostartRunSubKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// registryAutostart implements autostartBackend with the HKCU Run key.
type registryAutostart struct {
	// subKey is the registry key read/written (the Run key in production;
	// overridable by tests).
	subKey string
}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_windows.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newAutostartBackend returns the registry autostart backend for the
// committed App settings.
func newAutostartBackend(cfg appConfig) autostartBackend {
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
		return fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	// Remove a stale entry pointing at this binary under a different value
	// name, so Enable never leaves two autostart entries behind.
	if existing, _, ferr := a.find(); ferr == nil && existing != "" && existing != id {
		_ = key.DeleteValue(existing)
	}
	if err := key.SetStringValue(id, cmd); err != nil {
		return fmt.Errorf("appkit: autostart: write registry value: %w", err)
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
		return fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.DeleteValue(id); err != nil && !errors.Is(err, winregistry.ErrNotExist) {
		return fmt.Errorf("appkit: autostart: delete registry value: %w", err)
	}
	return nil
}

func (a *registryAutostart) status() (bool, string, string) {
	id, _, err := a.find()
	if err != nil || id == "" {
		return false, "", ""
	}
	return true, `HKCU\` + a.subKey + `\` + id, autostartBackendRegistryRun
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
		return "", "", fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return "", "", fmt.Errorf("appkit: autostart: list registry values: %w", err)
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
