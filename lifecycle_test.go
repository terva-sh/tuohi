package tuohi

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestCloseDecisions(t *testing.T) {
	for _, decision := range []string{"default", "allow", "cancel", "defer"} {
		t.Run(decision, func(t *testing.T) {
			var l windowLifecycle
			var r *CloseRequest
			calls, closed := 0, 0
			var queued []func()
			l.start(WindowState{Visible: true}, func(request *CloseRequest) {
				calls++
				r = request
				switch decision {
				case "allow":
					r.Allow()
				case "cancel":
					r.Cancel()
				case "defer":
					if !r.Defer() {
						t.Error("Defer refused")
					}
				}
			}, nil, func(f func()) { queued = append(queued, f) }, func() { closed++ })
			allowed := l.request()
			if allowed != (decision == "default" || decision == "allow") {
				t.Fatalf("allow=%v", allowed)
			}
			if decision == "defer" {
				if l.request() || calls != 1 {
					t.Fatal("repeated native close was not coalesced")
				}
				select {
				case <-r.Done():
					t.Fatal("pending request ended")
				default:
				}
				if !r.Allow() || r.Cancel() || r.Defer() {
					t.Fatal("decision was not exclusive")
				}
				if closed != 0 || len(queued) != 1 {
					t.Fatal("deferred allow did not marshal")
				}
				queued[0]()
				if closed != 1 {
					t.Fatal("deferred allow did not close")
				}
			} else if r.Allow() || r.Cancel() {
				t.Fatal("completed request accepted another decision")
			}
			select {
			case <-r.Done():
			default:
				t.Fatal("completed request has no Done")
			}
		})
	}
}

func TestCloseInvalidationAndQueuedLifetime(t *testing.T) {
	for _, shutdown := range []string{"close", "quit"} {
		t.Run(shutdown, func(t *testing.T) {
			var l windowLifecycle
			var request *CloseRequest
			var queued func()
			closed := false
			l.start(WindowState{}, func(r *CloseRequest) { request = r; r.Defer() }, nil, func(f func()) { queued = f }, func() { closed = true })
			l.request()
			request.Allow()
			if shutdown == "quit" {
				l.quit()
			} else {
				l.update(WindowState{Closed: true})
			}
			queued()
			if closed || request.Allow() || request.Cancel() {
				t.Fatal("stale request affected lifetime")
			}
			if shutdown == "quit" && !l.request() {
				t.Fatal("quit trapped a later native close")
			}
		})
	}
	var l windowLifecycle
	var r *CloseRequest
	l.start(WindowState{}, func(request *CloseRequest) { r = request; r.Defer(); l.quit() }, nil, nil, nil)
	if l.request() {
		t.Fatal("request survived quit inside callback")
	}
	select {
	case <-r.Done():
	default:
		t.Fatal("quit left deferred request waiting")
	}
	if r.Allow() {
		t.Fatal("invalidated request accepted decision")
	}
}

func TestConcurrentCloseResolution(t *testing.T) {
	var l windowLifecycle
	var r *CloseRequest
	l.start(WindowState{}, func(request *CloseRequest) { r = request; r.Defer() }, nil, func(func()) {}, func() {})
	l.request()
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			ok := false
			if i%2 == 0 {
				ok = r.Allow()
			} else {
				ok = r.Cancel()
			}
			if ok {
				winners.Add(1)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("decision winners=%d", winners.Load())
	}
}

func TestWindowStateDeliveryAndReentrancy(t *testing.T) {
	var l windowLifecycle
	var got []WindowState
	l.start(WindowState{Visible: true}, nil, func(s WindowState) {
		if l.snapshot() != s {
			t.Error("callback preceded snapshot")
		}
		got = append(got, s)
		if s.Active {
			l.update(WindowState{})
		}
	}, nil, nil)
	l.update(WindowState{Visible: true}) // duplicate
	l.update(WindowState{Visible: true, Active: true})
	l.update(WindowState{Closed: true})
	l.update(WindowState{Visible: true}) // late native event
	if len(got) != 4 || !got[3].Closed || l.request() {
		t.Fatalf("states=%+v", got)
	}
}

func TestScopeQuitInvalidatesDeferredClose(t *testing.T) {
	e := &titleEngine{}
	var r *CloseRequest
	e.c.lifecycle.start(WindowState{}, func(request *CloseRequest) { r = request; r.Defer() }, nil, nil, nil)
	e.c.lifecycle.request()
	s := &appScope{views: map[*View]engine{new(View): e}}
	s.invalidateCloseRequests()
	select {
	case <-r.Done():
	default:
		t.Fatal("App.Quit left a pending request")
	}
	if r.Allow() || !e.c.lifecycle.request() {
		t.Fatal("quit did not invalidate and bypass close policy")
	}
}

func TestRequestCloseCannotTargetReplacementView(t *testing.T) {
	f := newFakeUI()
	prev := ui
	ui = f.uiDispatcher
	defer func() { ui = prev }()
	old, next := &closeCountingEngine{}, &closeCountingEngine{}
	v := &View{}
	v.RequestClose() // unshown close button/worker is harmless
	v.w = old
	v.RequestClose() // queued for old lifetime
	v.Close()
	v.RequestClose() // teardown raced a late renderer/worker request
	v.w = next
	f.drain()
	if old.closes.Load() != 1 || next.closes.Load() != 0 || v.live() != next {
		t.Fatal("queued close affected replacement lifetime")
	}
}
