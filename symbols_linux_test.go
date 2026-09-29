//go:build linux

package tuohi

import (
	"strings"
	"testing"
)

// TestSymbolsNeed checks that a missing function is recorded, named with its
// library, and left unbound, while a present one is bound and callable. libc
// stands in for the WebKitGTK stack, so the test needs no display.
func TestSymbolsNeed(t *testing.T) {
	libc, err := openFirst("libc.so.6")
	if err != nil {
		t.Skipf("no glibc to load: %v", err)
	}

	var syms symbols
	var strlen func(s string) uintptr
	syms.need(&strlen, libc, "strlen")
	if err := syms.err(); err != nil {
		t.Fatalf("present function reported missing: %v", err)
	}
	if strlen == nil {
		t.Fatal("present function was not bound")
	}
	if n := strlen("tuohi"); n != 5 {
		t.Fatalf("strlen(%q) = %d, want 5", "tuohi", n)
	}

	var absent func()
	syms.need(&absent, libc, "tuohi_no_such_function")
	if absent != nil {
		t.Fatal("missing function was bound")
	}
	err = syms.err()
	if err == nil {
		t.Fatal("missing function not reported")
	}
	for _, want := range []string{"tuohi_no_such_function in libc.so.6", "WebKitGTK 2.40"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "strlen") {
		t.Errorf("error %q names a function that was found", err)
	}
}

// TestRecoverInit checks that recoverInit turns a panic into the returned
// error, and leaves a clean return alone.
func TestRecoverInit(t *testing.T) {
	panics := func() (err error) {
		defer recoverInit(&err)
		panic("symbol gone")
	}
	if err := panics(); err == nil || !strings.Contains(err.Error(), "symbol gone") {
		t.Fatalf("panic returned %v, want an error naming it", err)
	}

	returns := func() (err error) {
		defer recoverInit(&err)
		return nil
	}
	if err := returns(); err != nil {
		t.Fatalf("clean return gave %v, want nil", err)
	}
}
