//go:build windows

// Windows backend: a single named pipe is both the lock and the hand-off
// channel. CreateNamedPipe with FILE_FLAG_FIRST_PIPE_INSTANCE succeeds only for
// the first instance and fails once one exists, so it doubles as the
// single-instance lock; the same pipe then carries the forwarded arguments.
// The pipe grants access to the user who created it and nobody else, is named
// for that user, and a sender checks who owns it before writing. The running
// instance serves one sender at a time with overlapped I/O, so that a read can
// time out and Release can interrupt a wait.

package instance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pipeBufferSize is the pipe's inbound buffer. A message larger than this is
// still delivered; the sender's write just waits for the reader to drain it.
const pipeBufferSize = 64 * 1024

var (
	errStopped = errors.New("instance: stopped")
	errTimeout = errors.New("instance: timed out")
)

// currentUser returns the SID of the user this process runs as.
func currentUser() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

// pipeName names id's pipe for the user sid. Every session on the machine
// shares one pipe namespace, so the user is part of the name: each user who
// runs the application gets a running instance of their own, rather than
// finding another user's pipe and being told one is already running.
func pipeName(id string, sid *windows.SID) string {
	return `\\.\pipe\tuohi-` + key(id) + "-" + sid.String()
}

// nameTaken reports whether a CreateNamedPipe error means another process
// already holds the pipe's name. CreateNamedPipe documents that
// FILE_FLAG_FIRST_PIPE_INSTANCE fails with ERROR_ACCESS_DENIED once the name
// has an instance. ERROR_PIPE_BUSY, which the kernel reports for a pipe at its
// instance limit, is not documented for this case but can only mean the same.
// Any other error is a failure to create the pipe.
func nameTaken(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY)
}

// pipeSecurity returns security attributes that make sid the pipe's owner and
// grant it, and nobody else, full access. The default descriptor would also
// let Everyone and the anonymous account read the pipe and SYSTEM and
// Administrators control it. SYSTEM is left out on purpose: nothing running as
// it has a launch to hand over. The owner is what a sender checks, so it must
// be the user rather than the Administrators group, which an elevated process
// would make the owner by default.
func pipeSecurity(sid *windows.SID) (*windows.SecurityAttributes, error) {
	s := sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + s + "D:P(A;;GA;;;" + s + ")")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}, nil
}

func acquire(id string, onMessage func(Message)) (*Lock, error) {
	sid, err := currentUser()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(pipeName(id, sid))
	if err != nil {
		return nil, err
	}
	sa, err := pipeSecurity(sid)
	if err != nil {
		return nil, err
	}
	// One instance, reused for every sender, so the name stays held from here
	// to Release. A sender that finds it busy retries; see send.
	h, err := windows.CreateNamedPipe(name,
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, 0, pipeBufferSize, 0, sa)
	if err != nil {
		if nameTaken(err) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	s := &pipeServer{h: h, timeout: ioTimeout, onMessage: onMessage}
	if s.stop, err = windows.CreateEvent(nil, 1, 0, nil); err == nil {
		s.ov.HEvent, err = windows.CreateEvent(nil, 1, 0, nil)
	}
	if err != nil {
		_ = s.close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.run()
	}()

	return &Lock{release: func() error {
		// run returns once it sees stop, having cancelled and waited out any
		// pending I/O, so the handles are no longer in use when they close.
		if err := windows.SetEvent(s.stop); err != nil {
			return err
		}
		<-done
		return s.close()
	}}, nil
}

// pipeServer is the running instance's end of the pipe. It is allocated on
// the heap and ov is one of its fields because the kernel writes to ov until
// an overlapped operation finishes, and a goroutine's stack can move.
type pipeServer struct {
	h         windows.Handle
	stop      windows.Handle // event set by Release
	ov        windows.Overlapped
	timeout   time.Duration
	onMessage func(Message)
}

func (s *pipeServer) close() error {
	for _, e := range []windows.Handle{s.stop, s.ov.HEvent} {
		if e != 0 {
			_ = windows.CloseHandle(e)
		}
	}
	return windows.CloseHandle(s.h)
}

func (s *pipeServer) run() {
	for {
		if ev, _ := windows.WaitForSingleObject(s.stop, 0); ev == windows.WAIT_OBJECT_0 {
			return
		}
		err := windows.ConnectNamedPipe(s.h, &s.ov)
		if errors.Is(err, windows.ERROR_IO_PENDING) {
			_, err = s.wait(windows.INFINITE)
		}
		switch {
		case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
			r := &pipeReader{s: s, deadline: time.Now().Add(s.timeout)}
			// Its own goroutine, as on Unix, so a callback that waits on a
			// thread calling Release cannot deadlock the two.
			if m, ok := readMessage(r); ok && s.onMessage != nil {
				go s.onMessage(m)
			}
		case errors.Is(err, windows.ERROR_NO_DATA):
			// The sender connected and left before it was served.
		default:
			// Release, or a failure that would recur on every try. Stop
			// serving, but leave the pipe, which is the lock, to Release.
			return
		}
		_ = windows.DisconnectNamedPipe(s.h)
	}
}

// wait waits up to timeout milliseconds for the overlapped operation pending
// on s.ov and returns its byte count. When Release sets stop first, or the
// time runs out, it cancels the operation and waits for the cancellation to
// finish, so that the kernel is done with s.ov and the buffer, and returns
// errStopped or errTimeout.
func (s *pipeServer) wait(timeout uint32) (uint32, error) {
	ev, err := windows.WaitForMultipleObjects([]windows.Handle{s.ov.HEvent, s.stop}, false, timeout)
	var cut error
	switch {
	case err != nil:
		cut = err
	case ev == windows.WAIT_OBJECT_0:
	case ev == windows.WAIT_OBJECT_0+1:
		cut = errStopped
	default:
		cut = errTimeout
	}
	if cut != nil {
		_ = windows.CancelIoEx(s.h, &s.ov)
	}
	var n uint32
	err = windows.GetOverlappedResult(s.h, &s.ov, &n, true)
	if cut != nil {
		return 0, cut
	}
	return n, err
}

// pipeReader reads what the connected sender writes, for readMessage. A read
// fails once deadline passes or Release is called, and the sender closing its
// end is the end of the message.
type pipeReader struct {
	s        *pipeServer
	deadline time.Time
}

func (r *pipeReader) Read(p []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(r.s.h, p, &n, &r.s.ov)
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		ms := max(time.Until(r.deadline).Milliseconds(), 0)
		n, err = r.s.wait(uint32(ms))
	}
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) {
		return int(n), io.EOF
	}
	return int(n), err
}

// checkOwner returns an error unless the pipe h is connected to is owned by
// sid. The running instance makes its user the owner, and only a process that
// holds the privilege to restore files can make another user the owner of what
// it creates, so a pipe that another user created first under this name fails
// the check, and the message is not handed to them.
func checkOwner(h windows.Handle, sid *windows.SID) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !owner.Equals(sid) {
		return errors.New("instance: the pipe belongs to another user")
	}
	return nil
}

func send(id string, data []byte) error {
	sid, err := currentUser()
	if err != nil {
		return err
	}
	path := pipeName(id, sid)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// The running instance serves one sender at a time and gives each at most
	// ioTimeout, so a busy pipe frees up within that. Wait twice as long before
	// giving up, which also covers one sender queued ahead of this one.
	// SECURITY_IDENTIFICATION lets the server learn who connected but not act
	// as them.
	giveUp := time.Now().Add(2 * ioTimeout)
	var h windows.Handle
	for {
		h, err = windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) || time.Now().After(giveUp) {
			return fmt.Errorf("instance: no running instance to receive the message: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := checkOwner(h, sid); err != nil {
		_ = windows.CloseHandle(h)
		return err
	}
	// os.NewFile adds an overlapped handle to the runtime poller, which is what
	// makes the write deadline work.
	f := os.NewFile(uintptr(h), path)
	defer func() { _ = f.Close() }()
	if err := f.SetWriteDeadline(time.Now().Add(ioTimeout)); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
