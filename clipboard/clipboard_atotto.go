//go:build windows

// Windows still goes through github.com/atotto/clipboard, which uses the
// Win32 clipboard.

package clipboard

import "github.com/atotto/clipboard"

func copyText(text string) error { return clipboard.WriteAll(text) }

func paste() (string, error) { return clipboard.ReadAll() }
