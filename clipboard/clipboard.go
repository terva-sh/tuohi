// Package clipboard reads and writes the system clipboard as text.
//
//	if err := clipboard.Copy("hello"); err != nil {
//		log.Print(err)
//	}
//	text, err := clipboard.Paste()
//
// It is text-only: Copy and Paste carry UTF-8 strings, and whatever else is
// on the clipboard, such as an image, is not reachable through it. Copy cuts
// text at its first NUL byte, because the native clipboards take C strings,
// and replaces invalid UTF-8 with U+FFFD.
//
// On Linux, FreeBSD and NetBSD it calls the GTK that tuohi's window already
// loaded: gtk_clipboard_* on GTK 3 and gdk_clipboard_* on GTK 4. No external
// program runs. That has three costs:
//
//   - The clipboard works only once tuohi has started its toolkit, which is
//     when a tuohi App shows its first window. A program that never opens a
//     window has no clipboard, and Copy and Paste return ErrNoApp. Off the UI
//     thread they also need the app's loop to be running (App.Wait).
//   - The process serves the text it copied. On Wayland, and on X11 without
//     a clipboard manager, the text is gone when the process exits. Wayland
//     may also refuse to set the clipboard unless one of the process's
//     windows had recent input.
//   - GTK 4 reads the clipboard asynchronously, so Paste waits on a round
//     trip through the UI thread, which keeps running its loop meanwhile.
//
// On macOS and Windows it still wraps github.com/atotto/clipboard: macOS runs
// pbcopy and pbpaste, which the system ships, and Windows uses the Win32
// clipboard directly. Other platforms return ErrUnsupported.
//
// Both functions are safe to call from any goroutine. On GTK the work runs on
// the UI thread: in place when the caller is on it, otherwise marshalled
// there, with the caller waiting.
package clipboard

import (
	"errors"
	"strings"
)

// ErrNoApp is returned by Copy and Paste when the clipboard needs tuohi's
// toolkit and no tuohi App is running one: none has shown a window yet, or
// its loop has stopped and the caller is not on the UI thread.
var ErrNoApp = errors.New("clipboard: needs a running tuohi app, which starts the toolkit when it shows its first window")

// ErrUnsupported is returned by Copy and Paste on a platform with no
// clipboard backend.
var ErrUnsupported = errors.New("clipboard: not supported on this platform")

// Copy puts text on the system clipboard, replacing what was there.
func Copy(text string) error {
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	return copyText(strings.ToValidUTF8(text, "�"))
}

// Paste returns the text on the system clipboard. On GTK and macOS a
// clipboard that holds no text, empty or holding only an image, is "" with a
// nil error; atotto on Windows reports it as an error.
func Paste() (string, error) {
	return paste()
}
