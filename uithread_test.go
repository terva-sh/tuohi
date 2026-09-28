package tuohi

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeUI is a dispatcher whose UI thread is a queue the test drains by hand.
type fakeUI struct {
	*uiDispatcher
	onUIFlag atomic.Bool
	refuse   atomic.Bool
	extFlag  atomic.Bool

	mu     sync.Mutex
	queued []func()
}

func newFakeUI() *fakeUI {
	f := &fakeUI{}
	f.uiDispatcher = &uiDispatcher{
		onUI: f.onUIFlag.Load,
		post: func(fn func()) bool {
			if f.refuse.Load() {
				return false
			}
			f.mu.Lock()
			f.queued = append(f.queued, fn)
			f.mu.Unlock()
			return true
		},
		external: f.extFlag.Load,
	}
	return f
}

// drain runs everything queued so far, as the UI thread would.
func (f *fakeUI) drain() {
	f.mu.Lock()
	q := f.queued
	f.queued = nil
	f.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func (f *fakeUI) queuedLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queued)
}

// waitQueued waits until n closures are queued, so a call's post is known to
// have happened.
func (f *fakeUI) waitQueued(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.queuedLen() < n {
		if time.Now().After(deadline) {
			t.Fatalf("queued = %d, want %d", f.queuedLen(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUIRunInPlaceOnUIThread(t *testing.T) {
	f := newFakeUI()
	f.onUIFlag.Store(true)
	ran := false
	f.run(func() { ran = true })
	if !ran || f.queuedLen() != 0 {
		t.Fatalf("on the UI thread run should run in place: ran=%v queued=%d", ran, f.queuedLen())
	}
	if err := f.call(func() {}); err != nil {
		t.Fatalf("call on the UI thread: %v", err)
	}
}

func TestUIRunQueuesOffUIThread(t *testing.T) {
	f := newFakeUI()
	var order []int
	f.run(func() { order = append(order, 1) })
	f.run(func() { order = append(order, 2) })
	if len(order) != 0 {
		t.Fatal("run off the UI thread ran in place")
	}
	f.drain()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("queued runs ran as %v, want [1 2]", order)
	}
}

func TestUICallWithoutLoop(t *testing.T) {
	f := newFakeUI()
	ran := false
	if err := f.call(func() { ran = true }); !errors.Is(err, errUILoopStopped) {
		t.Fatalf("call with no loop: err = %v, want errUILoopStopped", err)
	}
	f.drain()
	if ran || f.queuedLen() != 0 {
		t.Fatal("a call refused for want of a loop must not queue or run")
	}
}

func TestUICallRunsAndWaits(t *testing.T) {
	f := newFakeUI()
	f.enterLoop()
	defer f.exitLoop()
	var ran atomic.Bool
	errc := make(chan error, 1)
	go func() { errc <- f.call(func() { ran.Store(true) }) }()
	f.waitQueued(t, 1)
	select {
	case err := <-errc:
		t.Fatalf("call returned before its operation ran: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	f.drain()
	if err := <-errc; err != nil || !ran.Load() {
		t.Fatalf("call: err=%v ran=%v", err, ran.Load())
	}
}

func TestUICallCancelledWhenLoopStops(t *testing.T) {
	f := newFakeUI()
	f.enterLoop()
	var ran atomic.Bool
	errc := make(chan error, 1)
	go func() { errc <- f.call(func() { ran.Store(true) }) }()
	f.waitQueued(t, 1)
	f.exitLoop()
	if err := <-errc; !errors.Is(err, errUILoopStopped) {
		t.Fatalf("call pending when the loop stopped: err = %v", err)
	}
	// A later loop drains the stale closure: the cancelled operation must not
	// run, since its caller was already told it failed.
	f.drain()
	if ran.Load() {
		t.Fatal("a cancelled operation ran")
	}
}

func TestUICallRunningSurvivesLoopStop(t *testing.T) {
	f := newFakeUI()
	f.enterLoop()
	started, release := make(chan struct{}), make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- f.call(func() {
			close(started)
			<-release
		})
	}()
	f.waitQueued(t, 1)
	go f.drain()
	<-started
	f.exitLoop()
	select {
	case err := <-errc:
		t.Fatalf("call returned while its operation was running: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("an operation that ran must report success, got %v", err)
	}
}

func TestUICallRefusedPost(t *testing.T) {
	f := newFakeUI()
	f.enterLoop()
	defer f.exitLoop()
	f.refuse.Store(true)
	if err := f.call(func() {}); !errors.Is(err, errUILoopStopped) {
		t.Fatalf("call whose post was refused: err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) != 0 {
		t.Fatalf("a refused operation stayed pending: %d", len(f.pending))
	}
}

func TestUICallExternalLoop(t *testing.T) {
	// A loop tuohi does not own (macOS tray, embedding host) never reports
	// stopping, so a call waits on it rather than failing.
	f := newFakeUI()
	f.extFlag.Store(true)
	errc := make(chan error, 1)
	go func() { errc <- f.call(func() {}) }()
	f.waitQueued(t, 1)
	f.drain()
	if err := <-errc; err != nil {
		t.Fatalf("call under an external loop: %v", err)
	}
}

func TestUINestedLoopsCancelOnlyAtLast(t *testing.T) {
	f := newFakeUI()
	f.enterLoop()
	f.enterLoop() // a nested Run
	errc := make(chan error, 1)
	go func() { errc <- f.call(func() {}) }()
	f.waitQueued(t, 1)
	f.exitLoop()
	select {
	case err := <-errc:
		t.Fatalf("call cancelled while an outer loop still runs: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	f.drain()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	f.exitLoop()
}

// closeCountingEngine counts Close calls; every other engine method panics
// through the nil embedded interface, so the test proves Close touches
// nothing else.
type closeCountingEngine struct {
	engine
	closes atomic.Int32
}

func (e *closeCountingEngine) Close() { e.closes.Add(1) }

// TestViewCloseConcurrent runs View.Close from many goroutines at once. Run it
// under -race: before View guarded its engine handle, a Close could re-read a
// handle another Close had cleared and call Close on a nil engine.
func TestViewCloseConcurrent(t *testing.T) {
	for round := 0; round < 50; round++ {
		e := &closeCountingEngine{}
		v := &View{w: e}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				v.Close()
			}()
		}
		close(start)
		wg.Wait()
		if n := e.closes.Load(); n != 1 {
			t.Fatalf("round %d: engine closed %d times, want 1", round, n)
		}
		if v.live() != nil {
			t.Fatalf("round %d: View still holds its engine after Close", round)
		}
	}
}

func TestUICallCancelledWhenExternalLoopStops(t *testing.T) {
	// An external loop reports nothing when it stops, so a call admitted
	// under it must notice the stop itself rather than wait forever.
	f := newFakeUI()
	f.extFlag.Store(true)
	var ran atomic.Bool
	errc := make(chan error, 1)
	go func() { errc <- f.call(func() { ran.Store(true) }) }()
	f.waitQueued(t, 1)
	f.extFlag.Store(false)
	select {
	case err := <-errc:
		if !errors.Is(err, errUILoopStopped) {
			t.Fatalf("call after the external loop stopped: err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call still waiting after the external loop stopped")
	}
	f.drain()
	if ran.Load() {
		t.Fatal("a cancelled operation ran")
	}
}

// TestViewCloseKeepsReshownRegistration checks that a Close which lost a
// race with App.Show re-showing the View does not unregister the new window.
func TestViewCloseKeepsReshownRegistration(t *testing.T) {
	s := &appScope{}
	app := &App{scope: s}
	v := &View{}
	oldEngine, newEngine := &closeCountingEngine{}, &closeCountingEngine{}
	app.registerView(v, oldEngine)
	// The View was closed and shown again before the first Close unregistered.
	app.registerView(v, newEngine)
	app.unregisterView(v, oldEngine)
	s.viewsMu.Lock()
	got := s.views[v]
	s.viewsMu.Unlock()
	if got != newEngine {
		t.Fatal("closing the old engine unregistered the re-shown View")
	}
	app.unregisterView(v, newEngine)
	if _, ok := s.views[v]; ok {
		t.Fatal("closing the current engine left the View registered")
	}
}

// navCountingEngine counts Navigate and Close; other methods panic through
// the nil embedded interface.
type navCountingEngine struct {
	closeCountingEngine
	navigates atomic.Int32
}

func (e *navCountingEngine) Navigate(string) { e.navigates.Add(1) }

// TestViewQueuedCallDroppedAfterClose checks that a View call queued from
// another goroutine does not reach an engine that Close destroyed before the
// queue drained.
func TestViewQueuedCallDroppedAfterClose(t *testing.T) {
	f := newFakeUI()
	prev := ui
	ui = f.uiDispatcher
	defer func() { ui = prev }()

	e := &navCountingEngine{}
	v := &View{w: e}
	v.Navigate("https://example.com/")
	f.drain()
	if n := e.navigates.Load(); n != 1 {
		t.Fatalf("a queued Navigate on a live View ran %d times, want 1", n)
	}

	v.Navigate("https://example.com/")
	v.Close()
	f.drain()
	if n := e.navigates.Load(); n != 1 {
		t.Fatal("a Navigate queued before Close reached the closed engine")
	}
}
