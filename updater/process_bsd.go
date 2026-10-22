//go:build darwin || freebsd || netbsd

package updater

import (
	"context"
	"golang.org/x/sys/unix"
)

type nativeWatch struct{ fd int }

func watchProcess(pid int) (processWatch, error) {
	fd, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	event := unix.Kevent_t{Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	event.Ident = uint64(pid)
	_, err = unix.Kevent(fd, []unix.Kevent_t{event}, nil, nil)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &nativeWatch{fd}, nil
}
func (w *nativeWatch) Close() error { return unix.Close(w.fd) }
func (w *nativeWatch) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		events := make([]unix.Kevent_t, 1)
		timeout := unix.NsecToTimespec(100000000)
		n, err := unix.Kevent(w.fd, nil, events, &timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
}
