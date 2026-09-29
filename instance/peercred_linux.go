package instance

import "golang.org/x/sys/unix"

// peerUID returns the user id of the process connected to the Unix socket fd,
// as SO_PEERCRED recorded it when that process connected.
func peerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}
