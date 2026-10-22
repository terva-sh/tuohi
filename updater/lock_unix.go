//go:build linux || darwin || freebsd || netbsd

package updater

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

type installLock struct{ file *os.File }

func lockInstall(target string) (*installLock, error) {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".tuohi-update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return &installLock{f}, nil
}
func (l *installLock) Close() error { return l.file.Close() }
