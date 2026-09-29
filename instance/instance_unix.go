//go:build unix

// Unix backend, macOS included: flock on a lock file is the single-instance
// lock, and a Unix socket beside it carries the forwarded arguments. Both live
// in a directory only this user can use, and the running instance also checks
// the user at the other end of each connection where the system reports it.

package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// dirName is the directory, inside the per-user base, that holds the lock and
// socket of every application using this package. Each application's names
// inside it come from key, so they do not collide.
const dirName = "tuohi"

// ioTimeout bounds how long the running instance waits for a sender to finish
// its message, and how long Send waits to connect and write. A launch writes
// its message at once, so the only sender that takes longer is one that has
// stalled. It is a variable so that tests can shorten it.
var ioTimeout = 5 * time.Second

// errNoPeerCred is returned by peerUID where the system cannot report the
// user at the other end of a Unix socket.
var errNoPeerCred = errors.New("instance: this system does not report a socket peer's user")

// dir returns the directory for the lock and socket, creating it when it is
// missing. XDG_RUNTIME_DIR is the right place on Linux, and is used when it is
// an absolute path to a directory that passes checkDir and can hold ours: a
// container can mount it read-only, or a session started with su can inherit
// another user's. Otherwise the base is the user's cache directory,
// ~/.cache on Linux and the BSDs and ~/Library/Caches on macOS. macOS's
// per-user TMPDIR would also do, but the system deletes files there that have
// not been touched for days, and a deleted lock file lets a second process
// become primary while the first still runs. The shared temporary directory is
// never used, because another user can create the names there first.
func dir() (string, error) {
	uid := os.Getuid()
	if base := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(base) && checkDir(base, uid) == nil {
		if d, err := makeDir(filepath.Join(base, dirName), uid); err == nil {
			return d, nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("instance: no per-user directory for the lock: %w", err)
	}
	return makeDir(filepath.Join(base, dirName), uid)
}

// makeDir creates d with mode 0700, and its parent when that is missing, and
// then checks d with checkDir, so a directory that was already there is used
// only when it is as private as one this function creates.
func makeDir(d string, uid int) (string, error) {
	if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
		return "", err
	}
	switch err := os.Mkdir(d, 0o700); {
	case err == nil:
		// The umask can clear bits Mkdir asked for; the owner needs all three.
		if err := os.Chmod(d, 0o700); err != nil {
			return "", err
		}
	case !errors.Is(err, fs.ErrExist):
		return "", err
	}
	if err := checkDir(d, uid); err != nil {
		return "", err
	}
	return d, nil
}

// checkDir returns an error unless path is a directory, rather than a
// symbolic link to one, that belongs to uid and grants nothing to group or
// others. Nobody else can then create, replace or connect to what is inside.
func checkDir(path string, uid int) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("instance: %s is not a directory", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("instance: cannot read the owner of %s", path)
	}
	if int(st.Uid) != uid {
		return fmt.Errorf("instance: %s belongs to uid %d, not %d", path, st.Uid, uid)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("instance: %s has mode %#o, which is open to group or others", path, perm)
	}
	return nil
}

// paths returns the lock file and socket for id. A Unix socket's path must fit
// in sockaddr_un, 108 bytes on Linux and 104 on macOS and the BSDs counting
// the terminating NUL, so a longer one is an error here rather than an
// unexplained one from the kernel.
func paths(id string) (lock, sock string, err error) {
	d, err := dir()
	if err != nil {
		return "", "", err
	}
	stem := filepath.Join(d, key(id))
	lock, sock = stem+".lock", stem+".sock"
	if limit := len(syscall.RawSockaddrUnix{}.Path) - 1; len(sock) > limit {
		return "", "", fmt.Errorf("instance: socket path %s is %d bytes, over the %d-byte limit", sock, len(sock), limit)
	}
	return lock, sock, nil
}

func acquire(id string, onMessage func(Message)) (*Lock, error) {
	lockPath, sockPath, err := paths(id)
	if err != nil {
		return nil, err
	}
	// lockPath is dir() + a sha256 hex of id, so it cannot traverse out.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304
	if err != nil {
		return nil, err
	}
	// A file descriptor is a small non-negative int, so the conversion is safe.
	fd := int(f.Fd()) // #nosec G115
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}

	// We hold the lock: we are the primary. A previous primary that crashed may
	// have left a stale socket file; since we hold the lock, removing it is safe.
	_ = os.Remove(sockPath)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	go serve(ln, onMessage, ioTimeout)

	return &Lock{release: func() error {
		// Closing the listener unblocks the Accept loop and removes the socket.
		// The lock file stays. A launcher that opened it before a removal could
		// still lock that file once it is released, while another creates and
		// locks a new one at the same path, and both would be primary.
		_ = ln.Close()
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		return f.Close()
	}}, nil
}

func serve(ln *net.UnixListener, onMessage func(Message), timeout time.Duration) {
	self := os.Getuid()
	for {
		conn, err := ln.AcceptUnix()
		if err != nil {
			return // listener closed on Release
		}
		go handle(conn, self, timeout, onMessage)
	}
}

// handle reads one Message from conn and passes it to onMessage. A connection
// from another user is closed before anything is read, and one whose sender
// has not finished within timeout is closed without a message.
func handle(conn *net.UnixConn, self int, timeout time.Duration, onMessage func(Message)) {
	defer func() { _ = conn.Close() }()
	if uid, err := connPeerUID(conn); !peerAllowed(uid, err, self) {
		return
	}
	if conn.SetReadDeadline(time.Now().Add(timeout)) != nil {
		return
	}
	if m, ok := readMessage(conn); ok && onMessage != nil {
		onMessage(m)
	}
}

// connPeerUID returns the user id of the process at the other end of conn.
func connPeerUID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid, uidErr := -1, error(nil)
	err = raw.Control(func(fd uintptr) {
		uid, uidErr = peerUID(int(fd)) // #nosec G115 -- a descriptor fits an int
	})
	if err != nil {
		return -1, err
	}
	return uid, uidErr
}

// peerAllowed reports whether a connection may hand a message to a process
// running as self, given the uid and error connPeerUID returned for it. Only
// the same user may, and a peer whose user could not be read is refused. Where
// the system cannot report the peer at all, the private directory has already
// kept other users out, so the connection is allowed.
func peerAllowed(uid int, err error, self int) bool {
	if errors.Is(err, errNoPeerCred) {
		return true
	}
	return err == nil && uid == self
}

// readMessage reads one encoded Message from r, which the sender closes once
// it has written it. It reports false for a message over maxMessage, one that
// does not decode, and a read that fails, including one a deadline cut short.
func readMessage(r io.Reader) (Message, bool) {
	data, err := io.ReadAll(io.LimitReader(r, maxMessage+1))
	if err != nil || len(data) > maxMessage {
		return Message{}, false
	}
	var m Message
	if json.Unmarshal(data, &m) != nil {
		return Message{}, false
	}
	return m, true
}

func send(id string, data []byte) error {
	_, sockPath, err := paths(id)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sockPath, ioTimeout)
	if err != nil {
		return err // no instance listening (dial refused / socket missing)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetWriteDeadline(time.Now().Add(ioTimeout)); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}
