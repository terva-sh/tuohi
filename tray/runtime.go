package tray

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"sync"
	"sync/atomic"
)

// ErrNotRunning means the tray targeted by an update has been removed, or no
// tray exists. A queued update never applies to a later tray lifetime.
var ErrNotRunning = errors.New("tray: no active tray for this operation")

// ErrUnavailable means a native tray registration or host is unavailable.
var ErrUnavailable = errors.New("tray: host or native registration unavailable")

// Changes is a patch to the current tray. Nil fields preserve their values.
// Non-nil empty icon slices clear that variant; Title/Tooltip pointers may
// clear text. Items replaces the entire menu including callbacks and checked
// state; a pointer to an empty slice removes it. Omitting Items preserves
// native checkbox toggles. Startup identity, Dock policy and tray-level click
// handlers remain as configured by Set/Run.
type Changes struct {
	Icon []byte
	DarkModeIcon []byte
	TemplateIcon []byte
	Title *string
	Tooltip *string
	Items *[]Item
}

// Update changes an existing tray without removing/recreating its native
// status item. It is safe from any goroutine and copies slices before queuing.
// The host's native UI loop must be running for calls from other goroutines;
// ctx bounds the wait and cancels work still queued. Work already accepted by
// the native thread may complete after cancellation. Calls on that thread run
// directly. A tray removal/recreation never receives an earlier queued patch.
func Update(ctx context.Context, changes Changes) error {
	if err := ctx.Err(); err != nil { return err }
	changes = cloneChanges(changes)
	if err := validateChanges(changes); err != nil { return err }
	return update(ctx, changes)
}

func cloneBytes(b []byte) []byte {
	if b == nil { return nil }
	return append(make([]byte, 0, len(b)), b...)
}

func cloneItems(items []Item) []Item {
	if items == nil { return nil }
	out := make([]Item, len(items))
	for i, item := range items {
		out[i] = item
		out[i].Icon = cloneBytes(item.Icon)
		out[i].Submenu = cloneItems(item.Submenu)
	}
	return out
}

func cloneConfig(cfg Config) Config {
	cfg.Icon, cfg.DarkModeIcon, cfg.TemplateIcon = cloneBytes(cfg.Icon), cloneBytes(cfg.DarkModeIcon), cloneBytes(cfg.TemplateIcon)
	cfg.Items = cloneItems(cfg.Items)
	return cfg
}

func cloneChanges(c Changes) Changes {
	c.Icon, c.DarkModeIcon, c.TemplateIcon = cloneBytes(c.Icon), cloneBytes(c.DarkModeIcon), cloneBytes(c.TemplateIcon)
	if c.Title != nil { v := *c.Title; c.Title = &v }
	if c.Tooltip != nil { v := *c.Tooltip; c.Tooltip = &v }
	if c.Items != nil { v := cloneItems(*c.Items); c.Items = &v }
	return c
}

func validateChanges(c Changes) error {
	check := func(data []byte) error {
		if len(data) == 0 { return nil }
		if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil { return errors.New("tray: update contains an invalid PNG icon") }
		return nil
	}
	for _, data := range [][]byte{c.Icon, c.DarkModeIcon, c.TemplateIcon} {
		if err := check(data); err != nil { return err }
	}
	var walk func([]Item) error
	walk = func(items []Item) error {
		for _, item := range items {
			if err := check(item.Icon); err != nil { return err }
			if err := walk(item.Submenu); err != nil { return err }
		}
		return nil
	}
	if c.Items != nil { return walk(*c.Items) }
	return nil
}

func applyChanges(cfg Config, c Changes) Config {
	if c.Icon != nil { cfg.Icon = c.Icon }
	if c.DarkModeIcon != nil { cfg.DarkModeIcon = c.DarkModeIcon }
	if c.TemplateIcon != nil { cfg.TemplateIcon = c.TemplateIcon }
	if c.Title != nil { cfg.Title = *c.Title }
	if c.Tooltip != nil { cfg.Tooltip = *c.Tooltip }
	if c.Items != nil { cfg.Items = *c.Items }
	return cfg
}

// Availability reports native registration and host presence, never whether
// the user can actually see the icon. For conservative close-to-tray policy,
// require Running, Registered and HostPresent, and keep the decision in the
// application. GNOME may need an extension; WSLg/nested sessions may have no
// host. Even a registered host can suppress or hide an icon.
type Availability struct {
	// Generation distinguishes tray lifetimes. An already admitted callback
	// from an old tray may finish after removal; consult CurrentAvailability
	// before changing window/background policy based on a callback.
	Generation uint64
	Running bool
	Registered bool
	HostPresent bool
	Err error
}

var availabilityGlobal struct {
	sync.Mutex
	active *availabilitySource
}
var availabilityGeneration atomic.Uint64

// CurrentAvailability returns the latest state, or a zero value without an
// active tray. On Linux registration and host presence are observed separately.
// macOS reports status-item creation; Windows reports shell registration.
// Neither native platform guarantees visual presentation or user reachability.
func CurrentAvailability() Availability {
	availabilityGlobal.Lock(); a := availabilityGlobal.active; availabilityGlobal.Unlock()
	if a == nil { return Availability{} }
	a.mu.Lock(); defer a.mu.Unlock()
	return a.state
}

type availabilitySource struct {
	mu sync.Mutex
	state Availability
	onChange func(Availability)
	wake chan struct{}
	done chan struct{}
	closed bool
}

func newAvailability(fn func(Availability)) *availabilitySource {
	a := &availabilitySource{onChange: fn, wake: make(chan struct{}, 1), done: make(chan struct{})}
	a.state.Generation = availabilityGeneration.Add(1)
	availabilityGlobal.Lock(); availabilityGlobal.active = a; availabilityGlobal.Unlock()
	if fn != nil { go a.callbacks() }
	return a
}

func (a *availabilitySource) publish(v Availability) {
	a.mu.Lock()
	if a.closed { a.mu.Unlock(); return }
	v.Generation = a.state.Generation
	old := a.state
	a.state = v
	changed := old.Running != v.Running || old.Registered != v.Registered || old.HostPresent != v.HostPresent || !sameAvailabilityError(old.Err, v.Err)
	a.mu.Unlock()
	if changed && a.onChange != nil { select { case a.wake <- struct{}{}: default: } }
}

func sameAvailabilityError(a, b error) bool {
	if a == nil || b == nil { return a == nil && b == nil }
	return a.Error() == b.Error()
}

func (a *availabilitySource) close() {
	if a == nil { return }
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		a.state = Availability{Generation: a.state.Generation}
		close(a.done)
	}
	a.mu.Unlock()
	availabilityGlobal.Lock()
	if availabilityGlobal.active == a { availabilityGlobal.active = nil }
	availabilityGlobal.Unlock()
}

func (a *availabilitySource) callbacks() {
	for {
		select { case <-a.done: return; case <-a.wake: }
		a.mu.Lock()
		if a.closed { a.mu.Unlock(); return }
		state := a.state
		a.mu.Unlock()
		a.onChange(state)
	}
}
