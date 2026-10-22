//go:build linux || freebsd || netbsd

package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
)

type dbusBackend struct {
	conn    *dbus.Conn
	owner   string
	name    string
	caps    Capabilities
	actions bool
	alive   atomic.Bool
	event   func(managedEvent)
	signals chan *dbus.Signal
}

func openManagedBackend(ctx context.Context, cfg Config, event func(managedEvent)) (managedBackend, error) {
	c, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot connect to the notification session bus", ErrUnavailable)
	}
	return newDBusBackend(ctx, c, cfg, event)
}

// The connection is dedicated to this client. Bind operations and notification
// signals to one unique daemon owner: IDs from a restarted daemon are unrelated.
func newDBusBackend(ctx context.Context, c *dbus.Conn, cfg Config, event func(managedEvent)) (_ managedBackend, err error) {
	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()
	b := &dbusBackend{conn: c, name: cfg.Name, event: event, signals: make(chan *dbus.Signal, 32)}
	if b.name == "" {
		b.name = filepath.Base(os.Args[0])
	}
	c.Signal(b.signals)
	if err = c.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, notifInterface)); err != nil {
		return nil, fmt.Errorf("%w: cannot observe notification service lifetime", ErrUnavailable)
	}
	if err = c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, notifInterface).Store(&b.owner); err != nil {
		return nil, managedDBusError(ctx, err)
	}
	if err = c.AddMatchSignal(dbus.WithMatchSender(b.owner), dbus.WithMatchInterface(notifInterface), dbus.WithMatchObjectPath(notifPath)); err != nil {
		return nil, fmt.Errorf("%w: cannot observe notification actions", ErrUnavailable)
	}
	var capabilities []string
	if err = c.Object(b.owner, notifPath).CallWithContext(ctx, notifInterface+".GetCapabilities", 0).Store(&capabilities); err != nil {
		return nil, managedDBusError(ctx, err)
	}
	for _, capability := range capabilities {
		if capability == "actions" {
			b.caps.Activation = true
		}
	}
	b.caps.Replacement, b.caps.Dismissal = true, true
	b.actions = cfg.OnActivate != nil && b.caps.Activation
	b.alive.Store(true)
	go b.listen()
	return b, nil
}

func (b *dbusBackend) capabilities() Capabilities { return b.caps }

func (b *dbusBackend) permission(ctx context.Context, _ bool) (PermissionStatus, error) {
	if err := ctx.Err(); err != nil {
		return PermissionUnknown, err
	}
	if !b.alive.Load() {
		return PermissionUnknown, ErrUnavailable
	}
	return PermissionNotRequired, nil
}

func (b *dbusBackend) post(ctx context.Context, _, replace string, n Notification) (string, error) {
	if !b.alive.Load() {
		return "", ErrUnavailable
	}
	var replaces uint32
	if replace != "" {
		v, err := strconv.ParseUint(replace, 10, 32)
		if err != nil {
			return "", errors.New("notify: invalid native notification identity")
		}
		replaces = uint32(v)
	}
	icon := n.Options.Icon
	if icon != "" {
		if _, err := os.Stat(icon); err == nil {
			icon, _ = filepath.Abs(icon)
		}
	}
	level := n.Options.Urgency.level()
	hints := map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(level))}
	if level == 2 {
		hints["sound-name"] = dbus.MakeVariant("bell")
	}
	if len(n.Options.IconData) > 0 {
		image, err := bytesToRGBA(n.Options.IconData)
		if err != nil {
			return "", errors.New("notify: invalid PNG notification icon")
		}
		hints["image-data"] = dbus.MakeVariant(imageDataHint(image))
	}
	actions := []string{}
	if b.actions {
		actions = []string{"default", "Open"}
	}
	var id uint32
	err := b.conn.Object(b.owner, notifPath).CallWithContext(ctx, notifInterface+".Notify", 0,
		b.name, replaces, icon, n.Title, n.Message, actions, hints, int32(-1)).Store(&id)
	if err != nil {
		return "", managedDBusError(ctx, err)
	}
	if id == 0 {
		return "", fmt.Errorf("%w: notification service returned no identity", ErrUnavailable)
	}
	return strconv.FormatUint(uint64(id), 10), nil
}

func (b *dbusBackend) dismiss(ctx context.Context, id string) error {
	if !b.alive.Load() {
		return ErrUnavailable
	}
	v, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return errors.New("notify: invalid native notification identity")
	}
	err = b.conn.Object(b.owner, notifPath).CallWithContext(ctx, notifInterface+".CloseNotification", 0, uint32(v)).Err
	if err != nil {
		return managedDBusError(ctx, err)
	}
	return nil
}

func (b *dbusBackend) close() error {
	b.alive.Store(false)
	b.conn.RemoveSignal(b.signals)
	return b.conn.Close()
}

func managedDBusError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var native dbus.Error
	if errors.As(err, &native) {
		switch native.Name {
		case "org.freedesktop.DBus.Error.UnknownMethod":
			return ErrUnsupported
		case "org.freedesktop.DBus.Error.AccessDenied":
			return ErrPermissionDenied
		}
	}
	// A daemon can echo request content in its error body. Do not copy that
	// body or the application routing key into the returned error or logs.
	return fmt.Errorf("%w: notification service request failed", ErrUnavailable)
}

func (b *dbusBackend) listen() {
	defer b.conn.RemoveSignal(b.signals)
	for {
		select {
		case <-b.conn.Context().Done():
			if b.alive.Swap(false) {
				b.event(managedEvent{err: ErrUnavailable})
			}
			return
		case signal, ok := <-b.signals:
			if !ok {
				return
			}
			if signal.Name == "org.freedesktop.DBus.NameOwnerChanged" && signal.Sender == "org.freedesktop.DBus" && len(signal.Body) == 3 {
				name, _ := signal.Body[0].(string)
				owner, _ := signal.Body[2].(string)
				if name == notifInterface && owner != b.owner {
					if b.alive.Swap(false) {
						b.event(managedEvent{err: ErrUnavailable})
					}
					return
				}
				continue
			}
			if signal.Sender != b.owner || signal.Path != notifPath || len(signal.Body) != 2 {
				continue
			}
			id, ok := signal.Body[0].(uint32)
			if !ok {
				continue
			}
			e := managedEvent{native: strconv.FormatUint(uint64(id), 10)}
			switch signal.Name {
			case notifInterface + ".ActionInvoked":
				action, _ := signal.Body[1].(string)
				if !b.actions || action != "default" {
					continue
				}
				e.activated = true
			case notifInterface + ".NotificationClosed":
				if _, ok := signal.Body[1].(uint32); !ok {
					continue
				}
				e.closed = true
			default:
				continue
			}
			b.event(e)
		}
	}
}
