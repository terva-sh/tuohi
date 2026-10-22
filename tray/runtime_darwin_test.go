package tray

import (
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

// Exercise the same pool scope used to enqueue worker updates. Yield between
// native allocations to give the Go scheduler opportunities to migrate us.
func TestWorkerAutoreleasePoolThreadAffinity(t *testing.T) {
	if err := ensureInit(); err != nil {
		t.Fatal(err)
	}
	var threadSelf func() uintptr
	purego.RegisterLibFunc(&threadSelf, purego.RTLD_DEFAULT, "pthread_self")
	done := make(chan struct{})
	go func() {
		defer close(done)
		withTrayAutoreleasePool(func() {
			thread := threadSelf()
			for i := range 1000 {
				class("NSNumber").Send(sel("numberWithUnsignedLongLong:"), uint64(i))
				runtime.Gosched()
				if got := threadSelf(); got != thread {
					t.Errorf("autorelease scope migrated from thread %d to %d", thread, got)
					return
				}
			}
		})
	}()
	<-done
}
