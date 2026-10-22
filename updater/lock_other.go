//go:build !linux && !darwin && !windows && !freebsd && !netbsd

package updater

type installLock struct{}

func lockInstall(string) (*installLock, error) { return nil, ErrUnsupported }
func (*installLock) Close() error              { return nil }
