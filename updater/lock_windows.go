package updater

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

type installLock struct{ file *os.File }

func lockInstall(target string) (*installLock, error) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".tuohi-update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return &installLock{f}, nil
}
func (l *installLock) Close() error { return l.file.Close() }
