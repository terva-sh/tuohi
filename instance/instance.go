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
// is flock on a file in XDG_RUNTIME_DIR (or the temporary directory when that
// is unset or not writable), and the arguments travel over a Unix socket
// beside it. On Windows one named pipe is both: creating its first instance
// is the lock, and the arguments are written to it.
package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// ErrAlreadyRunning is returned by Acquire when another process holds id.
var ErrAlreadyRunning = errors.New("instance: another instance is already running")

// errEmptyID is returned by Acquire and Send for an empty id.
var errEmptyID = errors.New("instance: empty id")

// Message is what a later launch hands the running instance.
type Message struct {
	// Args are the later launch's command-line arguments, without the program
	// name.
	Args []string `json:"args"`
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
// returned ErrAlreadyRunning. It returns an error when no instance is
// listening. An empty id is an error.
func Send(id string, args []string) error {
	if id == "" {
		return errEmptyID
	}
	return send(id, args)
}

// key derives a short, filesystem- and pipe-name-safe key from an arbitrary
// id, so the lock, socket and pipe names stay bounded in length and
// collision-free.
func key(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}
