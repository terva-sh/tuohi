//go:build darwin || freebsd

package instance

import "golang.org/x/sys/unix"

// peerUID returns the user id of the process connected to the Unix socket fd,
// as LOCAL_PEERCRED recorded it when that process connected.
func peerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}
