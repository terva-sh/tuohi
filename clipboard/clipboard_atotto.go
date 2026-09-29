//go:build darwin || windows

// macOS and Windows still go through github.com/atotto/clipboard: pbcopy and
// pbpaste on macOS, the Win32 clipboard on Windows.

package clipboard

import "github.com/atotto/clipboard"

func copyText(text string) error { return clipboard.WriteAll(text) }

func paste() (string, error) { return clipboard.ReadAll() }
