package tuohi

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// This file carries the threading rule from docs/architecture.md ("One
// threading rule"): every exported View method is safe to call from any
// goroutine on every engine. On the UI thread a call runs in place. Elsewhere
// it is marshalled to the UI thread, without waiting unless the caller needs
// a result. A caller that waits gives up only when no loop is left to run its
// operation, never on a timer, so a call never reports a failure and then
// runs anyway.

// errUILoopStopped is what a call that waits for the UI thread returns when
// no loop is running to serve it.
var errUILoopStopped = errors.New("tuohi: the UI loop is not running")

// Operation states. An operation moves from pending to running on the UI
// thread, or from pending to cancelled when the loop stops; the two moves are
// compare-and-swaps, so exactly one of them wins.
const (
	opPending int32 = iota
	opRunning
	opCancelled
	opDone
)

// uiOp is one marshalled call whose caller waits for it.
type uiOp struct {
	f     func()
	state atomic.Int32
	done  chan struct{} // closed once the state is final (done or cancelled)
}

// uiDispatcher marshals work onto the UI thread through three per-engine
// hooks, and tracks whether a loop is running to drain it.
type uiDispatcher struct {
	// onUI reports whether the caller runs on the UI thread.
	onUI func() bool
	// post queues f to run on the UI thread and reports whether it could.
	post func(f func()) bool
	// external reports whether a loop tuohi does not own is draining the UI
	// queue (macOS: the tray package's, or an embedding host's [NSApp run]).
	// Such a loop never reports stopping, so a caller waits on it as long as
	// it runs.
	external func() bool

	mu      sync.Mutex
	loops   int            // loops tuohi runs now (App.Wait, webview.Run)
	pending map[*uiOp]bool // waiting operations not yet final
}

// ui is the process-wide dispatcher, wired to this platform's engine.
var ui = &uiDispatcher{onUI: onUIThread, post: postUI, external: uiLoopExternal}

// run runs f on the UI thread: in place when the caller is already there,
// otherwise queued without waiting. A queued f runs when a loop next drains
// the queue; it is dropped if the queue cannot take it.
func (d *uiDispatcher) run(f func()) {
	if d.onUI() {
		f()
		return
	}
	d.post(f)
}

// call runs f on the UI thread and waits for it to finish. It runs f in place
// when the caller is already on the UI thread. Otherwise it returns
// errUILoopStopped, and f never runs, when no loop is running, when the queue
// refuses f, or when the loop stops before f starts. Once f has started, call
// waits for it to finish, whatever the loop does.
func (d *uiDispatcher) call(f func()) error {
	if d.onUI() {
		f()
		return nil
	}
	op := &uiOp{f: f, done: make(chan struct{})}
	d.mu.Lock()
	if d.loops == 0 && !d.external() {
		d.mu.Unlock()
		return errUILoopStopped
	}
	if d.pending == nil {
		d.pending = map[*uiOp]bool{}
	}
	d.pending[op] = true
	d.mu.Unlock()
	if !d.post(func() { d.runOp(op) }) {
		d.cancel(op)
	}
	d.wait(op)
	if op.state.Load() == opCancelled {
		return errUILoopStopped
	}
	return nil
}

// externalLoopPoll is how often a waiting call checks that some loop is still
// running. A loop tuohi owns cancels its callers when it exits (exitLoop), but
// an external loop reports nothing when it stops, so its end is only seen by
// looking.
const externalLoopPoll = 50 * time.Millisecond

// wait blocks until op is final. While it waits it checks that a loop is
// still running, and cancels op when none is: an external loop that stopped
// before op started will never run it. This is not a timeout: a busy loop
// that still runs is waited for however long it takes.
func (d *uiDispatcher) wait(op *uiOp) {
	tick := time.NewTicker(externalLoopPoll)
	defer tick.Stop()
	for {
		select {
		case <-op.done:
			return
		case <-tick.C:
			d.mu.Lock()
			stopped := d.loops == 0 && !d.external()
			d.mu.Unlock()
			if stopped {
				d.cancel(op) // no-op once op has started; keep waiting then
			}
		}
	}
}

// runOp is the UI-thread half of call: it claims op, runs it, and releases
// its caller. A cancelled op is skipped.
func (d *uiDispatcher) runOp(op *uiOp) {
	if !op.state.CompareAndSwap(opPending, opRunning) {
		return
	}
	defer func() {
		op.state.Store(opDone)
		d.forget(op)
		close(op.done)
	}()
	op.f()
}

// cancel moves op from pending to cancelled and releases its caller. It does
// nothing to an op that has already started, whose caller keeps waiting.
func (d *uiDispatcher) cancel(op *uiOp) {
	if op.state.CompareAndSwap(opPending, opCancelled) {
		d.forget(op)
		close(op.done)
	}
}

func (d *uiDispatcher) forget(op *uiOp) {
	d.mu.Lock()
	delete(d.pending, op)
	d.mu.Unlock()
}

// enterLoop records that a loop tuohi owns has started draining the UI queue.
// The loop's owner calls it on the UI thread before its loop and exitLoop
// after it.
func (d *uiDispatcher) enterLoop() {
	d.mu.Lock()
	d.loops++
	d.mu.Unlock()
}

// exitLoop records that a loop has returned. When no loop is left, owned or
// external, every operation still pending is cancelled, since nothing would
// run it: queued work is only drained again when a loop next starts, and the
// caller of an operation must not wait for a loop that may never come.
func (d *uiDispatcher) exitLoop() {
	d.mu.Lock()
	d.loops--
	var stranded []*uiOp
	if d.loops == 0 && !d.external() {
		for op := range d.pending {
			stranded = append(stranded, op)
		}
	}
	d.mu.Unlock()
	for _, op := range stranded {
		d.cancel(op)
	}
}
