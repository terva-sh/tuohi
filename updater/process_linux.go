package updater

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
)

type nativeWatch struct{ fd int }

func watchProcess(pid int) (processWatch, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: stable process watch: %v", ErrUnsupported, err)
	}
	return &nativeWatch{fd}, nil
}
func (w *nativeWatch) Close() error { return unix.Close(w.fd) }
func (w *nativeWatch) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := []unix.PollFd{{Fd: int32(w.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(p, 100)
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
