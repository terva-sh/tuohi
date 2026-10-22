package tuohi

import "sync"

// WindowState describes the native top-level window, independent of page
// focus. Visible means mapped/shown and not miniaturized where the native
// toolkit exposes that state. It does not imply that pixels are unobscured,
// on the current workspace, or visible to the user. Closed is terminal for
// this window lifetime. Close resets the View before creating a new lifetime.
type WindowState struct {
	Visible bool
	Active  bool
	Closed  bool
}

// CloseRequest is one native or View.RequestClose request. Its methods are
// safe from any goroutine. Only the first Allow or Cancel wins. If the callback
// returns without a decision or Defer, closing is allowed. Defer keeps the
// existing window alive until a later decision; repeated requests coalesce.
// View.Close and App.Quit invalidate the request without waiting for a decision.
type CloseRequest struct {
	owner    *windowLifecycle
	done     chan struct{}
	deferred bool
	decision uint8
	callback bool
}

// Done closes when decided or invalidated. It carries no decision; a stale
// request's Allow, Cancel and Defer return false and cannot affect a new view.
func (r *CloseRequest) Done() <-chan struct{} { return r.done }

// Defer keeps the request pending after CloseRequested returns. The consumer
// must eventually Allow or Cancel, unless teardown/quit invalidates it first.
func (r *CloseRequest) Defer() bool {
	l := r.owner
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending != r || r.decision != 0 || !r.callback {
		return false
	}
	r.deferred = true
	return true
}

// Allow accepts the close, marshaling a deferred decision to the UI thread.
func (r *CloseRequest) Allow() bool { return r.resolve(1) }

// Cancel keeps the existing window and renderer alive. A hide-to-tray handler
// can Cancel then call View.Hide without rebuilding its page.
func (r *CloseRequest) Cancel() bool { return r.resolve(2) }

func (r *CloseRequest) resolve(decision uint8) bool {
	l := r.owner
	l.mu.Lock()
	if l.pending != r || r.decision != 0 {
		l.mu.Unlock()
		return false
	}
	r.decision = decision
	close(r.done)
	if r.callback {
		l.mu.Unlock()
		return true
	}
	if decision == 2 {
		l.pending = nil
		l.mu.Unlock()
		return true
	}
	dispatch := l.dispatch
	l.mu.Unlock()
	dispatch(func() {
		l.mu.Lock()
		valid := l.pending == r && !l.state.Closed && !l.quitting
		l.mu.Unlock()
		if valid {
			l.closeWindow()
		}
	})
	return true
}

// windowLifecycle has its own lock; native callbacks and consumer callbacks
// never hold the bridge/View mutex. State delivery and request admission run
// on the UI thread; decisions and shutdown invalidation may run anywhere.
type windowLifecycle struct {
	mu          sync.Mutex
	started     bool
	quitting    bool
	state       WindowState
	pending     *CloseRequest
	onClose     func(*CloseRequest)
	onChanged   func(WindowState)
	dispatch    func(func())
	closeWindow func()
}

func (l *windowLifecycle) start(state WindowState, onClose func(*CloseRequest), onChanged func(WindowState), dispatch func(func()), closeWindow func()) {
	l.mu.Lock()
	l.started, l.state = true, state
	l.onClose, l.onChanged = onClose, onChanged
	l.dispatch, l.closeWindow = dispatch, closeWindow
	l.mu.Unlock()
	if onChanged != nil {
		onChanged(state)
	}
}

func (l *windowLifecycle) snapshot() WindowState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

func (l *windowLifecycle) update(state WindowState) {
	l.mu.Lock()
	if !l.started || l.state.Closed || l.state == state {
		l.mu.Unlock()
		return
	}
	l.state = state
	if state.Closed {
		l.invalidateLocked()
	}
	changed := l.onChanged
	l.mu.Unlock()
	if changed != nil {
		changed(state)
	}
}

func (l *windowLifecycle) invalidateLocked() {
	if r := l.pending; r != nil {
		if r.decision == 0 {
			close(r.done)
		}
		l.pending = nil
	}
}

func (l *windowLifecycle) quit() {
	l.mu.Lock()
	l.quitting = true
	l.invalidateLocked()
	l.mu.Unlock()
}

// request reports whether the native caller should proceed with its normal
// close. Deferred Allow calls closeWindow directly, bypassing interception.
func (l *windowLifecycle) request() bool {
	l.mu.Lock()
	if l.state.Closed {
		l.mu.Unlock()
		return false
	}
	if !l.started || l.quitting || l.onClose == nil {
		l.mu.Unlock()
		return true
	}
	if l.pending != nil {
		l.mu.Unlock()
		return false
	}
	r := &CloseRequest{owner: l, done: make(chan struct{}), callback: true}
	l.pending = r
	callback := l.onClose
	l.mu.Unlock()
	callback(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	r.callback = false
	if l.pending != r || l.state.Closed || l.quitting {
		return false
	}
	if r.decision == 0 && r.deferred {
		return false
	}
	l.pending = nil
	if r.decision == 0 {
		close(r.done)
	}
	return r.decision != 2
}

// WindowState returns the last delivered native snapshot, safe from any
// goroutine. An unshown or programmatically closed View returns Closed=true.
func (v *View) WindowState() WindowState {
	w := v.live()
	if w == nil {
		return WindowState{Closed: true}
	}
	return w.core().lifecycle.snapshot()
}

// RequestClose asks to close through CloseRequested, for example from custom
// window chrome. It is safe from any goroutine and queues onto the UI thread.
// Close instead tears down unconditionally.
func (v *View) RequestClose() {
	v.onUI(func(w engine) {
		if w.core().lifecycle.request() {
			v.closeEngine(w)
		}
	})
}
