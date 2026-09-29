//go:build windows

// Windows backend: a single named pipe is both the lock and the hand-off
// channel. CreateNamedPipeW with FILE_FLAG_FIRST_PIPE_INSTANCE succeeds only for
// the first instance and fails once one exists, so it doubles as the
// single-instance lock; the same pipe then carries the forwarded arguments.
// Windows has no dlopen, so the kernel32 symbols are resolved with
// LoadLibrary/GetProcAddress and bound with purego.RegisterFunc.

package instance

import (
	"encoding/json"
	"errors"
	"sync"
	"syscall"
	"time"

	"github.com/ebitengine/purego"
)

const (
	pipeAccessInbound         = 0x00000001
	fileFlagFirstPipeInstance = 0x00080000
	pipeWaitByte              = 0x00000000 // PIPE_TYPE_BYTE | PIPE_READMODE_BYTE | PIPE_WAIT
	genericWrite              = 0x40000000
	openExisting              = 3
	pipeInBufferSize          = 64 * 1024
)

// invalidHandle is INVALID_HANDLE_VALUE ((HANDLE)-1).
var invalidHandle = ^uintptr(0)

var (
	initOnce sync.Once
	initErr  error

	createNamedPipeW    func(name *uint16, openMode, pipeMode, maxInstances, outBuf, inBuf, timeout uint32, sa uintptr) uintptr
	connectNamedPipe    func(h, overlapped uintptr) int32
	disconnectNamedPipe func(h uintptr) int32
	readFile            func(h uintptr, buf *byte, n uint32, read *uint32, overlapped uintptr) int32
	writeFile           func(h uintptr, buf *byte, n uint32, written *uint32, overlapped uintptr) int32
	createFileW         func(name *uint16, access, share uint32, sa uintptr, disp, flags uint32, template uintptr) uintptr
	closeHandle         func(h uintptr) int32
)

func ensureInit() error {
	initOnce.Do(func() {
		k32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = err
			return
		}
		reg := func(p any, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(k32, name)
			if e != nil {
				initErr = e
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&createNamedPipeW, "CreateNamedPipeW")
		reg(&connectNamedPipe, "ConnectNamedPipe")
		reg(&disconnectNamedPipe, "DisconnectNamedPipe")
		reg(&readFile, "ReadFile")
		reg(&writeFile, "WriteFile")
		reg(&createFileW, "CreateFileW")
		reg(&closeHandle, "CloseHandle")
	})
	return initErr
}

func pipeName(id string) string { return `\\.\pipe\native-si-` + key(id) }

func acquire(id string, onMessage func(Message)) (*Lock, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(pipeName(id))
	if err != nil {
		return nil, err
	}
	h := createNamedPipeW(name,
		pipeAccessInbound|fileFlagFirstPipeInstance,
		pipeWaitByte,
		1, 0, pipeInBufferSize, 0, 0)
	if h == invalidHandle {
		// FILE_FLAG_FIRST_PIPE_INSTANCE fails once an instance exists: the pipe
		// name is derived deterministically and always valid, so a failure here
		// means another instance already owns it.
		return nil, ErrAlreadyRunning
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		servePipe(h, onMessage, stop)
	}()

	return &Lock{release: func() error {
		close(stop)
		// CloseHandle does NOT cancel a synchronous ConnectNamedPipe a thread is
		// blocked in, and closing the handle while that call is still pending
		// blocks this thread until it completes - so the server owns the handle
		// and closes it on the way out (see servePipe). Wake it by connecting to
		// the pipe ourselves: its ConnectNamedPipe returns, it sees stop closed
		// and exits. That connect fails with ERROR_PIPE_BUSY whenever the server
		// is between iterations (handling a message, disconnecting), so keep
		// trying until it has actually exited instead of racing it once.
		deadline := time.Now().Add(5 * time.Second)
		for {
			select {
			case <-done:
				return nil
			default:
			}
			if time.Now().After(deadline) {
				return nil
			}
			if c := createFileW(name, genericWrite, 0, 0, openExisting, 0, 0); c != invalidHandle {
				closeHandle(c)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}}, nil
}

func servePipe(h uintptr, onMessage func(Message), stop chan struct{}) {
	// The server thread owns the pipe handle and closes it on the way out.
	// Release runs on another thread, and closing a handle whose synchronous
	// ConnectNamedPipe is still pending blocks until that call completes - so
	// closing it here, after the connect has returned, frees the pipe instance
	// without that hazard.
	defer closeHandle(h)
	buf := make([]byte, pipeInBufferSize)
	for {
		// Blocks until a client connects. Release wakes this call by connecting
		// to the pipe itself, which is how the stop check below is reached and
		// this loop exits (closing h on the way out).
		connectNamedPipe(h, 0)
		select {
		case <-stop:
			return
		default:
		}

		var total []byte
		for {
			var n uint32
			ok := readFile(h, &buf[0], uint32(len(buf)), &n, 0)
			if n > 0 {
				total = append(total, buf[:n]...)
			}
			if ok == 0 {
				break // client closed its end (ERROR_BROKEN_PIPE) or an error
			}
		}
		if len(total) > 0 {
			var m Message
			if json.Unmarshal(total, &m) == nil && onMessage != nil {
				onMessage(m)
			}
		}
		disconnectNamedPipe(h)
	}
}

func send(id string, args []string) error {
	err := ensureInit()
	if err != nil {
		return err
	}
	data, err := json.Marshal(Message{Args: args})
	if err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(pipeName(id))
	if err != nil {
		return err
	}
	h := createFileW(name, genericWrite, 0, 0, openExisting, 0, 0)
	if h == invalidHandle {
		return errors.New("instance: no running instance to receive the message")
	}
	defer closeHandle(h)
	var written uint32
	if writeFile(h, &data[0], uint32(len(data)), &written, 0) == 0 {
		return errors.New("instance: failed to write to the running instance")
	}
	return nil
}
