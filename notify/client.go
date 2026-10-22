package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ErrClosed means a managed client or notification has ended its lifetime.
var ErrClosed = errors.New("notify: managed notification is closed")

// ErrPermissionDenied means the OS has not authorized notification delivery.
// Posting never opens a permission dialog; call RequestPermission explicitly.
var ErrPermissionDenied = errors.New("notify: notification permission is not granted")

// ErrDuplicateID means a client already has a live notification with that
// application ID. Replace that handle or dismiss it before reusing the ID.
var ErrDuplicateID = errors.New("notify: application notification ID is already in use")

// Capabilities describes the managed backend, not a promise that the user
// will see a notification. Desktop settings and do-not-disturb may hide it.
type Capabilities struct {
	Activation  bool
	Replacement bool
	Dismissal   bool
	// Permission reports a native permission status/request API. Linux's
	// notification protocol needs no permission and has no prompt API.
	Permission bool
}

// PermissionStatus is notification authorization reported by the backend.
type PermissionStatus uint8

const (
	PermissionUnknown PermissionStatus = iota
	PermissionNotDetermined
	PermissionDenied
	PermissionAuthorized
	PermissionProvisional
	PermissionNotRequired
)

// Config configures one managed notification client. It is copied at creation.
type Config struct {
	// Name is the display source on Linux. macOS derives it from the bundle.
	Name string
	// OnActivate receives only the opaque application Notification.ID. Keep
	// the account/room lookup in Go; never put credentials or message content
	// in an ID. The ID is not sent to the OS. Calls run serially on a worker,
	// never a native UI thread, and may call View methods or Client.Close.
	// A callback admitted before Close/Dismiss may finish afterwards.
	OnActivate func(id string)
}

// Notification is the content of a managed notification. Composition and
// privacy policy belong to the application: Title and Message go to the OS.
type Notification struct {
	// ID is an opaque application correlation key, kept only in Go. Use a
	// random key and keep routing details in an application-owned map. It is
	// required, limited to 256 bytes, and unique among this client's live
	// notifications. A replacement keeps the same ID (or may leave it empty).
	ID      string
	Title   string
	Message string
	Options Options
}

// Client owns notification correlation, activation delivery and resources.
// Construct it with NewClient. Methods are safe from any goroutine, serialize
// native operations, and may block; call them off the UI thread. Existing
// Show/ShowOpts do not participate in this client's lifetime.
type Client struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	cfg         Config
	backend     managedBackend
	caps        Capabilities
	byID        map[string]*Handle
	byNative    map[string]*Handle
	queue       []*Handle
	wake        chan struct{}
	done        chan struct{}
	cleanupDone chan struct{}
	closed      bool
	closeErr    error
}

// Handle identifies a notification posted by a Client. Its application ID is
// stable; OS identifiers are private and may change on replacement.
type Handle struct {
	client   *Client
	id       string
	native   string
	done     chan struct{}
	closed   bool
	suppress bool
	queued   bool
	err      error
}

type managedEvent struct {
	native    string
	activated bool
	closed    bool
	err       error // an empty native ID with an error ends every outstanding handle
}

type managedBackend interface {
	capabilities() Capabilities
	permission(context.Context, bool) (PermissionStatus, error)
	post(context.Context, string, string, Notification) (string, error)
	dismiss(context.Context, string) error
	close() error
}

// NewClient opens a managed notification backend for ctx's lifetime. Cancel
// ctx or call Close to stop callbacks, dismiss tracked notifications best-effort
// and release resources. It does not ask permission or silently fall back to
// read-only command-line helpers. macOS requires a bundled application and a
// free UNUserNotificationCenter delegate; Windows balloons and unsupported
// platforms return ErrUnsupported. See notify/README.md for capabilities.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	return newClient(ctx, cfg, openManagedBackend)
}

func newClient(ctx context.Context, cfg Config, open func(context.Context, Config, func(managedEvent)) (managedBackend, error)) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(ctx)
	c := &Client{ctx: lifetime, cancel: cancel, cfg: cfg, byID: make(map[string]*Handle), byNative: make(map[string]*Handle),
		wake: make(chan struct{}, 1), done: make(chan struct{}), cleanupDone: make(chan struct{})}
	b, err := open(lifetime, cfg, c.event)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := lifetime.Err(); err != nil {
		_ = b.close()
		cancel()
		return nil, err
	}
	c.backend, c.caps = b, b.capabilities()
	if cfg.OnActivate != nil && !c.caps.Activation {
		_ = b.close()
		cancel()
		return nil, ErrUnsupported
	}
	go c.callbacks()
	go func() {
		select {
		case <-lifetime.Done():
			_ = c.Close()
		case <-c.done:
		}
	}()
	return c, nil
}

// Capabilities returns the backend's capabilities at creation. A notification
// service restart may make the client unavailable; create a new client then.
func (c *Client) Capabilities() Capabilities { return c.caps }

// Permission queries current authorization without prompting. Linux returns
// PermissionNotRequired; it cannot report desktop suppression settings.
func (c *Client) Permission(ctx context.Context) (PermissionStatus, error) {
	return c.permission(ctx, false)
}

// RequestPermission explicitly asks the OS for notification authorization.
// Call it in response to a user choice. Denial is a PermissionDenied result;
// macOS cannot show the prompt again after denial. Linux has no prompt and
// returns PermissionNotRequired. Cancelling ctx stops waiting, not an already
// visible OS permission dialog.
func (c *Client) RequestPermission(ctx context.Context) (PermissionStatus, error) {
	return c.permission(ctx, true)
}

func (c *Client) permission(ctx context.Context, request bool) (PermissionStatus, error) {
	ctx, cancel := c.operation(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return PermissionUnknown, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return PermissionUnknown, ErrClosed
	}
	return c.backend.permission(ctx, request)
}

func (n Notification) validate() error {
	if n.ID == "" || len(n.ID) > 256 {
		return errors.New("notify: an opaque application ID of 1 to 256 bytes is required")
	}
	return n.Options.validate()
}

// Post sends a notification and returns its correlated handle. If activation
// was requested in Config but unavailable, NewClient already failed. A nil
// callback permits read-only managed notifications. A successful Post means
// the service accepted the request, not that the desktop displayed it.
// Context cancellation cannot undo an OS operation already accepted.
func (c *Client) Post(ctx context.Context, n Notification) (*Handle, error) {
	ctx, cancel := c.operation(ctx)
	defer cancel()
	if err := n.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return nil, ErrClosed
	}
	if c.byID[n.ID] != nil {
		return nil, ErrDuplicateID
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, errors.New("notify: cannot generate native identifier")
	}
	native, err := c.backend.post(ctx, hex.EncodeToString(key[:]), "", n)
	if err != nil {
		return nil, err
	}
	h := &Handle{client: c, id: n.ID, native: native, done: make(chan struct{})}
	c.byID[h.id], c.byNative[h.native] = h, h
	return h, nil
}

// ID returns the opaque application ID supplied to Post, never an OS ID.
func (h *Handle) ID() string { return h.id }

// Done closes when the OS closes the notification, Dismiss succeeds, the
// service disappears, or its client closes. An activation alone need not
// dismiss a resident Linux notification. macOS does not report every passive
// expiration/dismissal, so Done there also depends on explicit cleanup.
func (h *Handle) Done() <-chan struct{} { return h.done }

// Err is nil for normal dismissal/expiry, ErrUnavailable on service loss,
// and ErrClosed when the owning client closed. Read it after Done closes.
func (h *Handle) Err() error {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()
	return h.err
}

// Replace changes content without changing the handle's application ID. Empty
// n.ID keeps that ID; a different ID is rejected. Failure leaves the existing
// handle tracked. Replacement is only attempted when the backend supports it.
func (h *Handle) Replace(ctx context.Context, n Notification) error {
	ctx, cancel := h.client.operation(ctx)
	defer cancel()
	if n.ID == "" {
		n.ID = h.id
	}
	if n.ID != h.id {
		return errors.New("notify: replacement cannot change the application ID")
	}
	if err := n.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c := h.client
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil || h.closed {
		return ErrClosed
	}
	if !c.caps.Replacement {
		return ErrUnsupported
	}
	native, err := c.backend.post(ctx, h.native, h.native, n)
	if err != nil {
		return err
	}
	delete(c.byNative, h.native)
	h.native = native
	c.byNative[native] = h
	return nil
}

// Dismiss removes this notification and suppresses queued activation callbacks.
// It is idempotent for an already closed notification in a live client. An
// activation callback already admitted by the worker may finish afterwards.
func (h *Handle) Dismiss(ctx context.Context) error {
	ctx, cancel := h.client.operation(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	c := h.client
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return ErrClosed
	}
	if h.closed {
		h.suppress = true
		return nil
	}
	if !c.caps.Dismissal {
		return ErrUnsupported
	}
	if err := c.backend.dismiss(ctx, h.native); err != nil {
		return err
	}
	h.suppress = true
	c.finish(h, nil)
	return nil
}

// Close stops activation delivery, dismisses outstanding notifications
// best-effort (with a shared five-second cleanup deadline), and closes native
// resources. It is idempotent and may be called from OnActivate. A callback
// already admitted may finish after Close; queued callbacks are discarded.
func (c *Client) Close() error {
	c.cancel() // interrupt native waits before acquiring their serialization lock
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.cleanupDone
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.closed = true
	close(c.done)
	var native []string
	for _, h := range c.byID {
		native = append(native, h.native)
		h.suppress = true
		c.finish(h, ErrClosed)
	}
	c.queue = nil
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var errs []error
	if c.caps.Dismissal {
		for _, id := range native {
			if err := c.backend.dismiss(ctx, id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := c.backend.close(); err != nil {
		errs = append(errs, err)
	}
	c.mu.Lock()
	c.closeErr = errors.Join(errs...)
	close(c.cleanupDone)
	err := c.closeErr
	c.mu.Unlock()
	return err
}

func (c *Client) finish(h *Handle, err error) {
	if h.closed {
		return
	}
	h.closed, h.err = true, err
	delete(c.byID, h.id)
	delete(c.byNative, h.native)
	close(h.done)
}

func (c *Client) operation(ctx context.Context) (context.Context, context.CancelFunc) {
	op, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	return op, func() { stop(); cancel() }
}

func (c *Client) event(e managedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return
	}
	if e.native == "" && e.err != nil {
		for _, h := range c.byID {
			h.suppress = true
			c.finish(h, e.err)
		}
		return
	}
	h := c.byNative[e.native]
	if h == nil {
		return
	}
	if e.activated && c.cfg.OnActivate != nil && !h.queued {
		h.queued = true
		c.queue = append(c.queue, h)
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
	if e.closed {
		c.finish(h, e.err)
	}
}

func (c *Client) callbacks() {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			if c.closed || c.ctx.Err() != nil || len(c.queue) == 0 {
				c.mu.Unlock()
				break
			}
			h := c.queue[0]
			c.queue[0], c.queue = nil, c.queue[1:]
			h.queued = false
			fn := c.cfg.OnActivate
			if h.suppress {
				fn = nil
			}
			c.mu.Unlock()
			if fn != nil {
				fn(h.id)
			}
		}
	}
}
