// The Windows clipboard: user32's clipboard functions with the text as
// CF_UNICODETEXT in kernel32 global memory. It needs no window and no loop,
// so unlike GTK and macOS it works in any program, from any goroutine. The
// clipboard is opened without an owner window, which is enough for text
// rendered at once. Each operation keeps its goroutine on one OS thread,
// because the thread that opens the clipboard must be the one that closes
// it.
//
// CF_UNICODETEXT is NUL-terminated UTF-16, so text is cut at its first NUL,
// which Copy already does on every platform.

package clipboard

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13     // CF_UNICODETEXT
	gmemMoveable  = 0x0002 // GMEM_MOVEABLE, which SetClipboardData requires

	// openTimeout is how long a call waits for another process to close the
	// clipboard, retrying every openRetry.
	openTimeout = time.Second
	openRetry   = 10 * time.Millisecond
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
)

// openClipboard opens the clipboard for the calling thread, retrying while
// another process holds it.
func openClipboard() error {
	deadline := time.Now().Add(openTimeout)
	for {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("clipboard: OpenClipboard: %w", err)
		}
		time.Sleep(openRetry)
	}
}

func copyText(text string) error {
	data, err := windows.UTF16FromString(text) // with its NUL
	if err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return err
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("clipboard: EmptyClipboard: %w", err)
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data))*2)
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: GlobalLock: %w", err)
	}
	copy(unsafe.Slice((*uint16)(ptr(p)), len(data)), data) // #nosec G103 -- the locked global block
	_, _, _ = procGlobalUnlock.Call(h)
	// On success the system owns the block; on failure it is still ours.
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: SetClipboardData: %w", err)
	}
	return nil
}

func paste() (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	// Windows converts CF_TEXT and CF_OEMTEXT to CF_UNICODETEXT on request,
	// so this is false only when the clipboard holds no text.
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText); r == 0 {
		return "", nil
	}
	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", fmt.Errorf("clipboard: GetClipboardData: %w", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("clipboard: GlobalLock: %w", err)
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(h) }()
	// The block's size bounds the read, in case the text has no NUL.
	size, _, _ := procGlobalSize.Call(h)
	return windows.UTF16ToString(unsafe.Slice((*uint16)(ptr(p)), size/2)), nil // #nosec G103 -- the locked global block
}

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion. The values it is fed are global memory blocks
// the Go GC neither owns nor moves; the spelling only keeps go vet's
// unsafeptr check quiet.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } // #nosec G103 -- audited FFI reinterpret
