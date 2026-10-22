//go:build linux || freebsd || netbsd

package notify

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// A private bus avoids notifying the real desktop, even inside test-gui.
func notificationTestBus(t *testing.T, serviceDirs ...string) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("private notification service test needs dbus-daemon")
	}
	dir, err := os.MkdirTemp("", "db") // short enough for a Unix socket under TMPDIR
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	args := []string{"--session", "--nofork", "--print-address=1", "--address=unix:tmpdir=" + dir}
	if len(serviceDirs) != 0 {
		// Isolate activation from the real session's installed services.
		config := `<busconfig><type>session</type><listen>unix:tmpdir=` + html.EscapeString(dir) + `</listen>`
		for _, serviceDir := range serviceDirs {
			config += `<servicedir>` + html.EscapeString(serviceDir) + `</servicedir>`
		}
		config += `<policy context="default"><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>`
		path := filepath.Join(dir, "bus.conf")
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		args = []string{"--config-file=" + path, "--nofork", "--print-address=1"}
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case address := <-ready:
		if address == "" {
			t.Fatal("private session bus did not start")
		}
		return address
	case <-time.After(5 * time.Second):
		t.Fatal("private session bus startup timed out")
	}
	return ""
}

func notificationTestConn(t *testing.T, address string) *dbus.Conn {
	t.Helper()
	c, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type notificationCall struct {
	replaces                   uint32
	name, icon, title, message string
	actions                    []string
	hints                      map[string]dbus.Variant
}

type notificationService struct {
	mu      sync.Mutex
	conn    *dbus.Conn
	calls   []notificationCall
	closed  []uint32
	next    uint32
	actions bool
	fail    bool
}

func (s *notificationService) GetCapabilities() ([]string, *dbus.Error) {
	if s.actions {
		return []string{"actions", "body"}, nil
	}
	return []string{"body"}, nil
}

func (s *notificationService) Notify(name string, replaces uint32, icon, title, message string, actions []string, hints map[string]dbus.Variant, _ int32) (uint32, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return 0, dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{"private request content must not appear in errors"})
	}
	s.calls = append(s.calls, notificationCall{replaces, name, icon, title, message, actions, hints})
	if replaces != 0 {
		return replaces, nil
	}
	s.next++
	return s.next, nil
}

func (s *notificationService) CloseNotification(id uint32) *dbus.Error {
	s.mu.Lock()
	s.closed = append(s.closed, id)
	s.mu.Unlock()
	_ = s.conn.Emit(notifPath, notifInterface+".NotificationClosed", id, uint32(3))
	return nil
}

func serveNotifications(t *testing.T, c *dbus.Conn, actions bool) *notificationService {
	t.Helper()
	s := &notificationService{conn: c, actions: actions, next: 40}
	if err := c.Export(s, notifPath, notifInterface); err != nil {
		t.Fatal(err)
	}
	owner, err := c.RequestName(notifInterface, dbus.NameFlagDoNotQueue)
	if err != nil || owner != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("notification service name: %v, %v", owner, err)
	}
	return s
}

func dbusTestClient(t *testing.T, address string, cfg Config) *Client {
	t.Helper()
	c, err := newClient(context.Background(), cfg, func(ctx context.Context, cfg Config, event func(managedEvent)) (managedBackend, error) {
		conn, err := dbus.Connect(address)
		if err != nil {
			return nil, err
		}
		return newDBusBackend(ctx, conn, cfg, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestManagedDBusWireActionsReplacementDismissal(t *testing.T) {
	address := notificationTestBus(t)
	serviceConn := notificationTestConn(t, address)
	s := serveNotifications(t, serviceConn, true)
	activated := make(chan string, 4)
	c := dbusTestClient(t, address, Config{Name: "tuohi test", OnActivate: func(id string) { activated <- id }})
	if !c.Capabilities().Activation || c.Capabilities().Permission {
		t.Fatal("incorrect protocol capabilities")
	}
	if status, err := c.RequestPermission(context.Background()); err != nil || status != PermissionNotRequired {
		t.Fatalf("permission = %v %v", status, err)
	}
	h, err := c.Post(context.Background(), Notification{ID: "application-private-route", Title: "Activity", Options: Options{Urgency: UrgencyCritical}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Replace(context.Background(), Notification{Title: "Updated"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	calls := append([]notificationCall(nil), s.calls...)
	s.mu.Unlock()
	if len(calls) != 2 || calls[0].replaces != 0 || calls[1].replaces != 41 {
		t.Fatalf("replacement wire calls = %+v", calls)
	}
	if strings.Join(calls[0].actions, ",") != "default,Open" || calls[0].hints["urgency"].Value() != byte(2) {
		t.Fatal("missing action/urgency payload")
	}
	for _, call := range calls {
		if strings.Contains(call.name+call.title+call.message+call.icon+strings.Join(call.actions, ""), h.ID()) {
			t.Fatal("application routing key leaked into OS payload")
		}
	}
	// An unrelated bus client cannot activate a tracked daemon ID.
	attacker := notificationTestConn(t, address)
	_ = attacker.Emit(notifPath, notifInterface+".ActionInvoked", uint32(41), "default")
	_ = serviceConn.Emit(notifPath, notifInterface+".ActionInvoked", uint32(999), "default")
	select {
	case <-activated:
		t.Fatal("spoofed/unknown notification activated")
	case <-time.After(50 * time.Millisecond):
	}
	if err := serviceConn.Emit(notifPath, notifInterface+".ActionInvoked", uint32(41), "default"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-activated:
		if id != h.ID() {
			t.Fatal("wrong application route")
		}
	case <-time.After(time.Second):
		t.Fatal("native action was not delivered")
	}
	if err := h.Dismiss(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitNotification(t, h.Done())
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.closed) != 1 || s.closed[0] != 41 {
		t.Fatal("wrong notification dismissed")
	}
}

func TestManagedDBusOwnerLossDoesNotAliasIDs(t *testing.T) {
	address := notificationTestBus(t)
	serviceConn := notificationTestConn(t, address)
	serveNotifications(t, serviceConn, true)
	c := dbusTestClient(t, address, Config{})
	h, err := c.Post(context.Background(), Notification{ID: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serviceConn.ReleaseName(notifInterface); err != nil {
		t.Fatal(err)
	}
	awaitNotification(t, h.Done())
	if !errors.Is(h.Err(), ErrUnavailable) {
		t.Fatalf("service loss = %v", h.Err())
	}
	newConn := notificationTestConn(t, address)
	serveNotifications(t, newConn, true) // intentionally starts IDs from 41 again
	if _, err := c.Post(context.Background(), Notification{ID: "new"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale daemon client = %v", err)
	}
	newClient := dbusTestClient(t, address, Config{})
	if _, err := newClient.Post(context.Background(), Notification{ID: "new"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDBusCapabilitiesAndPrivateErrors(t *testing.T) {
	address := notificationTestBus(t)
	serviceConn := notificationTestConn(t, address)
	s := serveNotifications(t, serviceConn, false)
	c := dbusTestClient(t, address, Config{})
	if c.Capabilities().Activation {
		t.Fatal("server without actions reported activation")
	}
	s.mu.Lock()
	s.fail = true
	s.mu.Unlock()
	_, err := c.Post(context.Background(), Notification{ID: "opaque"})
	if err == nil || strings.Contains(err.Error(), "private request content") {
		t.Fatalf("private daemon error leaked: %v", err)
	}
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("permission error = %v", err)
	}
}

// This test binary is the service executable launched by the private bus.
// A normal test run returns immediately; only the .service entry opts in.
func TestManagedDBusActivationHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "tuohi-notification-activation-helper" {
		return
	}
	address := os.Getenv("DBUS_STARTER_ADDRESS")
	if address == "" {
		t.Fatal("activation did not supply a starter bus")
	}
	c := notificationTestConn(t, address)
	serveNotifications(t, c, true)
	select {
	case <-c.Context().Done(): // the parent test stops the private bus
	case <-time.After(15 * time.Second):
		t.Fatal("activation helper outlived the private-bus test")
	}
}

func TestManagedDBusActivatesIdleService(t *testing.T) {
	dir, err := os.MkdirTemp("", "notify-services")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	service := fmt.Sprintf("[D-BUS Service]\nName=%s\nExec=%s -test.run=^TestManagedDBusActivationHelper$ -- tuohi-notification-activation-helper\n", notifInterface, strconv.Quote(executable))
	if err := os.WriteFile(filepath.Join(dir, notifInterface+".service"), []byte(service), 0600); err != nil {
		t.Fatal(err)
	}
	address := notificationTestBus(t, dir)
	observer := notificationTestConn(t, address)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var owner string
	err = observer.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, notifInterface).Store(&owner)
	var native dbus.Error
	if !errors.As(err, &native) || native.Name != "org.freedesktop.DBus.Error.NameHasNoOwner" {
		t.Fatalf("service already had an owner: %q %v", owner, err)
	}
	c, err := newClient(ctx, Config{}, func(ctx context.Context, cfg Config, event func(managedEvent)) (managedBackend, error) {
		conn, err := dbus.Connect(address)
		if err != nil {
			return nil, err
		}
		return newDBusBackend(ctx, conn, cfg, event)
	})
	if err != nil {
		t.Fatalf("client failed to activate installed service: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !c.Capabilities().Activation {
		t.Fatal("activated service's capabilities were not discovered")
	}
	h, err := c.Post(ctx, Notification{ID: "after-activation"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Replace(ctx, Notification{Title: "Updated"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Dismiss(ctx); err != nil {
		t.Fatal(err)
	}
	awaitNotification(t, h.Done())
	if err := h.Err(); err != nil {
		t.Fatal(err)
	}
	// Existing owner-loss tests cover restarted daemons reusing numeric IDs.
	// Close the activated client normally before stopping its helper's bus.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
