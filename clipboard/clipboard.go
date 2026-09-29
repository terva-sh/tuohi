// Package clipboard reads and writes the system clipboard as text.
//
//	if err := clipboard.Copy("hello"); err != nil {
//		log.Print(err)
//	}
//	text, err := clipboard.Paste()
//
// It is text-only: Copy and Paste carry UTF-8 strings, and whatever else is
// on the clipboard, such as an image, is not reachable through it.
//
// For now it wraps github.com/atotto/clipboard, so it needs what atotto
// needs: on Linux one of xclip, xsel or wl-copy/wl-paste on PATH, on macOS
// pbcopy and pbpaste, which the system ships. Windows uses the Win32
// clipboard directly. Without a tool on Linux, Copy and Paste return an
// error. Both are safe to call from any goroutine.
package clipboard

import "github.com/atotto/clipboard"

// Copy puts text on the system clipboard, replacing what was there.
func Copy(text string) error {
	return clipboard.WriteAll(text)
}

// Paste returns the text on the system clipboard. An empty clipboard is ""
// with a nil error.
func Paste() (string, error) {
	return clipboard.ReadAll()
}
