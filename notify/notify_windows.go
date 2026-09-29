// Windows notification backend: a balloon notification via Shell_NotifyIconW.
// Balloons attach to a notification-area icon, so the first Show lazily
// creates a hidden helper window and adds an application-icon notification
// entry (kept for the life of the process); every Show then sends NIM_MODIFY
// with NIF_INFO to raise the balloon. No tray, window or cgo is required.
// Symbols are resolved with LoadLibrary/GetProcAddress and bound with
// purego.RegisterFunc; the window procedure is a purego.NewCallback.
//
// Options: the urgency maps to the balloon info flags - UrgencyCritical shows
// the error glyph, low/normal the information glyph (the historical look of a
// plain Show). Options.Icon is either a stock name ("info", "information",
// "warning" or "error", case-insensitive) selecting the matching system
// glyph, or a path to an .ico/.bmp file loaded with LoadImageW
// (LR_LOADFROMFILE) and shown both as the balloon icon (NIIF_USER +
// hBalloonIcon under NOTIFYICON_VERSION_4) and as the tray icon. LoadImageW
// does not decode PNG, so Options.IconData and PNG paths are rejected instead
// of silently showing nothing.
//
// beep calls the kernel Beep function (a tone that blocks for the duration);
// alertSound backs Alert with the user32 MessageBeep system sound, because
// balloons themselves are silent.

package notify

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	// NOTIFYICONDATAW dwInfoFlags (shellapi.h)
	niifInfo      = 0x00000001
	niifWarning   = 0x00000002
	niifError     = 0x00000003
	niifUser      = 0x00000004 // NIIF_USER: balloon icon comes from hBalloonIcon
	niifLargeIcon = 0x00000020

	imageIcon      = 0x00000001 // IMAGE_ICON
	lrLoadFromFile = 0x00000010

	// Standard icon IDs for LoadIconW(0, id).
	idiApplication = 32512
	idiError       = 32513
	idiInformation = 32515
	idiWarning     = 32516

	// Standard cursor ID for LoadCursorW(0, id).
	idcArrow = 32512

	// notifyIconVersion4 enables NIIF_USER/hBalloonIcon balloon icons
	// (NOTIFYICON_VERSION_4, set with NIM_SETVERSION).
	notifyIconVersion4 = 4
)

// notifyIconData mirrors NOTIFYICONDATAW layout (976 bytes on x64). Fields Go
// never reads are left blank - they exist only so the struct's size and
// offsets match what the shell expects; the named counterparts are documented
// in the comments.
type notifyIconData struct {
	cbSize       uint32
	hWnd         uintptr
	uID          uint32
	uFlags       uint32
	_            uint32 // uCallbackMessage (NIF_MESSAGE; unused)
	hIcon        uintptr
	_            [128]uint16 // szTip (NIF_TIP tooltip text; unused)
	_            uint32      // dwState
	_            uint32      // dwStateMask
	szInfo       [256]uint16 // balloon text
	uVersion     uint32      // uVersion/uTimeout union: icon version for NIM_SETVERSION
	szInfoTitle  [64]uint16  // balloon title
	dwInfoFlags  uint32
	_            [16]byte // guidItem
	hBalloonIcon uintptr  // NIIF_USER custom balloon icon
}

// Compile-time check that the mirror matches sizeof(NOTIFYICONDATAW) on the
// current architecture - 956 bytes on 32-bit Windows, 976 on 64-bit (LLP64).
// notifyIconDataSize is pinned per architecture (see notifyIconData_size_*.go).
var (
	_ [unsafe.Sizeof(notifyIconData{}) - notifyIconDataSize]byte
	_ [notifyIconDataSize - unsafe.Sizeof(notifyIconData{})]byte
)

// wndClassExW mirrors WNDCLASSEXW (80 bytes on x64).
type wndClassExW struct {
	cbSize        uint32
	_             uint32 // style
	lpfnWndProc   uintptr
	_             int32 // cbClsExtra
	_             int32 // cbWndExtra
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	_             uintptr // hbrBackground
	_             *uint16 // lpszMenuName
	lpszClassName *uint16
	_             uintptr // hIconSm
}

var (
	initOnce sync.Once
	initErr  error

	nid notifyIconData
	hic uintptr

	// version4 is true once the shell accepted NOTIFYICON_VERSION_4, which is
	// what makes NIIF_USER + hBalloonIcon work. On the rare shell that
	// refuses it, custom file icons degrade to a swapped tray icon.
	version4 bool

	// Cached custom file icon (LoadImageW handle) - one per path, destroyed
	// when replaced.
	iconMu   sync.Mutex
	iconPath string
	iconH    uintptr

	wndProcCB uintptr
	className *uint16

	registerClassExW func(*wndClassExW) uint16
	createWindowExW  func(exStyle uint32, className, windowName *uint16, style uint32, x, y, width, height int32, parent, menu, instance, param uintptr) uintptr
	defWindowProcW   func(hwnd, msg, wParam, lParam uintptr) uintptr
	loadIconW        func(instance, name uintptr) uintptr
	loadCursorW      func(instance, name uintptr) uintptr
	getModuleHandleW func(name *uint16) uintptr
	shellNotifyIconW func(msg uint32, data *notifyIconData) int32
	loadImageW       func(instance uintptr, name *uint16, typ uint32, cx, cy int32, flags uint32) uintptr
	destroyIcon      func(icon uintptr) uint32
	messageBeep      func(uType uint32) uint32
	kernelBeep       func(freq, duration uint32) uint32
	getLastError     func() uint32
)

func ensureInit() error {
	initOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load user32.dll: %w", err)
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load shell32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load kernel32.dll: %w", err)
			return
		}
		reg := func(p any, lib syscall.Handle, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(lib, name)
			if e != nil {
				initErr = fmt.Errorf("notify: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&loadIconW, user32, "LoadIconW")
		reg(&loadCursorW, user32, "LoadCursorW")
		reg(&loadImageW, user32, "LoadImageW")
		reg(&destroyIcon, user32, "DestroyIcon")
		reg(&messageBeep, user32, "MessageBeep")
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&kernelBeep, kernel32, "Beep")
		reg(&getLastError, kernel32, "GetLastError")
		reg(&shellNotifyIconW, shell32, "Shell_NotifyIconW")
		if initErr != nil {
			return
		}

		wndProcCB = purego.NewCallback(notifyWndProc)
		className = utf16Ptr("NativeNotifyWindow")
		hInst := getModuleHandleW(nil)
		wc := wndClassExW{
			lpfnWndProc:   wndProcCB,
			hInstance:     hInst,
			hIcon:         loadIconW(0, idiApplication),
			hCursor:       loadCursorW(0, idcArrow),
			lpszClassName: className,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc)) // #nosec G115 -- fixed small struct size
		if registerClassExW(&wc) == 0 {
			initErr = errors.New("notify: RegisterClassExW failed")
			return
		}

		w := createWindowExW(0, className, utf16Ptr("native notify"), 0, 0, 0, 0, 0, 0, 0, hInst, 0)
		if w == 0 {
			initErr = errors.New("notify: CreateWindowExW failed")
			return
		}

		hic = loadIconW(0, idiApplication)
		n := notifyIconData{}
		n.cbSize = uint32(unsafe.Sizeof(n)) // #nosec G115 -- fixed struct size (976 on x64)
		n.hWnd = w
		n.uID = 1
		n.uFlags = nifMessage | nifIcon
		n.hIcon = hic
		if shellNotifyIconW(nimAdd, &n) == 0 {
			initErr = errors.New("notify: Shell_NotifyIconW NIM_ADD failed")
			return
		}
		// Version 4 enables NIIF_USER + hBalloonIcon on the balloons. A shell
		// that rejects it (very old Windows) still shows plain balloons, so
		// the failure is recorded, not fatal.
		n.uVersion = notifyIconVersion4
		if shellNotifyIconW(nimSetVersion, &n) != 0 {
			version4 = true
		}
		nid = n
	})
	return initErr
}

// notifyWndProc handles no messages; the window exists only to host the
// notification icon data.
func notifyWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	return defWindowProcW(hwnd, msg, wParam, lParam)
}

// urgencyFlags maps a normalized urgency level to the balloon info flags.
// Windows has no "low" balloon style: low and normal both show the
// information glyph (a zero Options keeps the historical info look), and
// critical shows the error glyph. An explicit stock icon overrides this.
func urgencyFlags(u Urgency) uint32 {
	if u.level() == 2 {
		return niifError
	}
	return niifInfo
}

// stockGlyph recognizes the Windows stock icon names and returns the balloon
// flag for the matching system glyph. The names are matched
// case-insensitively; anything else is treated as a file path by the caller.
func stockGlyph(name string) (uint32, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "info", "information":
		return niifInfo, true
	case "warning":
		return niifWarning, true
	case "error":
		return niifError, true
	}
	return 0, false
}

// loadFileIcon loads an .ico or .bmp file with LoadImageW (LR_LOADFROMFILE)
// and caches the handle per path, destroying the icon it replaces. LoadImageW
// does not decode PNG - that is why Options.IconData and PNG file paths are
// rejected rather than silently showing nothing.
func loadFileIcon(path string) (uintptr, error) {
	iconMu.Lock()
	defer iconMu.Unlock()
	if path == iconPath && iconH != 0 {
		return iconH, nil
	}
	h := loadImageW(0, utf16Ptr(path), imageIcon, 0, 0, lrLoadFromFile)
	if h == 0 {
		return 0, fmt.Errorf("notify: LoadImageW cannot load icon file %q (error %d); Windows notification icons must be .ico or .bmp files", path, getLastError())
	}
	if iconH != 0 {
		destroyIcon(iconH)
	}
	iconPath = path
	iconH = h
	return h, nil
}

// show raises a balloon from the lazily created notification icon. Title is
// limited to 63 characters and message to 255 (Win32 limits). The name is
// ignored: Windows derives the source identity from the OS.
func show(name, title, message string, opts Options) error {
	if err := ensureInit(); err != nil {
		return err
	}
	if len(opts.IconData) > 0 {
		return fmt.Errorf("%w: Options.IconData (PNG bytes) is not decoded on Windows; pass an .ico or .bmp file path as Options.Icon", ErrUnsupported)
	}

	n := nid
	n.uFlags = nifInfo
	n.dwInfoFlags = urgencyFlags(opts.Urgency)
	n.hBalloonIcon = 0
	swapped := false // true when the tray icon was replaced for this post

	if opts.Icon != "" {
		if glyph, ok := stockGlyph(opts.Icon); ok {
			// Stock name: the matching system glyph in the balloon.
			n.dwInfoFlags = glyph
		} else if h, err := loadFileIcon(opts.Icon); err != nil {
			return err
		} else if version4 {
			// Custom balloon icon: NIIF_USER + hBalloonIcon, sized like the
			// standard large balloon icons.
			n.dwInfoFlags = niifUser | niifLargeIcon
			n.hBalloonIcon = h
		} else {
			// Pre-v4 shells cannot show hBalloonIcon; the balloon then shows
			// the tray icon, so swap it in for this post only.
			n.uFlags |= nifIcon
			n.hIcon = h
			swapped = true
		}
	}
	copyString(n.szInfoTitle[:], title)
	copyString(n.szInfo[:], message)

	if shellNotifyIconW(nimModify, &n) == 0 {
		return fmt.Errorf("notify: Shell_NotifyIconW NIM_MODIFY (notification) failed")
	}
	if swapped {
		// Restore the plain application icon as the persistent tray icon.
		r := nid
		r.uFlags = nifIcon
		r.hIcon = hic
		shellNotifyIconW(nimModify, &r)
	}
	return nil
}

// beep sounds a tone through the kernel Beep function, which blocks for the
// duration. The frequency is clamped to the 37–32767 Hz range the driver
// accepts; zero or out-of-range values fall back to the defaults.
func beep(freq float64, duration int) error {
	if err := ensureInit(); err != nil {
		return err
	}
	if freq == 0 {
		freq = DefaultFreq
	} else if freq > 32767 {
		freq = 32767
	} else if freq < 37 {
		freq = DefaultFreq
	}
	if duration == 0 {
		duration = DefaultDuration
	} else if duration < 0 {
		duration = DefaultDuration
	}
	// Beep's success flag and GetLastError must come from the same OS thread,
	// so pin the goroutine across both calls.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if kernelBeep(uint32(freq), uint32(duration)) == 0 { // #nosec G115 -- freq clamped to 37..32767, duration >= 0
		return fmt.Errorf("notify: kernel32 Beep failed (error %d)", getLastError())
	}
	return nil
}

// alertSound backs Alert: after the critical balloon is posted, play the
// default system sound, because balloons themselves are silent. Failures are
// cosmetic and ignored.
func alertSound() error {
	messageBeep(0) // MB_OK selects the default system sound
	return nil
}

// copyString copies s as UTF-16 into buf, NUL-terminated and truncated to fit.
func copyString(buf []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	m := len(u)
	if m > len(buf) {
		m = len(buf)
	}
	copy(buf[:m], u[:m])
	buf[len(buf)-1] = 0
}

// utf16Ptr returns a NUL-terminated UTF-16 pointer for s ("" on an embedded
// NUL, never nil).
func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}
