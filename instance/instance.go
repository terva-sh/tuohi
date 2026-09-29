// Package instance keeps one process of an application running per user and
// hands the command line of every later launch to it, cgo-free.
//
// The first process to call Acquire with an id becomes the running instance
// and holds a Lock until it calls Release or exits. A later process that calls
// Acquire with the same id gets ErrAlreadyRunning; it then hands its
// arguments to the running instance with Send and exits:
//
//	func main() {
//		lock, err := instance.Acquire("com.example.app", func(m instance.Message) {
//			// Runs on its own goroutine: hand m.Args to the UI thread.
//		})
//		if errors.Is(err, instance.ErrAlreadyRunning) {
//			if err := instance.Send("com.example.app", os.Args[1:]); err != nil {
//				log.Fatal(err)
//			}
//			return
//		}
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer lock.Release()
//		// ... open the window and run the application ...
//	}
//
// The id names the application, so pick one that is stable across releases
// and unique to it, such as a reverse-DNS name. It is hashed into the name of
// the lock, so any non-empty string works.
//
// A forwarded Message is untrusted input. Any process running as the same user
// can send one, and so can a user who launches the program with arguments of
// their choosing: treat Message.Args as you would a command line typed by
// someone else, and validate a path or URL in them before acting on it.
//
// Each platform uses what it already has. On Unix, including macOS, the lock
// is flock on a file in a per-user directory, and the arguments travel over a
// Unix socket beside it. On Windows one named pipe is both: creating its first
// instance is the lock, and the arguments are written to it.
//
// On Unix the channel is closed to other users. The directory is
// XDG_RUNTIME_DIR/tuohi when XDG_RUNTIME_DIR is a directory that only this
// user can use, and tuohi under os.UserCacheDir otherwise; there is no
// fallback to the shared temporary directory, where another user could take
// the names first. Acquire and Send create the directory with mode 0700 and
// refuse one that is a symbolic link, belongs to another user, or is open to
// group or others. The running instance also closes a connection from another
// user where the system reports the peer's user, which Linux, macOS and
// FreeBSD do; elsewhere the private directory is the only check. It reads at
// most 1 MiB per message and gives a sender 5 seconds to finish, so a stalled
// or oversized send is dropped rather than held. None of this stops a process
// running as the same user from sending a message: that is how the user's own
// later launch gets through, and such a process can already do anything the
// user can.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrAlreadyRunning is returned by Acquire when another process holds id.
var ErrAlreadyRunning = errors.New("instance: another instance is already running")

// errEmptyID is returned by Acquire and Send for an empty id.
var errEmptyID = errors.New("instance: empty id")

// maxMessage caps the encoded size of one Message. A megabyte holds any
// command line macOS allows, since its ARG_MAX of 1 MiB counts the
// environment too, and any Windows one, which is at most 32,767 UTF-16 units.
// Only a Linux command line far beyond what a desktop launch passes could
// exceed it, and a cap this size still bounds what a sender can make the
// running instance buffer.
const maxMessage = 1 << 20

// Message is what a later launch hands the running instance.
type Message struct {
	// Args are the later launch's command-line arguments, without the program
	// name.
	Args []string `json:"args"`

	// Dir is the later launch's working directory, against which a relative
	// path in Args is resolved. It is empty when the later launch could not
	// read its working directory. Like Args, it is untrusted input.
	Dir string `json:"dir"`
}

// Lock is a held single-instance lock. Get one from Acquire.
type Lock struct {
	once    sync.Once
	relErr  error
	release func() error
}

// Acquire makes this process the single instance identified by id, returning
// the held Lock. It returns ErrAlreadyRunning when another process holds id; a
// second Acquire of the same id from this process is refused the same way
// until the first Lock is released. onMessage, when non-nil, receives each
// Message a later launch sends, on its own goroutine, so hand its contents to
// the UI thread before touching UI state. An empty id is an error.
func Acquire(id string, onMessage func(Message)) (*Lock, error) {
	if id == "" {
		return nil, errEmptyID
	}
	return acquire(id, onMessage)
}

// Release relinquishes the lock and stops listening for messages. It is safe
// to call more than once: later calls do nothing and return the first call's
// result.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.relErr = l.release() })
	return l.relErr
}

// Send delivers args to the instance running under id, for use after Acquire
// returned ErrAlreadyRunning, together with this process's working directory
// as Message.Dir. It returns an error when no instance is listening, and when
// the encoded message is over the 1 MiB the running instance accepts. An empty
// id is an error.
func Send(id string, args []string) error {
	if id == "" {
		return errEmptyID
	}
	dir, _ := os.Getwd()
	data, err := json.Marshal(Message{Args: args, Dir: dir})
	if err != nil {
		return err
	}
	if len(data) > maxMessage {
		return fmt.Errorf("instance: message is %d bytes, over the %d-byte limit", len(data), maxMessage)
	}
	return send(id, data)
}

// key derives a short, filesystem- and pipe-name-safe key from an arbitrary
// id, so the lock, socket and pipe names stay bounded in length and
// collision-free.
func key(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}
