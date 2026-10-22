// Windows backend: a notification-area icon via Shell_NotifyIconW, backed by
// a hidden helper window whose procedure receives the icon's mouse messages
// and pops the menu (TrackPopupMenu). Windows has no dlopen, so symbols are
// resolved with LoadLibrary/GetProcAddress and bound with
// purego.RegisterFunc; the window procedure is a purego.NewCallback.
//
// Config.Icon (a PNG) becomes the HICON via CreateIconFromResourceEx; when a
// DarkModeIcon is set the tray picks light/dark from the system theme
// (HKCU ... Personalize SystemUsesLightTheme) at Set time and switches live
// on WM_SETTINGCHANGE ("ImmersiveColorSet"). Menu items support checkmarks
// (MF_CHECKED, toggled by SetMenuItemInfoW on click), disabled state
// (MF_GRAYED) and one level of submenu (MF_POPUP); per-item icons are not
// supported by AppendMenuW and are ignored. Left click runs Config.OnClick, a
// double click runs OnDoubleClick, and a right click runs OnRightClick and
// pops the context menu. Bounds comes from Shell_NotifyIconGetRect.
//
// Run owns the thread's message loop and blocks; Stop posts WM_CLOSE to the
// helper window from any thread, which tears the icon down and ends the loop.

package tray

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmNull          = 0x0000
	wmApp           = 0x8000
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmSettingChange = 0x001A
	wmContextMenu   = 0x007B
	wmTimer         = 0x0113

	trayCallbackMsg = wmApp + 1
	trayUpdateMsg   = wmApp + 2

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	notifyIconVersion4 = 4

	idiApplication = 32512
	idcArrow       = 32512

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	lrDefaultColor = 0x00000000

	smCXSmIcon = 49 // SM_CXSMICON
	smCYSmIcon = 50 // SM_CYSMICON

	miiState       = 0x00000001 // MENUITEMINFO fMask: state
	mfsCheckedFlag = 0x00000008 // MFS_CHECKED
	mfsDisabled    = 0x00000003 // MFS_DISABLED | MFS_GRAYED
	byPosition     = 0x00000400 // MF_BYPOSITION
)

type point struct{ X, Y int32 }

// wndClassExW mirrors WNDCLASSEXW (80 bytes on x64). Fields the package does not
// set are blanked to keep the layout without tripping the unused-field linter.
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

// msgStruct is an opaque MSG buffer (48 bytes on x64): its address is handed to
// Get/Translate/DispatchMessage and its fields are never read here.
type msgStruct struct{ _ [6]uintptr }

// notifyIconData mirrors NOTIFYICONDATAW; its size (976 bytes on x64) is written
// into cbSize so the shell accepts it. uVersion carries NOTIFYICON_VERSION_4
// for the NIM_SETVERSION call; the szInfo tail is blanked to preserve the size
// without unused-field warnings.
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	_                uint32      // dwState
	_                uint32      // dwStateMask
	_                [256]uint16 // szInfo (balloon text; unused here)
	uVersion         uint32      // union with uTimeout
	_                [64]uint16  // szInfoTitle
	_                uint32      // dwInfoFlags
	_                [16]byte    // guidItem
	_                uintptr     // hBalloonIcon
}

// Compile-time guard that the mirror matches sizeof(NOTIFYICONDATAW) on the
// current architecture - 956 bytes on 32-bit Windows, 976 on 64-bit (LLP64).
// notifyIconDataSize is pinned per architecture (see notifyIconData_size_*.go).
var (
	_ [unsafe.Sizeof(notifyIconData{}) - notifyIconDataSize]byte
	_ [notifyIconDataSize - unsafe.Sizeof(notifyIconData{})]byte
)

// notifyIconIdentifier is the NOTIFYICONIDENTIFIER struct for
// Shell_NotifyIconGetRect (Windows 7+).
type notifyIconIdentifier struct {
	cbSize uint32
	hWnd   uintptr
	uID    uint32
	_      [16]byte // guidItem
}

type winRect struct{ left, top, right, bottom int32 }

// menuItemInfoW mirrors MENUITEMINFOW (used with fByPosition for checkmarks).
// Only the fields this file touches are named; the rest stay blank so the
// struct keeps the exact Win32 layout.
type menuItemInfoW struct {
	cbSize uint32
	fMask  uint32
	fState uint32
	_      uint32  // fType
	_      uint32  // wID
	_      uintptr // hSubMenu
	_      uintptr // hbmpChecked
	_      uintptr // hbmpUnchecked
	_      uintptr // dwItemData
	_      uintptr // dwTypeData
	_      uint32  // cch
	_      uintptr // hbmpItem
}

// cmdEntry records one clickable menu item for dispatch and checkbox
// toggling. cmd is the command id TrackPopupMenu returns.
type cmdEntry struct {
	onClick  func()
	checkbox bool
	checked  bool // runtime state; starts from Item.Checked
	hmenu    uintptr
	pos      uint32 // position inside hmenu, for fByPosition state updates
}

var (
	initOnce sync.Once
	initErr  error

	mu              sync.Mutex
	running         bool
	ownsLoop        bool // run() owns the GetMessage loop; only it may post WM_QUIT
	trayHwnd        uintptr
	hMenu           uintptr
	nid             notifyIconData
	hicon           uintptr
	hiconOwned      bool
	winConfig       Config
	winAvailability *availabilitySource
	winProbeActive  bool
	winUpdates      updateQueue
	ownerThread     uint32
	menuOpen        bool
	closeAfterMenu  bool
	deferredUpdates []uintptr
	taskbarCreated  uint32

	trayClickFn      func()
	trayDblClickFn   func()
	trayRightClickFn func()
	cmdByID          map[uintptr]*cmdEntry
	cmdNext          uintptr
	iconPNG          []byte // light icon (Config.Icon)
	iconDark         []byte // Config.DarkModeIcon
	tip              string

	cmdMu sync.Mutex

	trayWndProcCB uintptr
	classNamePtr  *uint16

	registerClassExW         func(*wndClassExW) uint16
	createWindowExW          func(exStyle uint32, className, windowName *uint16, style uint32, x, y, width, height int32, parent, menu, instance, param uintptr) uintptr
	defWindowProcW           func(hwnd, msg, wParam, lParam uintptr) uintptr
	getMessageW              func(msg *msgStruct, hwnd uintptr, filterMin, filterMax uint32) int32
	translateMessage         func(*msgStruct) int32
	dispatchMessageW         func(*msgStruct) uintptr
	postQuitMessage          func(exitCode int32)
	destroyWindow            func(hwnd uintptr) int32
	loadIconW                func(instance, name uintptr) uintptr
	loadCursorW              func(instance, name uintptr) uintptr
	getCursorPos             func(*point) int32
	endMenu                  func() int32
	registerWindowMessageW   func(*uint16) uint32
	setTimer                 func(hwnd, id uintptr, interval uint32, callback uintptr) uintptr
	setForegroundWindow      func(hwnd uintptr) int32
	trackPopupMenu           func(menu uintptr, flags uint32, x, y, reserved int32, hwnd, rect uintptr) int32
	createPopupMenu          func() uintptr
	appendMenuW              func(menu uintptr, flags uint32, id uintptr, item *uint16) int32
	destroyMenu              func(menu uintptr) int32
	postMessageW             func(hwnd, msg, wParam, lParam uintptr) int32
	getModuleHandleW         func(name *uint16) uintptr
	shellNotifyIconW         func(msg uint32, data *notifyIconData) int32
	createIconFromResourceEx func(resource *byte, bytes uint32, isIcon int32, ver uint32, cx, cy int32, flags uint32) uintptr
	destroyIcon              func(icon uintptr) int32
	getSystemMetrics         func(index int32) int32
	setMenuItemInfoW         func(menu uintptr, item uintptr, byPosition int32, info *menuItemInfoW) int32
	shellNotifyIconGetRect   func(id *notifyIconIdentifier, rc *winRect) int32
)

func ensureInit() error {
	initOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load user32.dll: %w", err)
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load shell32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load kernel32.dll: %w", err)
			return
		}

		reg := func(p any, lib syscall.Handle, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(lib, name)
			if e != nil {
				initErr = fmt.Errorf("tray: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&getMessageW, user32, "GetMessageW")
		reg(&translateMessage, user32, "TranslateMessage")
		reg(&dispatchMessageW, user32, "DispatchMessageW")
		reg(&postQuitMessage, user32, "PostQuitMessage")
		reg(&destroyWindow, user32, "DestroyWindow")
		reg(&loadIconW, user32, "LoadIconW")
		reg(&loadCursorW, user32, "LoadCursorW")
		reg(&getCursorPos, user32, "GetCursorPos")
		reg(&endMenu, user32, "EndMenu")
		reg(&registerWindowMessageW, user32, "RegisterWindowMessageW")
		reg(&setTimer, user32, "SetTimer")
		reg(&setForegroundWindow, user32, "SetForegroundWindow")
		reg(&trackPopupMenu, user32, "TrackPopupMenu")
		reg(&createPopupMenu, user32, "CreatePopupMenu")
		reg(&appendMenuW, user32, "AppendMenuW")
		reg(&destroyMenu, user32, "DestroyMenu")
		reg(&postMessageW, user32, "PostMessageW")
		reg(&setMenuItemInfoW, user32, "SetMenuItemInfoW")
		reg(&createIconFromResourceEx, user32, "CreateIconFromResourceEx")
		reg(&destroyIcon, user32, "DestroyIcon")
		reg(&getSystemMetrics, user32, "GetSystemMetrics")
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&shellNotifyIconW, shell32, "Shell_NotifyIconW")
		reg(&shellNotifyIconGetRect, shell32, "Shell_NotifyIconGetRect")
		if initErr != nil {
			return
		}

		taskbarCreated = registerWindowMessageW(utf16Ptr("TaskbarCreated"))
		trayWndProcCB = purego.NewCallback(trayWndProc)
		classNamePtr = utf16Ptr("NativeTrayWindow")
		hInst := getModuleHandleW(nil)
		wc := wndClassExW{
			lpfnWndProc:   trayWndProcCB,
			hInstance:     hInst,
			hIcon:         loadIconW(0, idiApplication),
			hCursor:       loadCursorW(0, idcArrow),
			lpszClassName: classNamePtr,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc)) // #nosec G115 -- fixed small struct size
		if registerClassExW(&wc) == 0 {
			initErr = errors.New("tray: RegisterClassExW failed")
		}
	})
	return initErr
}

// trayWndProc dispatches the icon's mouse callback and context menu, reacts
// to theme changes, and handles window teardown; everything else goes to
// DefWindowProc. wParam/lParam are unsafe.Pointer so the WM_SETTINGCHANGE
// payload can be read with a vet-clean pointer conversion (uintptr would trip
// unsafeptr).
func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam unsafe.Pointer) uintptr {
	if taskbarCreated != 0 && msg == taskbarCreated {
		restoreTrayRegistration()
		return 0
	}
	switch msg {
	case wmTimer:
		if uintptr(wParam) == 1 {
			probeTrayRegistration()
			return 0
		}
	case trayUpdateMsg:
		applyQueuedWindowsUpdate(uintptr(wParam))
		return 0
	case trayCallbackMsg:
		ev := uintptr(lParam) & 0xFFFF // #nosec G103 -- event id in LOWORD
		mu.Lock()
		left := trayClickFn
		dbl := trayDblClickFn
		right := trayRightClickFn
		mu.Unlock()
		switch ev {
		case wmLButtonUp:
			if left != nil {
				left()
			}
		case wmLButtonDblClk:
			if dbl != nil {
				dbl()
			}
		case wmRButtonUp:
			if right != nil {
				right()
			}
			showMenu(hwnd)
		case wmContextMenu:
			showMenu(hwnd)
		}
		return 0
	case wmSettingChange:
		// "ImmersiveColorSet" in lParam means the dark/light theme changed.
		if isImmersiveColorSet(lParam) {
			updateIconForTheme()
		}
		return defWindowProcW(hwnd, uintptr(msg), uintptr(wParam), uintptr(lParam))
	case wmClose:
		mu.Lock()
		tracking := menuOpen
		if tracking {
			closeAfterMenu = true
		}
		mu.Unlock()
		if tracking {
			endMenu()
			return 0
		}
		destroyWindow(hwnd)
		return 0
	case wmDestroy:
		mu.Lock()
		own := ownsLoop
		mu.Unlock()
		releaseTrayResources()
		if own {
			postQuitMessage(0) // end Run's loop; run() then cleans up
		}
		return 0
	}
	return defWindowProcW(hwnd, uintptr(msg), uintptr(wParam), uintptr(lParam))
}

// isImmersiveColorSet reports whether the WM_SETTINGCHANGE lParam names the
// "ImmersiveColorSet" theme change (the dark/light toggle). For the duration
// of the message the OS guarantees a NUL-terminated UTF-16 string at ptr.
func isImmersiveColorSet(ptr unsafe.Pointer) bool {
	if ptr == nil {
		return false
	}
	s := windows.UTF16PtrToString((*uint16)(ptr)) // #nosec G103 -- message lParam
	return strings.EqualFold(s, "ImmersiveColorSet")
}

// updateIconForTheme re-applies the icon that matches the current system
// theme.
func updateIconForTheme() {
	mu.Lock()
	a := winAvailability
	mu.Unlock()
	if a != nil {
		_ = applyWindowsUpdate(a, Changes{})
	}
}

func restoreTrayRegistration() {
	mu.Lock()
	a := winAvailability
	n := nid
	mu.Unlock()
	if a == nil {
		return
	}
	a.publish(Availability{Running: true, Err: ErrUnavailable})
	ok := shellNotifyIconW(nimAdd, &n) != 0
	if ok {
		versionTrayIcon(n)
	}
	publishWindowsAvailability(a, ok)
}

// A no-op modification probes current shell registration. TaskbarCreated
// announces a returning shell, but has no matching disappearance broadcast.
func probeTrayRegistration() {
	mu.Lock()
	a, n := winAvailability, nid
	mu.Unlock()
	if a == nil {
		return
	}
	n.uFlags = 0
	if shellNotifyIconW(nimModify, &n) != 0 {
		publishWindowsAvailability(a, true)
		return
	}
	restoreTrayRegistration()
}
func publishWindowsAvailability(a *availabilitySource, ok bool) {
	v := Availability{Running: true, Registered: ok, HostPresent: ok}
	if !ok {
		v.Err = ErrUnavailable
	} else {
		mu.Lock()
		probing := winProbeActive
		mu.Unlock()
		if !probing {
			v.Err = fmt.Errorf("%w: shell registration probing unavailable", ErrUnavailable)
		}
	}
	a.publish(v)
}
func versionTrayIcon(n notifyIconData) {
	n.uFlags = 0
	n.uVersion = notifyIconVersion4
	shellNotifyIconW(nimSetVersion, &n)
}

func update(ctx context.Context, c Changes) error {
	mu.Lock()
	a, h, thread := winAvailability, trayHwnd, ownerThread
	tracking := menuOpen
	mu.Unlock()
	if a == nil || h == 0 {
		return ErrNotRunning
	}
	if windows.GetCurrentThreadId() == thread {
		if tracking && c.Items != nil {
			return ErrMenuOpen
		}
		return applyWindowsUpdate(a, c)
	}
	token, r := winUpdates.add(ctx, c, a)
	if postMessageW(h, trayUpdateMsg, token, 0) == 0 {
		winUpdates.take(token)
		return ErrUnavailable
	}
	return winUpdates.wait(token, r)
}
func applyQueuedWindowsUpdate(token uintptr) {
	// TrackPopupMenu runs a nested loop. Do not destroy a menu it is using.
	winUpdates.Lock()
	r := winUpdates.pending[token]
	winUpdates.Unlock()
	if r == nil {
		return
	}
	mu.Lock()
	tracking := menuOpen
	mu.Unlock()
	if tracking && r.changes.Items != nil {
		deferredUpdates = append(deferredUpdates, token)
		return
	}
	r = winUpdates.take(token)
	if r == nil {
		return
	}
	if err := r.ctx.Err(); err != nil {
		r.result <- err
		return
	}
	r.result <- applyWindowsUpdate(r.source, r.changes)
}
func applyWindowsUpdate(a *availabilitySource, c Changes) error {
	mu.Lock()
	if !running || winAvailability != a {
		mu.Unlock()
		return ErrNotRunning
	}
	cfg := applyChanges(winConfig, c)
	n, old, oldOwned := nid, hicon, hiconOwned
	mu.Unlock()
	data := cfg.Icon
	if isSystemDarkMode() && len(cfg.DarkModeIcon) > 0 {
		data = cfg.DarkModeIcon
	}
	icon := uintptr(0)
	owned := false
	if len(data) > 0 {
		var err error
		icon, err = pngToHICON(data)
		if err != nil {
			return err
		}
		owned = true
	} else {
		icon = loadIconW(0, idiApplication)
	}
	n.hIcon = icon
	n.szTip = [128]uint16{}
	text := cfg.Tooltip
	setTip(&n, text)
	operation := uint32(nimModify)
	a.mu.Lock()
	registered := a.state.Registered
	a.mu.Unlock()
	if !registered {
		operation = nimAdd
	}
	ok := shellNotifyIconW(operation, &n) != 0
	if ok && operation == nimAdd {
		versionTrayIcon(n)
	}
	mu.Lock()
	nid = n
	hicon = icon
	hiconOwned = owned
	winConfig = cfg
	iconPNG = cfg.Icon
	iconDark = cfg.DarkModeIcon
	tip = text
	oldMenu := hMenu
	mu.Unlock()
	if c.Items != nil {
		cmdMu.Lock()
		cmdByID = make(map[uintptr]*cmdEntry)
		cmdMu.Unlock()
		menu := uintptr(0)
		if len(cfg.Items) > 0 {
			menu = buildMenu(cfg.Items)
		}
		mu.Lock()
		hMenu = menu
		mu.Unlock()
		if oldMenu != 0 {
			destroyMenu(oldMenu)
		}
	}
	if oldOwned && old != 0 && old != icon {
		destroyIcon(old)
	}
	publishWindowsAvailability(a, ok)
	if !ok {
		return ErrUnavailable
	}
	return nil
}

// currentIconPNG picks the icon PNG for the current system theme.
func currentIconPNG() []byte {
	if isSystemDarkMode() && len(iconDark) > 0 {
		return iconDark
	}
	return iconPNG
}

// isSystemDarkMode reports whether Windows is in dark theme by reading
// HKCU\...\Personalize\SystemUsesLightTheme (0 = dark). Light is assumed when
// the value cannot be read (pre-Windows 10 1809).
func isSystemDarkMode() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = key.Close() }()
	val, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	if err != nil {
		return false
	}
	return val == 0
}

// showMenu pops the context menu at the cursor and dispatches the chosen item,
// toggling checkbox marks and then running the item's OnClick.
func showMenu(hwnd uintptr) {
	mu.Lock()
	if hwnd != trayHwnd {
		mu.Unlock()
		return
	}
	menu := hMenu
	if menu != 0 {
		menuOpen = true
	}
	fns := map[uintptr]*cmdEntry{}
	cmdMu.Lock()
	for id, e := range cmdByID {
		fns[id] = e
	}
	cmdMu.Unlock()
	mu.Unlock()
	if menu == 0 {
		return
	}

	var pt point
	getCursorPos(&pt)
	setForegroundWindow(hwnd)
	cmd := trackPopupMenu(menu, tpmLeftAlign|tpmRightButton|tpmReturnCmd|tpmNoNotify, pt.X, pt.Y, 0, hwnd, 0)
	postMessageW(hwnd, wmNull, 0, 0)
	mu.Lock()
	menuOpen = false
	closing := closeAfterMenu
	closeAfterMenu = false
	mu.Unlock()
	if closing {
		destroyWindow(hwnd)
		deferredUpdates = nil
		return
	}
	// Dispatch the command from the snapshot before replacing its native menu.
	defer func() {
		queued := deferredUpdates
		deferredUpdates = nil
		for _, token := range queued {
			applyQueuedWindowsUpdate(token)
		}
	}()
	if cmd <= 0 {
		return
	}
	entry, ok := fns[uintptr(cmd)]
	if !ok {
		return
	}
	if entry.checkbox {
		entry.checked = !entry.checked
		setItemChecked(entry.hmenu, entry.pos, entry.checked)
	}
	if entry.onClick != nil {
		entry.onClick()
	}
}

// setItemChecked toggles MF_CHECKED on the menu item at pos within hmenu.
func setItemChecked(hmenu uintptr, pos uint32, checked bool) {
	if hmenu == 0 {
		return
	}
	mii := menuItemInfoW{cbSize: uint32(unsafe.Sizeof(menuItemInfoW{}))} // #nosec G115 -- fixed size
	mii.fMask = miiState
	if checked {
		mii.fState = mfsCheckedFlag
	}
	setMenuItemInfoW(hmenu, uintptr(pos), byPosition, &mii)
}

// buildMenu creates the popup hierarchy (submenus via MF_POPUP) and records
// each clickable item's command id and checkbox state.
func buildMenu(items []Item) uintptr {
	h := createPopupMenu()
	buildMenuItems(h, items, 0)
	return h
}

// buildMenuItems appends items to hmenu, recursing into submenus, and fills
// cmdByID for dispatch. depth > 0 marks a nested submenu (Windows supports one
// level via MF_POPUP; deeper nesting is flattened onto that level).
func buildMenuItems(hmenu uintptr, items []Item, depth int) {
	pos := uint32(0)
	for _, it := range items {
		if it.Separator {
			appendMenuW(hmenu, mfSeparator, 0, nil)
			pos++
			continue
		}
		if it.Submenu != nil {
			label := utf16Ptr(it.Label)
			flags := uint32(mfString | mfPopup)
			if it.Disabled {
				flags |= mfGrayed
			}
			sub := createPopupMenu()
			buildMenuItems(sub, it.Submenu, depth+1)
			appendMenuW(hmenu, flags, sub, label)
			pos++
			continue
		}
		label := utf16Ptr(it.Label)
		flags := uint32(mfString)
		checked := it.Checked
		if it.Disabled {
			flags |= mfGrayed
		}
		if checked {
			flags |= mfChecked
		}
		cmdMu.Lock()
		cmdNext++
		id := cmdNext
		cmdByID[id] = &cmdEntry{
			onClick:  it.OnClick,
			checkbox: it.Checkbox,
			checked:  checked,
			hmenu:    hmenu,
			pos:      pos,
		}
		cmdMu.Unlock()
		appendMenuW(hmenu, flags, id, label)
		pos++
	}
}

// set creates the hidden helper window, the icon and the menu without owning
// the message loop (Set). It must be called from the UI thread.
func set(cfg Config) error {
	if cfg.Tooltip == "" {
		cfg.Tooltip = cfg.Title
	}
	mu.Lock()
	if running {
		mu.Unlock()
		return ErrAlreadyRunning
	}
	err := ensureInit()
	if err != nil {
		mu.Unlock()
		return err
	}
	running = true
	ownsLoop = false
	ownerThread = windows.GetCurrentThreadId()
	mu.Unlock()

	hInst := getModuleHandleW(nil)
	hwnd := createWindowExW(0, classNamePtr, utf16Ptr("native tray"), 0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		mu.Lock()
		running = false
		mu.Unlock()
		return errors.New("tray: CreateWindowExW failed")
	}

	mu.Lock()
	trayClickFn = cfg.OnClick
	trayDblClickFn = cfg.OnDoubleClick
	trayRightClickFn = cfg.OnRightClick
	winConfig = cfg
	iconPNG = cfg.Icon
	iconDark = cfg.DarkModeIcon
	tip = cfg.Tooltip
	if tip == "" {
		tip = cfg.Title
	}
	cmdByID = map[uintptr]*cmdEntry{}
	cmdNext = 0
	mu.Unlock()

	// Icon: theme-aware PNG, falling back to the application icon.
	png := currentIconPNG()
	hic := uintptr(0)
	if len(png) > 0 {
		hic, err = pngToHICON(png)
		if err != nil {
			hic = 0
		}
	}
	owned := hic != 0
	if hic == 0 {
		hic = loadIconW(0, idiApplication)
	}

	hmenu := uintptr(0)
	if len(cfg.Items) > 0 {
		hmenu = buildMenu(cfg.Items)
	}

	n := notifyIconData{}
	n.cbSize = uint32(unsafe.Sizeof(n)) // #nosec G115 -- fixed struct size (976 on x64)
	n.hWnd = hwnd
	n.uID = 1
	n.uFlags = nifMessage | nifIcon | nifTip
	n.uCallbackMessage = trayCallbackMsg
	n.hIcon = hic
	setTip(&n, tip)
	registered := shellNotifyIconW(nimAdd, &n) != 0
	if registered {
		versionTrayIcon(n)
	}

	mu.Lock()
	trayHwnd = hwnd
	hMenu = hmenu
	hicon = hic
	hiconOwned = owned
	nid = n
	winAvailability = newAvailability(cfg.OnAvailability)
	source := winAvailability
	mu.Unlock()
	// Timers are destroyed with their hidden window and need a serviced loop.
	probing := setTimer(hwnd, 1, 2000, 0) != 0
	mu.Lock()
	winProbeActive = probing
	mu.Unlock()
	publishWindowsAvailability(source, registered)
	return nil
}

// run shows the tray and drives the thread's message loop until Stop is called.
func run(cfg Config) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := set(cfg); err != nil {
		return err
	}
	mu.Lock()
	ownsLoop = true
	mu.Unlock()

	var msg msgStruct
	for {
		r := getMessageW(&msg, 0, 0, 0)
		if r == 0 || r == -1 { // WM_QUIT or error
			break
		}
		translateMessage(&msg)
		dispatchMessageW(&msg)
	}

	removeTray()
	return nil
}

// releaseTrayResources frees the icon and menu handles (called from wmDestroy
// on the window's thread).
func releaseTrayResources() {
	mu.Lock()
	a, n, icon, owned, menu := winAvailability, nid, hicon, hiconOwned, hMenu
	running = false
	ownsLoop = false
	winAvailability = nil
	winProbeActive = false
	trayHwnd = 0
	ownerThread = 0
	hicon = 0
	hiconOwned = false
	hMenu = 0
	winConfig = Config{}
	trayClickFn = nil
	trayDblClickFn = nil
	trayRightClickFn = nil
	mu.Unlock()
	if a != nil {
		a.close()
		winUpdates.close(a)
	}
	shellNotifyIconW(nimDelete, &n)
	if owned && icon != 0 {
		destroyIcon(icon)
	}
	if menu != 0 {
		destroyMenu(menu)
	}
	cmdMu.Lock()
	cmdByID = nil
	cmdMu.Unlock()
}

// DestroyWindow must run on the window's owning thread. Off-thread removal
// is queued to its loop; the loop must still be running to complete teardown.
func removeTray() {
	mu.Lock()
	h, thread := trayHwnd, ownerThread
	tracking := menuOpen
	mu.Unlock()
	if h == 0 {
		return
	}
	if windows.GetCurrentThreadId() == thread && !tracking {
		destroyWindow(h)
		return
	}
	postMessageW(h, wmClose, 0, 0)
}
func stop()   { removeTray() }
func remove() { removeTray() }

// pngToHICON converts raw PNG bytes to an HICON (CreateIconFromResourceEx
// accepts PNG on Vista+), sized to the small-icon metric.
func pngToHICON(png []byte) (uintptr, error) {
	if len(png) == 0 {
		return 0, errors.New("tray: empty PNG data")
	}
	cx := getSystemMetrics(smCXSmIcon)
	cy := getSystemMetrics(smCYSmIcon)
	if cx == 0 {
		cx = 16
	}
	if cy == 0 {
		cy = 16
	}
	h := createIconFromResourceEx(&png[0], uint32(len(png)), 1, 0x00030000, cx, cy, lrDefaultColor)
	if h == 0 {
		return 0, fmt.Errorf("tray: CreateIconFromResourceEx failed for %d-byte PNG", len(png))
	}
	return h, nil
}

// setTip copies s into the fixed szTip buffer, NUL-terminated and truncated to
// fit. An embedded NUL (invalid) leaves the tip empty.
func setTip(n *notifyIconData, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	m := len(u)
	if m > len(n.szTip) {
		m = len(n.szTip)
	}
	copy(n.szTip[:m], u[:m])
	n.szTip[len(n.szTip)-1] = 0
}

// utf16Ptr returns a NUL-terminated UTF-16 pointer for s ("" on an embedded NUL,
// never nil).
func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}

// bounds reports the tray icon's on-screen rectangle via
// Shell_NotifyIconGetRect; zeros when the shell cannot provide it.
func bounds() (x, y, w, h int) {
	mu.Lock()
	hw := trayHwnd
	mu.Unlock()
	if hw == 0 {
		return 0, 0, 0, 0
	}
	id := notifyIconIdentifier{
		cbSize: uint32(unsafe.Sizeof(notifyIconIdentifier{})), // #nosec G115 -- fixed size
		hWnd:   hw,
		uID:    1,
	}
	var rc winRect
	if shellNotifyIconGetRect(&id, &rc) != 0 { // S_OK == 0
		return 0, 0, 0, 0
	}
	return int(rc.left), int(rc.top), int(rc.right - rc.left), int(rc.bottom - rc.top)
}
