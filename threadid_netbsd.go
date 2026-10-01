package tuohi

import "syscall"

// osThreadID is the calling OS thread's ID, from _lwp_self(2).
func osThreadID() int64 {
	id, _, _ := syscall.RawSyscall(syscall.SYS__LWP_SELF, 0, 0, 0)
	return int64(id)
}
