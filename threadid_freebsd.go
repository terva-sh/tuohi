package tuohi

import (
	"syscall"
	"unsafe"
)

// osThreadID is the calling OS thread's ID, from thr_self(2).
func osThreadID() int64 {
	var id int64
	syscall.RawSyscall(syscall.SYS_THR_SELF, uintptr(unsafe.Pointer(&id)), 0, 0) // #nosec G103 -- thr_self writes the ID through this pointer
	return id
}
