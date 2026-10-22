package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type managedFake struct {
	mu                sync.Mutex
	caps              Capabilities
	event             func(managedEvent)
	posts             []Notification
	replacements      []string
	dismissed         []string
	closed            int
	postErr           error
	dismissErr        error
	permissionWait    chan struct{}
	permissionStarted sync.Once
	postEvent         func(string)
}

func (f *managedFake) capabilities() Capabilities { return f.caps }
func (f *managedFake) permission(ctx context.Context, _ bool) (PermissionStatus, error) {
	if f.permissionWait != nil {
		f.permissionStarted.Do(func() { close(f.permissionWait) })
		<-ctx.Done()
		return PermissionUnknown, ctx.Err()
	}
	return PermissionAuthorized, nil
}
func (f *managedFake) post(_ context.Context, id, replace string, n Notification) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postErr != nil {
		return "", f.postErr
	}
	f.posts = append(f.posts, n)
	f.replacements = append(f.replacements, replace)
	if f.postEvent != nil {
		f.postEvent(id)
	}
	return id, nil
}
func (f *managedFake) dismiss(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dismissErr != nil {
		return f.dismissErr
	}
	f.dismissed = append(f.dismissed, id)
	return nil
}
func (f *managedFake) close() error { f.mu.Lock(); defer f.mu.Unlock(); f.closed++; return nil }

func fakeClient(t *testing.T, ctx context.Context, cfg Config, f *managedFake) *Client {
	t.Helper()
	c, err := newClient(ctx, cfg, func(_ context.Context, _ Config, event func(managedEvent)) (managedBackend, error) {
		f.event = event
		return f, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func fullManagedFake() *managedFake {
	return &managedFake{caps: Capabilities{Activation: true, Replacement: true, Dismissal: true, Permission: true}}
}

func awaitNotification(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notification operation did not finish")
	}
}

func TestManagedCorrelationReplacementAndDismissal(t *testing.T) {
	ctx := context.Background()
	activated := make(chan string, 4)
	f := fullManagedFake()
	c := fakeClient(t, ctx, Config{OnActivate: func(id string) { activated <- id }}, f)
	h, err := c.Post(ctx, Notification{ID: "opaque-route-1", Title: "Activity"})
	if err != nil {
		t.Fatal(err)
	}
	if h.native == h.ID() || h.ID() != "opaque-route-1" {
		t.Fatal("application and OS correlation were not separated")
	}
	if _, err := c.Post(ctx, Notification{ID: h.ID()}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate Post = %v", err)
	}
	if err := h.Replace(ctx, Notification{Title: "New activity"}); err != nil {
		t.Fatal(err)
	}
	if f.replacements[0] != "" || f.replacements[1] != h.native {
		t.Fatal("replacement lost native correlation")
	}
	if err := h.Replace(ctx, Notification{ID: "different"}); err == nil {
		t.Fatal("replacement changed application identity")
	}
	f.event(managedEvent{native: "unknown", activated: true})
	f.event(managedEvent{native: h.native, activated: true})
	select {
	case id := <-activated:
		if id != h.ID() {
			t.Fatal("activation routed to wrong application ID")
		}
	case <-time.After(time.Second):
		t.Fatal("missing activation")
	}
	if err := h.Dismiss(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.Dismiss(ctx); err != nil {
		t.Fatal(err)
	}
	awaitNotification(t, h.Done())
	if h.Err() != nil || len(f.dismissed) != 1 {
		t.Fatal("dismiss was not idempotent")
	}
	f.event(managedEvent{native: h.native, activated: true})
	select {
	case <-activated:
		t.Fatal("closed notification activated")
	default:
	}
	if err := h.Replace(ctx, Notification{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Replace closed = %v", err)
	}
}

func TestManagedCloseDropsQueuedActivationAndCallbackMayClose(t *testing.T) {
	ctx := context.Background()
	first := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var c *Client
	f := fullManagedFake()
	c = fakeClient(t, ctx, Config{OnActivate: func(id string) {
		if id != "first" {
			t.Error("dismissed queued activation was delivered")
			return
		}
		close(first)
		<-release
		_ = c.Close()
		close(finished)
	}}, f)
	h1, _ := c.Post(ctx, Notification{ID: "first"})
	h2, _ := c.Post(ctx, Notification{ID: "second"})
	f.event(managedEvent{native: h1.native, activated: true})
	awaitNotification(t, first)
	f.event(managedEvent{native: h2.native, activated: true})
	if err := h2.Dismiss(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitNotification(t, finished)
	awaitNotification(t, h1.Done())
	if !errors.Is(h1.Err(), ErrClosed) {
		t.Fatalf("close reason = %v", h1.Err())
	}
}

func TestManagedDismissDropsQueuedActivation(t *testing.T) {
	ctx := context.Background()
	first := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	observed := make(chan string, 4)
	f := fullManagedFake()
	c := fakeClient(t, ctx, Config{OnActivate: func(id string) {
		if id == "first" {
			close(first)
			<-release
			return
		}
		observed <- id
	}}, f)
	h1, _ := c.Post(ctx, Notification{ID: "first"})
	h2, _ := c.Post(ctx, Notification{ID: "dismissed"})
	h3, _ := c.Post(ctx, Notification{ID: "barrier"})
	f.event(managedEvent{native: h1.native, activated: true})
	awaitNotification(t, first)
	f.event(managedEvent{native: h2.native, activated: true})
	if err := h2.Dismiss(ctx); err != nil {
		t.Fatal(err)
	}
	f.event(managedEvent{native: h3.native, activated: true})
	once.Do(func() { close(release) })
	select {
	case id := <-observed:
		if id != "barrier" {
			t.Fatal("dismissed queued activation was delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("callback queue did not reach barrier")
	}
}

func TestManagedCloseInterruptsPermissionWait(t *testing.T) {
	f := fullManagedFake()
	f.permissionWait = make(chan struct{})
	c := fakeClient(t, context.Background(), Config{}, f)
	returned := make(chan struct{})
	go func() { _, _ = c.RequestPermission(context.Background()); close(returned) }()
	awaitNotification(t, f.permissionWait)
	closed := make(chan struct{})
	go func() { _ = c.Close(); close(closed) }()
	awaitNotification(t, returned)
	awaitNotification(t, closed)
}

func TestManagedQueuedOperationsRespectDeadline(t *testing.T) {
	f := fullManagedFake()
	c := fakeClient(t, context.Background(), Config{}, f)
	h, err := c.Post(context.Background(), Notification{ID: "existing"})
	if err != nil {
		t.Fatal(err)
	}
	f.permissionWait = make(chan struct{})
	permissionCtx, cancelPermission := context.WithCancel(context.Background())
	defer cancelPermission()
	permissionDone := make(chan struct{})
	go func() { _, _ = c.RequestPermission(permissionCtx); close(permissionDone) }()
	awaitNotification(t, f.permissionWait)
	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{"Permission", func(ctx context.Context) error { _, err := c.Permission(ctx); return err }},
		{"RequestPermission", func(ctx context.Context) error { _, err := c.RequestPermission(ctx); return err }},
		{"Post", func(ctx context.Context) error { _, err := c.Post(ctx, Notification{ID: "queued"}); return err }},
		{"Replace", func(ctx context.Context) error { return h.Replace(ctx, Notification{Title: "queued"}) }},
		{"Dismiss", h.Dismiss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- tc.call(ctx) }()
			select {
			case err := <-result:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("queued call = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("queued caller did not return while the permission request remained blocked")
			}
			select {
			case <-permissionDone:
				t.Fatal("queued caller cancelled the independent permission request")
			default:
			}
		})
	}
	cancelPermission()
	awaitNotification(t, permissionDone)
	f.mu.Lock()
	if len(f.posts) != 1 || len(f.dismissed) != 0 {
		t.Error("cancelled queued calls reached the native backend")
	}
	f.mu.Unlock()
	if _, err := c.Post(context.Background(), Notification{ID: "after-cancellation"}); err != nil {
		t.Fatalf("cancelled queued callers leaked operation admission: %v", err)
	}
}

func TestManagedNativeEventsBeforePostReturns(t *testing.T) {
	f := fullManagedFake()
	activated := make(chan string, 1)
	c := fakeClient(t, context.Background(), Config{OnActivate: func(id string) { activated <- id }}, f)
	f.postEvent = func(native string) {
		// A native backend may report an event before its post reply. State
		// locking must not deadlock it or lose correlation with the new ID.
		f.event(managedEvent{native: native, activated: true})
		f.event(managedEvent{native: native, closed: true})
	}
	posted := make(chan *Handle, 1)
	go func() { h, _ := c.Post(context.Background(), Notification{ID: "early"}); posted <- h }()
	select {
	case h := <-posted:
		if h == nil {
			t.Fatal("post failed")
		}
		awaitNotification(t, h.Done())
		if h.Err() != nil {
			t.Fatal(h.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("native event deadlocked Post")
	}
	select {
	case id := <-activated:
		if id != "early" {
			t.Fatal("early activation lost application correlation")
		}
	case <-time.After(time.Second):
		t.Fatal("early activation was lost")
	}
}

func TestManagedCancellationAndServiceLoss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := fullManagedFake()
	c := fakeClient(t, ctx, Config{}, f)
	h, _ := c.Post(context.Background(), Notification{ID: "first"})
	f.event(managedEvent{err: ErrUnavailable})
	awaitNotification(t, h.Done())
	if !errors.Is(h.Err(), ErrUnavailable) {
		t.Fatal("service loss was not reported")
	}
	h2, _ := c.Post(context.Background(), Notification{ID: "second"})
	cancel()
	awaitNotification(t, h2.Done())
	if !errors.Is(h2.Err(), ErrClosed) {
		t.Fatal("lifetime cancellation did not end handle")
	}
	_ = c.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed != 1 {
		t.Fatalf("backend closed %d times", f.closed)
	}
}

func TestManagedFailuresDoNotLoseLiveHandle(t *testing.T) {
	f := fullManagedFake()
	c := fakeClient(t, context.Background(), Config{}, f)
	h, _ := c.Post(context.Background(), Notification{ID: "route"})
	f.postErr = ErrUnavailable
	if err := h.Replace(context.Background(), Notification{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	f.dismissErr = ErrUnavailable
	if err := h.Dismiss(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
		t.Fatal("failed operation lost live handle")
	default:
	}
	f.postErr, f.dismissErr = nil, nil
	if err := h.Dismiss(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedUnsupportedActivationDoesNotFallBack(t *testing.T) {
	f := &managedFake{}
	_, err := newClient(context.Background(), Config{OnActivate: func(string) {}}, func(_ context.Context, _ Config, _ func(managedEvent)) (managedBackend, error) { return f, nil })
	if !errors.Is(err, ErrUnsupported) || f.closed != 1 {
		t.Fatal("unsupported activation did not fail and clean up")
	}
}
