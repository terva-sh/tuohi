//go:build windows

// Windows app-scope backends: the runtime application icon (App.Icon),
// applied per window, and Open/Reveal (ShellExecuteW). Windows has no dlopen, so shell32 symbols are resolved
// with LoadLibrary/GetProcAddress and bound with purego.RegisterFunc.
package tuohi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

// ensureInit is the one-time platform initialization App.begin runs. Windows
// has nothing to load at that point: the engine loads user32, kernel32 and
// WebView2 when it creates the first window (ensureWinInit, ensureCOMInit).
func ensureInit() error { return nil }

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
