//go:build unix

// Unix backend, macOS included: flock on a lock file is the single-instance
// lock, and a Unix socket beside it carries the forwarded arguments.

package instance

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// runtimeDir picks a per-user directory for the lock and socket.
// XDG_RUNTIME_DIR is the right place on Linux; elsewhere the temp dir is the
// portable fallback. A set-but-unusable runtime dir falls back to the temp dir
// too: containers and sandboxes can mount XDG_RUNTIME_DIR read-only, and the
// single-instance lock must not fail the whole app start because the runtime
// dir cannot hold a file. The choice is cached for the process lifetime so all
// instances of one application agree on where the lock lives.
var (
	runtimeDirOnce sync.Once
	runtimeDirPath string
)

func runtimeDir() string {
	runtimeDirOnce.Do(func() {
		runtimeDirPath = os.TempDir()
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && isWritableDir(d) {
			runtimeDirPath = d
		}
	})
	return runtimeDirPath
}

// isWritableDir reports whether dir accepts new files right now, by creating
// and removing a probe file inside it. CreateTemp fails on a missing or
// read-only directory.
func isWritableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".tuohi-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func paths(id string) (lock, sock string) {
	stem := filepath.Join(runtimeDir(), "native-si-"+key(id))
	return stem + ".lock", stem + ".sock"
}

func acquire(id string, onMessage func(Message)) (*Lock, error) {
	lockPath, sockPath := paths(id)
	// lockPath is runtimeDir() + a sha256 hex of id, so it cannot traverse out.
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
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	go serve(ln, onMessage)

	return &Lock{release: func() error {
		_ = ln.Close() // unblocks the Accept loop and unlinks the socket
		_ = os.Remove(sockPath)
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		err := f.Close()
		_ = os.Remove(lockPath)
		return err
	}}, nil
}

func serve(ln net.Listener, onMessage func(Message)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on Release
		}
		go func() {
			defer func() { _ = conn.Close() }()
			data, err := io.ReadAll(conn)
			if err != nil {
				return
			}
			var m Message
			if json.Unmarshal(data, &m) == nil && onMessage != nil {
				onMessage(m)
			}
		}()
	}
}

func send(id string, args []string) error {
	_, sockPath := paths(id)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return err // no instance listening (dial refused / socket missing)
	}
	defer func() { _ = conn.Close() }()
	data, err := json.Marshal(Message{Args: args})
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}
