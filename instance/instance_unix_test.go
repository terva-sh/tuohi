//go:build unix

package instance

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain runs every test with a private HOME and no XDG_RUNTIME_DIR, so the
// lock files, which are never removed, land in a directory deleted afterwards
// rather than in the user's own.
func TestMain(m *testing.M) {
	home, err := privateHome()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Unsetenv("XDG_CACHE_HOME")
	_ = os.Unsetenv("XDG_RUNTIME_DIR")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// privateHome makes a new 0700 directory to serve as HOME. macOS's per-user
// TMPDIR is long enough that a socket under it and Library/Caches/tuohi would
// pass the 104-byte limit, so the directory is made in /tmp there.
func privateHome() (string, error) {
	parent := ""
	if runtime.GOOS == "darwin" {
		parent = "/tmp"
	}
	return os.MkdirTemp(parent, "ti")
}

// isolate gives one test its own HOME, with XDG_RUNTIME_DIR and XDG_CACHE_HOME
// unset, and returns the home.
func isolate(t *testing.T) string {
	t.Helper()
	home, err := privateHome()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	return home
}

// cacheDir is where the lock and socket go when XDG_RUNTIME_DIR is unusable.
func cacheDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, dirName)
}

// lockDir returns the directory paths chose for id.
func lockDir(t *testing.T, id string) string {
	t.Helper()
	lock, _, err := paths(id)
	if err != nil {
		t.Fatalf("paths: %v", err)
	}
	return filepath.Dir(lock)
}

// TestDirIsPrivateWithoutTempFallback: with no XDG_RUNTIME_DIR the lock goes
// in a 0700 directory under the user's cache directory, never in the shared
// temporary directory.
func TestDirIsPrivateWithoutTempFallback(t *testing.T) {
	isolate(t)
	d := lockDir(t, uniqueID("private"))
	if want := cacheDir(t); d != want {
		t.Fatalf("lock directory = %s, want %s", d, want)
	}
	if d == os.TempDir() {
		t.Fatalf("lock directory is the shared temporary directory %s", d)
	}
	fi, err := os.Lstat(d)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("lock directory mode = %#o, want 0700", perm)
	}
}

// TestXDGRuntimeDir: a private XDG_RUNTIME_DIR holds the lock, and one open to
// group or others is passed over for the cache directory rather than used.
func TestXDGRuntimeDir(t *testing.T) {
	home := isolate(t)
	run := filepath.Join(home, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	id := uniqueID("xdg")
	if d, want := lockDir(t, id), filepath.Join(run, dirName); d != want {
		t.Fatalf("lock directory = %s, want %s", d, want)
	}

	// A fresh XDG_RUNTIME_DIR open to others, holding nothing of ours, is
	// passed over.
	open := filepath.Join(home, "open-run")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", open)
	if d, want := lockDir(t, id), cacheDir(t); d != want {
		t.Fatalf("with XDG_RUNTIME_DIR 0755, lock directory = %s, want %s", d, want)
	}

	t.Setenv("XDG_RUNTIME_DIR", "relative/run")
	if d, want := lockDir(t, id), cacheDir(t); d != want {
		t.Fatalf("with a relative XDG_RUNTIME_DIR, lock directory = %s, want %s", d, want)
	}
}

// TestXDGDirNoFallback: once XDG_RUNTIME_DIR holds our directory, an
// instance may hold its lock there, so a failed check there is an error, not
// a move to the cache directory where a second instance could lock too.
func TestXDGDirNoFallback(t *testing.T) {
	home := isolate(t)
	run := filepath.Join(home, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	id := uniqueID("xdg-no-fallback")
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()
	ours := filepath.Join(run, dirName)
	for _, c := range []struct {
		name string
		path string
		mode os.FileMode
	}{
		{"our directory 0755", ours, 0o755},
		{"XDG_RUNTIME_DIR 0755", run, 0o755},
	} {
		if err := os.Chmod(c.path, c.mode); err != nil {
			t.Fatal(err)
		}
		if _, _, err := paths(id); err == nil {
			t.Errorf("%s: paths succeeded, want an error rather than the cache directory", c.name)
		}
		if second, err := Acquire(id, nil); err == nil {
			_ = second.Release()
			t.Errorf("%s: a second Acquire succeeded", c.name)
		}
		if err := os.Chmod(c.path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAncestorsChecked: a lock directory below a directory other users can
// write to is refused, because they could swap it after the check, unless
// the sticky bit stops them, as on /tmp. The directory is checked as written
// and through a symbolic link.
func TestAncestorsChecked(t *testing.T) {
	home := isolate(t)
	shared := filepath.Join(home, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(shared, "cache")
	link := filepath.Join(home, "cache-link")
	if err := os.Symlink(cache, link); err != nil {
		t.Fatal(err)
	}
	id := uniqueID("ancestors")
	// os.UserCacheDir reads XDG_CACHE_HOME on Linux and the BSDs, and is
	// always $HOME/Library/Caches on macOS.
	setCache := func(base string) {
		if runtime.GOOS == "darwin" {
			t.Setenv("HOME", base)
		} else {
			t.Setenv("XDG_CACHE_HOME", base)
		}
	}
	for _, base := range []string{cache, link} {
		setCache(base)
		if _, _, err := paths(id); err == nil || !strings.Contains(err.Error(), "written by group or others") {
			t.Errorf("cache under a 0777 directory (%s): paths = %v, want it refused", base, err)
		}
	}
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{cache, link} {
		setCache(base)
		if _, _, err := paths(id); err != nil {
			t.Errorf("cache under a sticky 1777 directory (%s): %v", base, err)
		}
	}
	// A directory of a third user, neither us nor root. As root, which CI
	// runs as, make one with chown; as anyone else, name another uid.
	theirs, uid := home, os.Getuid()+1
	if os.Getuid() == 0 {
		theirs, uid = filepath.Join(home, "theirs"), 0
		if err := os.Mkdir(theirs, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(theirs, 4242, 4242); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkAncestor(theirs, uid); err == nil {
		t.Error("checkAncestor accepted a directory of another, non-root uid")
	}
}

// TestOpenDirRefused: an existing lock directory open to group or others is
// refused rather than used or quietly repaired.
func TestOpenDirRefused(t *testing.T) {
	isolate(t)
	d := cacheDir(t)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	id := uniqueID("open-dir")
	if lock, err := Acquire(id, nil); err == nil {
		_ = lock.Release()
		t.Fatalf("Acquire with %s at 0755 succeeded, want it refused", d)
	}
	if err := Send(id, nil); err == nil || !strings.Contains(err.Error(), "open to group or others") {
		t.Fatalf("Send with %s at 0755 = %v, want it refused", d, err)
	}
}

// TestCheckDir covers what cannot be set up without root: a directory that
// belongs to someone else, tested by naming a uid that is not ours. It also
// checks that a symbolic link to a good directory is refused.
func TestCheckDir(t *testing.T) {
	home := isolate(t)
	d := filepath.Join(home, "d")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(d, os.Getuid()); err != nil {
		t.Fatalf("checkDir of our own 0700 directory: %v", err)
	}
	if err := checkDir(d, os.Getuid()+1); err == nil {
		t.Fatal("checkDir accepted a directory that belongs to another uid")
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(d, link); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(link, os.Getuid()); err == nil {
		t.Fatal("checkDir accepted a symbolic link")
	}
}

// TestSocketPathTooLong: a socket path that would not fit in sockaddr_un is a
// clear error naming the limit.
func TestSocketPathTooLong(t *testing.T) {
	home := isolate(t)
	deep := filepath.Join(home, strings.Repeat("h", 110))
	if err := os.Mkdir(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", deep)
	lock, err := Acquire(uniqueID("long"), nil)
	if err == nil {
		_ = lock.Release()
		t.Fatal("Acquire with an over-long socket path succeeded")
	}
	if !strings.Contains(err.Error(), "-byte limit") {
		t.Fatalf("Acquire with an over-long socket path = %v, want the length error", err)
	}
}

// TestLockFileSurvivesRelease: Release removes the socket but leaves the lock
// file, and the lock can be taken again.
func TestLockFileSurvivesRelease(t *testing.T) {
	id := uniqueID("survives")
	lockPath, sockPath, err := paths(id)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file after Release: %v", err)
	}
	if _, err := os.Stat(sockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket after Release: %v, want it removed", err)
	}
	again, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = again.Release()
}

// dial connects to id's socket directly, as a sender Send does not model.
func dial(t *testing.T, id string) *net.UnixConn {
	t.Helper()
	_, sockPath, err := paths(id)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// waitClosed waits for the running instance to close conn from its end.
func waitClosed(t *testing.T, conn *net.UnixConn, within time.Duration) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		t.Fatal(err)
	}
	_, err := io.Copy(io.Discard, conn)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the running instance did not close the connection within %v", within)
	}
}

// TestOversizedMessageDropped: a well-formed message over maxMessage is
// dropped, and the next launch's message still arrives.
func TestOversizedMessageDropped(t *testing.T) {
	id := uniqueID("oversized")
	got := acquireCollect(t, id)

	pad := strings.Repeat("x", maxMessage)
	big := []byte(`{"args":["` + pad + `"]}`)
	conn := dial(t, id)
	// The running instance stops reading at the cap and closes, so the tail of
	// the write can fail; the test is what it does with the head.
	_, _ = conn.Write(big)
	_ = conn.CloseWrite()
	waitClosed(t, conn, 10*time.Second)

	want := []string{"after"}
	if err := Send(id, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	expectOnly(t, got, want)
}

// TestStalledSenderTimesOut: a sender that connects and writes nothing does
// not delay the next launch's message, and is disconnected once ioTimeout
// passes.
func TestStalledSenderTimesOut(t *testing.T) {
	saved := ioTimeout
	ioTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ioTimeout = saved })

	id := uniqueID("stalled")
	got := acquireCollect(t, id)
	stalled := dial(t, id)

	want := []string{"good"}
	if err := Send(id, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	expectOnly(t, got, want)
	waitClosed(t, stalled, 5*time.Second)
}

// TestPeerAllowed checks the decision on a connection's user. A connection
// from another uid cannot be made without root, so the refusal is tested here
// on the decision alone.
func TestPeerAllowed(t *testing.T) {
	const self = 1000
	cases := []struct {
		name string
		uid  int
		err  error
		want bool
	}{
		{"same user", self, nil, true},
		{"other user", self + 1, nil, false},
		{"root", 0, nil, false},
		{"unreadable", -1, errors.New("getsockopt failed"), false},
		{"unreadable but reported our uid", self, errors.New("getsockopt failed"), false},
		{"system cannot report", -1, errNoPeerCred, true},
	}
	for _, c := range cases {
		if got := peerAllowed(c.uid, c.err, self); got != c.want {
			t.Errorf("%s: peerAllowed(%d, %v) = %v, want %v", c.name, c.uid, c.err, got, c.want)
		}
	}
}

// TestPeerUIDOfOwnConnection reads the peer of a real connection, which is
// this process, where the system reports one.
func TestPeerUIDOfOwnConnection(t *testing.T) {
	id := uniqueID("peer")
	_, sockPath, err := paths(id)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	dial(t, id)
	conn, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	uid, err := connPeerUID(conn)
	switch runtime.GOOS {
	case "linux", "android", "darwin", "ios", "freebsd":
		if err != nil || uid != os.Getuid() {
			t.Fatalf("connPeerUID = %d, %v; want %d", uid, err, os.Getuid())
		}
	default:
		if !errors.Is(err, errNoPeerCred) {
			t.Fatalf("connPeerUID = %d, %v; want errNoPeerCred", uid, err)
		}
	}
}
