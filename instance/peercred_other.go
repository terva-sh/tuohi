//go:build unix && !linux && !darwin && !freebsd

package instance

// peerUID returns errNoPeerCred. NetBSD has LOCAL_PEEREID, but
// golang.org/x/sys offers no call for it, and a hand-written getsockopt could
// not be run anywhere this project tests; the other Unix systems are not
// targets. Here the 0700 directory the socket lives in is what keeps other
// users out.
func peerUID(int) (int, error) { return -1, errNoPeerCred }
