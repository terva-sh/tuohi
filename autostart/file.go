//go:build linux || freebsd || netbsd || darwin

package autostart

import (
	"io"
	"os"
	"path/filepath"
)

// The autostart support below is derived from Wails v3
// pkg/application/autostart.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// writeFileAtomic writes data to path via a temp file + rename in the same
// directory, so a partial write never leaves a half-formed plist or .desktop
// file in place.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	// os.File.Write reports short writes as errors, but double-check n ==
	// len(data) so a future change of writer type cannot silently rename a
	// truncated artefact into place.
	n, err := tmp.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
