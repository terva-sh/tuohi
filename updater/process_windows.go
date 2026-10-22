package updater

import (
	"context"
	"golang.org/x/sys/windows"
)

type nativeWatch struct{ handle windows.Handle }

func watchProcess(pid int) (processWatch, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil, err
	}
	return &nativeWatch{h}, nil
}
func (w *nativeWatch) Close() error { return windows.CloseHandle(w.handle) }
func (w *nativeWatch) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := windows.WaitForSingleObject(w.handle, 100)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
	}
}
