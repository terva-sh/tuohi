//go:build !linux && !darwin && !windows && !freebsd && !netbsd

package updater

func watchProcess(int) (processWatch, error) { return nil, ErrUnsupported }
