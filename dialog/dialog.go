// Package dialog shows the operating system's native open, save and
// choose-directory panels, cgo-free.
//
// A single entry point, Open, presents whichever panel Options.Type selects:
//
//	paths, err := dialog.Open(dialog.Options{
//		Type:  dialog.TypeOpenMultiple,
//		Title: "Open scenes",
//	})
//
// Threading: the panels are platform UI (AppKit, Win32/COM, GTK) and must be
// invoked on the program's main thread. This package does not impose a
// threading model - the caller is responsible for already being on the main
// thread. For example, an Ebitengine app wraps the call in
// ebiten.RunOnMainThread, and a webview host uses its own UI-thread dispatch
// (the appkit View method Dialog does exactly that).
//
// A cancelled panel - and a panel that cannot be shown at all (no backend, or
// no display, for example a Linux process without one) - yields a nil/empty
// result with a nil error. The error return is reserved for an unrecognized
// Options.Type.
package dialog

import (
	"fmt"
	"strings"
)

// Type selects which panel Open presents.
type Type string

const (
	// TypeOpen shows an open-file panel that selects a single file.
	TypeOpen Type = "open"

	// TypeOpenMultiple shows an open-file panel that selects several files.
	TypeOpenMultiple Type = "open-multiple"

	// TypeSave shows a save-file panel. Nothing is written; the panel only
	// picks the destination path.
	TypeSave Type = "save"

	// TypeDirectory shows a directory chooser.
	TypeDirectory Type = "directory"
)

// FileFilter restricts a panel to files of a given kind, shown as a named
// choice in the panel's type dropdown where the platform supports it.
type FileFilter struct {
	// Name is the human-readable label for this filter (e.g. "Images").
	Name string

	// Extensions lists the file extensions WITHOUT the leading dot
	// (e.g. {"png", "jpg"}). An empty list, or an entry "*", matches any file.
	Extensions []string
}

// Options configures a file panel. The zero value is valid: an open-file
// panel rooted at the platform's default directory with no type filtering.
type Options struct {
	// Type selects the panel Open shows. The zero value (empty) means
	// TypeOpen.
	Type Type

	// Title is the prompt shown prominently above the file list.
	Title string

	// Directory is the initial directory, as a filesystem path. Empty uses
	// the platform default (usually the last-used directory).
	Directory string

	// Filename is the suggested file name. Used only by TypeSave.
	Filename string

	// Extensions restricts selectable files to these extensions, given
	// without the leading dot (e.g. {"afoil", "dat"}). Empty, or any
	// "*"/"" entry, allows all files. Ignored by TypeDirectory and when
	// Filters is set.
	Extensions []string

	// Filters is the richer alternative to Extensions: a list of NAMED
	// filter groups. When non-empty it takes precedence over Extensions,
	// and platforms with a per-filter concept (Windows' COMDLG_FILTERSPEC,
	// GTK's GtkFileFilter) show them as separate choices. macOS flattens
	// them, exactly like Extensions.
	Filters []FileFilter
}

// Open presents the panel selected by opts.Type and returns every chosen
// path. The single-item panels - TypeOpen, TypeSave and TypeDirectory -
// return at most one path. Cancelling (or a platform that cannot present the
// panel) yields a nil slice with a nil error; the error is non-nil only for
// an unrecognized Type.
func Open(opts Options) ([]string, error) {
	switch opts.Type {
	case "", TypeOpen:
		return singleResult(open(opts)), nil
	case TypeOpenMultiple:
		return openMultiple(opts), nil
	case TypeSave:
		return singleResult(save(opts)), nil
	case TypeDirectory:
		return singleResult(pickDirectory(opts)), nil
	default:
		return nil, fmt.Errorf("dialog: unsupported dialog type %q", opts.Type)
	}
}

// singleResult wraps a single chosen path as a zero- or one-element slice
// (nil when nothing was picked).
func singleResult(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}

// cleanExtensions normalizes Options.Extensions for the backends: leading
// dots are stripped, and a nil result means "no restriction" (empty input, or
// any ""/"*" wildcard entry).
func cleanExtensions(exts []string) []string {
	clean := make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.TrimPrefix(e, ".")
		if e == "" || e == "*" {
			return nil
		}
		clean = append(clean, e)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// filterExtensions resolves the effective extension restriction of an
// Options, as one flat list: the union of every Filter's extensions when any
// filter is set (macOS's flattened view), otherwise Extensions. nil means
// "no restriction" (any filter may widen it with a wildcard).
func filterExtensions(opts Options) []string {
	var all []string
	for _, f := range opts.Filters {
		all = append(all, f.Extensions...)
	}
	if len(all) > 0 {
		return cleanExtensions(all)
	}
	return cleanExtensions(opts.Extensions)
}

// firstPath returns the first of a panel's chosen paths, or "" when none
// (the single-selection view every backend reduces to).
func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
