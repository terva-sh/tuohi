package tuohi

import "syscall"

// osThreadID is the calling OS thread's ID.
func osThreadID() int64 { return int64(syscall.Gettid()) }
