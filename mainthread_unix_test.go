//go:build linux || freebsd || netbsd

package tuohi

import (
	"errors"
	"runtime"
	"testing"
)

// TestFirstShowOffMainThread checks that, before any window exists, the UI
// thread check refuses a thread other than the main one: WebKitGTK 2.54
// aborts the process when it is first used there
// (TKT-01M3W9PT4ZEDH0SVD645JW23AA). Once the GUI scenarios have pinned the
// UI thread there is nothing left to check.
func TestFirstShowOffMainThread(t *testing.T) {
	if uiThreadPinned() {
		t.Skip("a window already pinned the UI thread")
	}
	errc := make(chan error)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		errc <- uiThreadErr()
	}()
	if err := <-errc; !errors.Is(err, ErrNotMainThread) {
		t.Fatalf("uiThreadErr off the main thread = %v, want ErrNotMainThread", err)
	}
	if osThreadID() == mainThreadID {
		t.Fatal("a test goroutine reports the main thread's ID")
	}
}
