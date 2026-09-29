package instance

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// uniqueID keeps parallel CI jobs and reruns from colliding on the same lock.
func uniqueID(name string) string {
	return fmt.Sprintf("native-instance-test-%s-%d", name, os.Getpid())
}

// TestAcquireSendRoundTrip exercises the single-instance machinery end to end
// in one process: the first Acquire wins, a second is rejected with
// ErrAlreadyRunning, and Send forwards arguments and its working directory,
// which arrive at the primary's onMessage. flock denies a second lock even from the same process (it is per
// open file description), and a Windows named pipe with
// FILE_FLAG_FIRST_PIPE_INSTANCE likewise rejects the second create - so the
// round trip is fully exercised on CI without spawning a child.
func TestAcquireSendRoundTrip(t *testing.T) {
	id := uniqueID("roundtrip")
	got := make(chan Message, 1)

	lock, err := Acquire(id, func(m Message) {
		select {
		case got <- m:
		default:
		}
	})
	if err != nil {
		t.Fatalf("Acquire (primary): %v", err)
	}
	defer func() { _ = lock.Release() }()

	second, err := Acquire(id, nil)
	if !errors.Is(err, ErrAlreadyRunning) {
		if second != nil {
			_ = second.Release()
		}
		t.Fatalf("second Acquire = %v, want ErrAlreadyRunning", err)
	}

	want := []string{"open", "/tmp/a b.txt", "café ✓"}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := Send(id, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case m := <-got:
		if !slices.Equal(m.Args, want) {
			t.Fatalf("forwarded args = %v, want %v", m.Args, want)
		}
		if m.Dir != wd {
			t.Fatalf("forwarded dir = %q, want %q", m.Dir, wd)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for forwarded args")
	}
}

// TestReleaseAllowsReacquire confirms Release frees the lock so a later
// Acquire succeeds again, and that a second Release is a harmless no-op.
func TestReleaseAllowsReacquire(t *testing.T) {
	id := uniqueID("reacquire")
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
	again, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("re-Acquire after Release: %v", err)
	}
	_ = again.Release()
}

// TestSendWithoutInstance reports an error rather than blocking when nothing is
// listening.
func TestSendWithoutInstance(t *testing.T) {
	id := uniqueID("noinstance")
	if err := Send(id, []string{"x"}); err == nil {
		t.Fatal("Send with no running instance should fail")
	}
}

// TestSendRefusesOversized: Send reports a message the running instance would
// drop for its size, rather than sending it and appearing to succeed.
func TestSendRefusesOversized(t *testing.T) {
	id := uniqueID("oversized-send")
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()
	err = Send(id, []string{strings.Repeat("x", maxMessage)})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("Send of an oversized message = %v, want the size limit error", err)
	}
}

// TestEmptyID pins that an empty id is refused before anything is locked or
// dialled: a single instance needs a stable name to lock on.
func TestEmptyID(t *testing.T) {
	if lock, err := Acquire("", nil); err == nil {
		_ = lock.Release()
		t.Fatal("Acquire with an empty id: expected error")
	}
	if err := Send("", nil); err == nil {
		t.Fatal("Send with an empty id: expected error")
	}
}

// TestNilLockRelease: Release on a nil *Lock does nothing rather than
// panicking, so a deferred Release after a failed Acquire is safe.
func TestNilLockRelease(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Fatalf("nil Lock Release = %v, want nil", err)
	}
}
