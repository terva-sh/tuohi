// Package clipboard reads and writes the system clipboard's text, and reads
// its image.
//
//	if err := clipboard.Copy("hello"); err != nil {
//		log.Print(err)
//	}
//	text, err := clipboard.Paste()
//	png, err := clipboard.PasteImage(clipboard.ImageLimits{MaxBytes: 16 << 20})
//
// Copy and Paste carry UTF-8 strings. Copy cuts text at its first NUL byte,
// because the native clipboards take C strings, and replaces invalid UTF-8
// with U+FFFD. PasteImage returns the clipboard's image as PNG, whatever
// format the clipboard offered it in, within the limits the caller sets; see
// its documentation for the formats it reads. It reads on GTK only for now,
// and returns ErrUnsupported on macOS and Windows.
//
// No external program runs. On Linux, FreeBSD and NetBSD it calls the GTK
// that tuohi's window already loaded, gtk_clipboard_* on GTK 3 and
// gdk_clipboard_* on GTK 4, on macOS the general NSPasteboard, and on
// Windows the Win32 clipboard. GTK and macOS have three costs:
//
//   - The clipboard works only once tuohi has started its toolkit, which is
//     when a tuohi App shows its first window. A program that never opens a
//     window has no clipboard, and every call returns ErrNoApp. Off the UI
//     thread they also need the app's loop to be running (App.Wait).
//   - On GTK the process serves the text it copied. On Wayland, and on X11
//     without a clipboard manager, the text is gone when the process exits.
//     Wayland may also refuse to set the clipboard unless one of the
//     process's windows had recent input.
//   - GTK 4 reads the clipboard asynchronously, so Paste and PasteImage wait
//     on a round trip through the UI thread, which keeps running its loop
//     meanwhile.
//   - On GTK a read waits for the process that owns the clipboard to answer,
//     with no timeout of its own, and GTK does not bound every transfer. An
//     owner that never answers keeps Paste or PasteImage waiting, so a
//     caller that must answer within a deadline calls them from a goroutine
//     it can abandon.
//
// Windows needs no tuohi window: the clipboard works in any program, and Copy
// opens it with a hidden window of its own for the call. When another
// process holds the clipboard open, Copy and Paste retry for up to a second.
// Other platforms return ErrUnsupported.
//
// Every function is safe to call from any goroutine. On GTK and macOS the
// clipboard is reached on the UI thread: in place when the caller is on it,
// otherwise marshalled there, with the caller waiting.
package clipboard

import (
	"errors"
	"strings"
)

// ErrNoApp is returned on GTK and macOS, where the clipboard needs tuohi's
// toolkit, when no tuohi App is running one: none has shown a window yet, or
// its loop has stopped and the caller is not on the UI thread.
var ErrNoApp = errors.New("clipboard: needs a running tuohi app, which starts the toolkit when it shows its first window")

// ErrUnsupported is returned on a platform with no clipboard backend, and
// by PasteImage on macOS and Windows, which do not read images yet.
var ErrUnsupported = errors.New("clipboard: not supported on this platform")

// Copy puts text on the system clipboard, replacing what was there.
func Copy(text string) error {
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	return copyText(strings.ToValidUTF8(text, "�"))
}

// Paste returns the text on the system clipboard. A clipboard that holds no
// text, empty or holding only an image, is "" with a nil error.
func Paste() (string, error) {
	return paste()
}
