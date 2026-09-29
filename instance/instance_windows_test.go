//go:build windows

package instance

import (
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// rawConnect connects to id's pipe directly, as a sender Send does not model.
func rawConnect(t *testing.T, id string) *os.File {
	t.Helper()
	sid, err := currentUser()
	if err != nil {
		t.Fatal(err)
	}
	path := pipeName(id, sid)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	f := os.NewFile(uintptr(h), path)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestPipeSecurity: the pipe belongs to the current user, and its access list
// has one entry, which is for that user.
func TestPipeSecurity(t *testing.T) {
	id := uniqueID("security")
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = lock.Release() }()
	sid, err := currentUser()
	if err != nil {
		t.Fatal(err)
	}

	f := rawConnect(t, id)
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_KERNEL_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("GetSecurityInfo: %v", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || !owner.Equals(sid) {
		t.Fatalf("pipe owner = %v, %v; want %v", owner, err, sid)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("pipe DACL = %v, %v", dacl, err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("pipe DACL has %d entries, want 1: %s", dacl.AceCount, sd)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if got := (*windows.SID)(unsafe.Pointer(&ace.SidStart)); ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !got.Equals(sid) {
		t.Fatalf("pipe DACL entry is type %d for %v, want an allow entry for %v", ace.Header.AceType, got, sid)
	}
}

// TestPipeNameIsPerUser: the pipe name carries the user, so two users running
// the same application on one machine do not share a pipe.
func TestPipeNameIsPerUser(t *testing.T) {
	sid, err := currentUser()
	if err != nil {
		t.Fatal(err)
	}
	if name := pipeName("com.example.app", sid); !strings.HasSuffix(name, "-"+sid.String()) {
		t.Fatalf("pipeName = %s, want it to end with the user's SID %s", name, sid)
	}
}

// TestNameTaken: only the errors that mean the name is held read as another
// instance running.
func TestNameTaken(t *testing.T) {
	for _, err := range []error{windows.ERROR_ACCESS_DENIED, windows.ERROR_PIPE_BUSY} {
		if !nameTaken(err) {
			t.Errorf("nameTaken(%v) = false, want true", err)
		}
	}
	for _, err := range []error{windows.ERROR_INVALID_NAME, windows.ERROR_INVALID_PARAMETER, windows.ERROR_NOT_ENOUGH_MEMORY} {
		if nameTaken(err) {
			t.Errorf("nameTaken(%v) = true, want false", err)
		}
	}
}

// TestOversizedMessageDropped: a well-formed message over maxMessage is
// dropped, and the next launch's message still arrives.
func TestOversizedMessageDropped(t *testing.T) {
	id := uniqueID("oversized")
	got := acquireCollect(t, id)

	pad := strings.Repeat("x", maxMessage)
	f := rawConnect(t, id)
	_ = f.SetWriteDeadline(time.Now().Add(10 * time.Second))
	// The running instance stops reading at the cap and disconnects, so the
	// tail of the write can fail; the test is what it does with the head.
	_, _ = f.Write([]byte(`{"args":["` + pad + `"]}`))
	_ = f.Close()

	// The instance serves one sender at a time, so this one is read only
	// after the oversized one has been dealt with.
	want := []string{"after"}
	if err := Send(id, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	expectOnly(t, got, want)
}

// TestStalledSenderTimesOut: a sender that connects and writes nothing is
// disconnected once ioTimeout passes, and the next launch, which waits for the
// busy pipe, gets through.
func TestStalledSenderTimesOut(t *testing.T) {
	saved := ioTimeout
	ioTimeout = 500 * time.Millisecond
	t.Cleanup(func() { ioTimeout = saved })

	id := uniqueID("stalled")
	got := acquireCollect(t, id)
	stalled := rawConnect(t, id)

	want := []string{"good"}
	if err := Send(id, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	expectOnly(t, got, want)
	if _, err := stalled.Write([]byte("x")); err == nil {
		t.Fatal("write on the stalled connection succeeded; want it disconnected")
	}
}

// TestReleaseInterruptsStalledSender: Release does not wait out a stalled
// sender's timeout, and the id can be acquired again.
func TestReleaseInterruptsStalledSender(t *testing.T) {
	id := uniqueID("release-stalled")
	lock, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	stalled := rawConnect(t, id)
	start := time.Now()
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if took := time.Since(start); took > ioTimeout/2 {
		t.Fatalf("Release took %v with a stalled sender connected", took)
	}
	_ = stalled.Close()
	again, err := Acquire(id, nil)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = again.Release()
}

// TestCheckOwner: Send refuses a pipe that is not owned by the current user. Another user's pipe cannot be made here without a second account, so
// this checks the refusal against a handle owned by someone else: the
// process's own token, whose owner is the user, is compared with SYSTEM.
func TestCheckOwner(t *testing.T) {
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := currentUser()
	if err != nil {
		t.Fatal(err)
	}
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.READ_CONTROL, &tok); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tok.Close() }()
	sd, err := windows.GetSecurityInfo(windows.Handle(tok), windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if owner.Equals(system) {
		t.Skip("the token is owned by SYSTEM here, so it cannot stand in for another user's object")
	}
	if err := checkOwner(windows.Handle(tok), system); err == nil {
		t.Fatalf("checkOwner accepted an object owned by %v as SYSTEM's", owner)
	}
	if owner.Equals(sid) {
		if err := checkOwner(windows.Handle(tok), sid); err != nil {
			t.Fatalf("checkOwner refused an object owned by the user: %v", err)
		}
	}
}
