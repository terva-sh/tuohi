//go:build linux || freebsd || netbsd

// Unix backend (Linux, FreeBSD, NetBSD): a StatusNotifierItem (SNI) icon plus a
// com.canonical.dbusmenu menu, both served over the session D-Bus through
// github.com/godbus/dbus/v5 (pure Go, no cgo). SNI is the tray protocol of
// modern desktops (KDE, GNOME with AppIndicator, XFCE, Cinnamon, MATE, ...)
// and supersedes the legacy XEmbed system tray.
//
// Set connects to the session bus, claims an org.kde.StatusNotifierItem-*
// bus name, exports the SNI object (/StatusNotifierItem) and the dbusmenu
// object (/MenuBar), and registers with the StatusNotifierWatcher. The icon
// is shipped as an ARGB32 IconPixmap; the tooltip via the SNI Title/ToolTip
// properties. The desktop pulls the menu tree on demand through GetLayout;
// activating an item arrives as a dbusmenu Event("clicked"), which flips
// checkbox items (emitting ItemsPropertiesUpdated) before invoking OnClick.
// Tray-level clicks arrive as SNI Activate / SecondaryActivate / ContextMenu
// calls.
//
// Run blocks until Remove/Stop releases the bus name and closes the
// connection. A session bus must be running (a real desktop session provides
// one); without it Set returns an error.

package tray

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/png" // registers the PNG decoder for tray icons
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// D-Bus well-known names, interfaces and object paths of the SNI protocol.
const (
	sniInterface     = "org.kde.StatusNotifierItem"
	sniPath          = "/StatusNotifierItem"
	menuInterface    = "com.canonical.dbusmenu"
	menuPath         = "/MenuBar"
	watcherInterface = "org.kde.StatusNotifierWatcher"
	watcherPath      = "/StatusNotifierWatcher"

	sniStatusPassive = "Passive"
	sniStatusActive  = "Active"

	dbusDirectionIn  = "in"
	dbusDirectionOut = "out"
)

// dbusPixmap is one icon pixmap of the SNI protocol: (iiay) width, height,
// ARGB32 big-endian pixel data.
type dbusPixmap struct {
	W    int32
	H    int32
	Data []byte
}

// dbusTooltip mirrors the SNI ToolTip property: (sa(iiay)ss).
type dbusTooltip struct {
	IconName string
	Pixmaps  []dbusPixmap
	Title    string
	Text     string
}

// itemState is one declarative menu item plus its runtime checkbox state.
type itemState struct {
	item    Item
	checked bool
}

// linuxTray is the running SNI service backing the single active tray.
type linuxTray struct {
	ops             chan struct{}
	closed          bool
	cfg             Config
	availability    *availabilitySource
	signals         chan *dbus.Signal
	watcherOwner    string
	registeredOwner string
	menuBase        int32
	menuNext        int32
	conn            *dbus.Conn
	props           *prop.Properties
	menuProps       *prop.Properties
	busName         string

	mu         sync.RWMutex
	menuRoot   []Item
	menuByID   map[int32]*itemState // dbus id -> item (DFS order, root items from 1)
	menuRev    uint32
	iconPixmap []dbusPixmap
	tooltip    string
	status     string

	onClickFn       func()
	onDoubleClickFn func()
	onRightClickFn  func()

	quit chan struct{}
}

// --- menu layout D-Bus types (com.canonical.dbusmenu) ---

// menuLayout is one node of the layout tree: (ia{sv}av) id, properties,
// children. A zero id is the root node.
type menuLayout struct {
	V0 int32
	V1 map[string]dbus.Variant
	V2 []dbus.Variant
}

// menuItemProps is one GetGroupProperties / ItemsPropertiesUpdated entry: (ia{sv}).
type menuItemProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

// menuItemRemovedProps is one removed-properties entry: (ias).
type menuItemRemovedProps struct {
	ID    int32
	Props []string
}

// menuEvent is one EventGroup entry: (isvu).
type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

// --- lifecycle ---

var (
	linuxMu sync.Mutex
	linuxT  atomic.Pointer[linuxTray]
)

// set opens the session-bus connection, exports the SNI and dbusmenu
// objects, and makes the icon visible. Requires a running session bus.
func set(cfg Config) error { _, err := startLinuxTray(cfg); return err }

func startLinuxTray(cfg Config) (*linuxTray, error) {
	// Resolve startup fallbacks once; nil updates preserve effective values.
	if cfg.Tooltip == "" {
		cfg.Tooltip = cfg.Title
	}
	cfg.Title = cfg.Tooltip
	linuxMu.Lock()
	defer linuxMu.Unlock()
	if linuxT.Load() != nil {
		return nil, ErrAlreadyRunning
	}

	t := &linuxTray{
		cfg: cfg, ops: make(chan struct{}, 1), menuNext: 1, signals: make(chan *dbus.Signal, 32),
		menuByID:        make(map[int32]*itemState),
		status:          sniStatusActive,
		onClickFn:       cfg.OnClick,
		onDoubleClickFn: cfg.OnDoubleClick,
		onRightClickFn:  cfg.OnRightClick,
		quit:            make(chan struct{}),
	}
	if err := t.create(cfg); err != nil {
		return nil, err
	}
	t.availability = newAvailability(cfg.OnAvailability)
	linuxT.Store(t)
	t.availability.publish(Availability{Running: true, Err: ErrUnavailable})
	go t.watchAvailability()
	return t, nil
}

// run shows the tray and blocks until Remove/Stop destroys it.
func run(cfg Config) error {
	t, err := startLinuxTray(cfg)
	if err != nil {
		return err
	}
	<-t.quit
	return nil
}

// stop destroys the running tray (if any). Safe from any goroutine.
func stop() {
	linuxMu.Lock()
	defer linuxMu.Unlock()
	if t := linuxT.Swap(nil); t != nil {
		t.destroy()
	}
}

func remove() { stop() }

func update(ctx context.Context, c Changes) error {
	t := linuxT.Load()
	if t == nil {
		return ErrNotRunning
	}
	select {
	case t.ops <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-t.ops }()
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.RLock()
	closed := t.closed
	t.mu.RUnlock()
	if closed {
		return ErrNotRunning
	}
	if t.conn.Context().Err() != nil {
		return ErrUnavailable
	}
	cfg := applyChanges(t.cfg, c)
	var pixmaps []dbusPixmap
	if c.Icon != nil && len(cfg.Icon) > 0 {
		w, h, data, err := pngToARGB(cfg.Icon)
		if err != nil {
			return err
		}
		pixmaps = []dbusPixmap{{W: int32(w), H: int32(h), Data: data}}
	}
	if c.Items != nil && int64(t.menuNext)+int64(itemCount(cfg.Items)) > 1<<31-1 {
		return fmt.Errorf("tray: menu identities exhausted; recreate the tray")
	}
	t.cfg = cfg
	if c.Icon != nil {
		t.publishIcon(pixmaps)
	}
	if c.Title != nil || c.Tooltip != nil {
		t.setText(cfg.Title, cfg.Tooltip)
	}
	if c.Items != nil {
		t.setMenu(cfg.Items)
	}
	if t.conn.Context().Err() != nil {
		return ErrUnavailable
	}
	return nil
}

func itemCount(items []Item) int {
	n := len(items)
	for _, it := range items {
		n += itemCount(it.Submenu)
	}
	return n
}

func bounds() (x, y, w, h int) { return 0, 0, 0, 0 }

// create connects and exports everything, then applies the config.
func (t *linuxTray) create(cfg Config) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("tray: connect to session bus: %w", err)
	}
	t.conn = conn

	// Unique bus name following the SNI convention (PID + tray number).
	t.busName = fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	reply, err := conn.RequestName(t.busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: request bus name %s: %w", t.busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return fmt.Errorf("tray: bus name %s already taken (reply=%d)", t.busName, reply)
	}

	// StatusNotifierItem object.
	id := cfg.AppName
	if id == "" {
		id = t.busName
	}
	sniSvc := &sniService{tray: t}
	if err := conn.Export(sniSvc, sniPath, sniInterface); err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export SNI service: %w", err)
	}
	sniProps, err := prop.Export(conn, sniPath, prop.Map{
		sniInterface: {
			"Category":      {Value: "ApplicationStatus", Writable: false, Emit: prop.EmitConst, Callback: nil},
			"Id":            {Value: id, Writable: false, Emit: prop.EmitConst, Callback: nil},
			"Title":         {Value: "", Writable: false, Emit: prop.EmitTrue, Callback: nil},
			"Status":        {Value: t.status, Writable: false, Emit: prop.EmitTrue, Callback: nil},
			"IconName":      {Value: "", Writable: false, Emit: prop.EmitTrue, Callback: nil},
			"IconPixmap":    {Value: []dbusPixmap{}, Writable: false, Emit: prop.EmitTrue, Callback: nil},
			"ToolTip":       {Value: dbusTooltip{}, Writable: false, Emit: prop.EmitTrue, Callback: nil},
			"Menu":          {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitConst, Callback: nil},
			"ItemIsMenu":    {Value: true, Writable: false, Emit: prop.EmitConst, Callback: nil},
			"WindowId":      {Value: int32(0), Writable: false, Emit: prop.EmitConst, Callback: nil},
			"IconThemePath": {Value: "", Writable: false, Emit: prop.EmitConst, Callback: nil},
		},
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export SNI properties: %w", err)
	}
	t.props = sniProps

	sniIntro := introspect.NewIntrospectable(&introspect.Node{
		Name: sniPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name: sniInterface,
				Methods: []introspect.Method{
					{Name: "Activate", Args: []introspect.Arg{
						{Name: "x", Type: "i", Direction: dbusDirectionIn},
						{Name: "y", Type: "i", Direction: dbusDirectionIn},
					}},
					{Name: "SecondaryActivate", Args: []introspect.Arg{
						{Name: "x", Type: "i", Direction: dbusDirectionIn},
						{Name: "y", Type: "i", Direction: dbusDirectionIn},
					}},
					{Name: "ContextMenu", Args: []introspect.Arg{
						{Name: "x", Type: "i", Direction: dbusDirectionIn},
						{Name: "y", Type: "i", Direction: dbusDirectionIn},
					}},
					{Name: "Scroll", Args: []introspect.Arg{
						{Name: "delta", Type: "i", Direction: dbusDirectionIn},
						{Name: "orientation", Type: "s", Direction: dbusDirectionIn},
					}},
				},
				Signals: []introspect.Signal{
					{Name: "NewTitle"},
					{Name: "NewIcon"},
					{Name: "NewToolTip"},
					{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
				},
				Properties: sniProps.Introspection(sniInterface),
			},
		},
	})
	if err := conn.Export(sniIntro, sniPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export SNI introspection: %w", err)
	}

	// dbusmenu object.
	menuSvc := &dbusMenuService{tray: t}
	if err := conn.Export(menuSvc, menuPath, menuInterface); err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export dbusmenu service: %w", err)
	}
	menuPropMap, err := prop.Export(conn, menuPath, prop.Map{
		menuInterface: {
			"Version":       {Value: uint32(3), Writable: false, Emit: prop.EmitConst, Callback: nil},
			"TextDirection": {Value: "ltr", Writable: false, Emit: prop.EmitConst, Callback: nil},
			"Status":        {Value: "normal", Writable: false, Emit: prop.EmitConst, Callback: nil},
			"IconThemePath": {Value: []string{}, Writable: false, Emit: prop.EmitConst, Callback: nil},
		},
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export dbusmenu properties: %w", err)
	}
	t.menuProps = menuPropMap

	menuIntro := introspect.NewIntrospectable(&introspect.Node{
		Name: menuPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name: menuInterface,
				Methods: []introspect.Method{
					{Name: "GetLayout", Args: []introspect.Arg{
						{Name: "parentId", Type: "i", Direction: dbusDirectionIn},
						{Name: "recursionDepth", Type: "i", Direction: dbusDirectionIn},
						{Name: "propertyNames", Type: "as", Direction: dbusDirectionIn},
						{Name: "revision", Type: "u", Direction: dbusDirectionOut},
						{Name: "layout", Type: "(ia{sv}av)", Direction: dbusDirectionOut},
					}},
					{Name: "GetGroupProperties", Args: []introspect.Arg{
						{Name: "ids", Type: "ai", Direction: dbusDirectionIn},
						{Name: "propertyNames", Type: "as", Direction: dbusDirectionIn},
						{Name: "properties", Type: "a(ia{sv})", Direction: dbusDirectionOut},
					}},
					{Name: "Event", Args: []introspect.Arg{
						{Name: "id", Type: "i", Direction: dbusDirectionIn},
						{Name: "eventId", Type: "s", Direction: dbusDirectionIn},
						{Name: "data", Type: "v", Direction: dbusDirectionIn},
						{Name: "timestamp", Type: "u", Direction: dbusDirectionIn},
					}},
					{Name: "AboutToShow", Args: []introspect.Arg{
						{Name: "id", Type: "i", Direction: dbusDirectionIn},
						{Name: "needUpdate", Type: "b", Direction: dbusDirectionOut},
					}},
					{Name: "EventGroup", Args: []introspect.Arg{
						{Name: "events", Type: "a(isvu)", Direction: dbusDirectionIn},
						{Name: "idErrors", Type: "ai", Direction: dbusDirectionOut},
					}},
					{Name: "AboutToShowGroup", Args: []introspect.Arg{
						{Name: "ids", Type: "ai", Direction: dbusDirectionIn},
						{Name: "updatesNeeded", Type: "ai", Direction: dbusDirectionOut},
						{Name: "idErrors", Type: "ai", Direction: dbusDirectionOut},
					}},
				},
				Signals: []introspect.Signal{
					{Name: "LayoutUpdated", Args: []introspect.Arg{
						{Name: "revision", Type: "u"},
						{Name: "parent", Type: "i"},
					}},
					{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{
						{Name: "updatedProps", Type: "a(ia{sv})"},
						{Name: "removedProps", Type: "a(ias)"},
					}},
				},
				Properties: menuPropMap.Introspection(menuInterface),
			},
		},
	})
	if err := conn.Export(menuIntro, menuPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		_ = conn.Close()
		return fmt.Errorf("tray: export dbusmenu introspection: %w", err)
	}

	// Subscribe before initial registration so watcher/host changes cannot be missed.
	conn.Signal(t.signals)
	for _, options := range [][]dbus.MatchOption{
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, watcherInterface)},
		{dbus.WithMatchInterface(watcherInterface), dbus.WithMatchObjectPath(watcherPath)},
		{dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchObjectPath(watcherPath)},
	} {
		if err := conn.AddMatchSignal(options...); err != nil {
			_ = conn.Close()
			return fmt.Errorf("tray: observe watcher: %w", err)
		}
	}

	// Apply the declarative config: icon, tooltip, menu, status.
	if len(cfg.Icon) > 0 {
		if err := t.setIcon(cfg.Icon); err != nil {
			_ = conn.Close()
			return err
		}
	}
	t.setText(cfg.Title, cfg.Tooltip)
	if len(cfg.Items) > 0 {
		t.setMenu(cfg.Items)
	}

	return nil
}

// Watcher refreshes always query a current unique owner, never stale signal IDs.
func (t *linuxTray) watchAvailability() {
	defer t.conn.RemoveSignal(t.signals)
	retry := time.NewTicker(5 * time.Second)
	defer retry.Stop()
	t.refreshAvailability()
	for {
		select {
		case <-t.quit:
			return
		case <-t.conn.Context().Done():
			t.availability.publish(Availability{Running: true, Err: fmt.Errorf("%w: session bus disconnected; recreate the tray", ErrUnavailable)})
			return
		case sig, ok := <-t.signals:
			if !ok {
				t.availability.publish(Availability{Running: true, Err: fmt.Errorf("%w: session bus disconnected; recreate the tray", ErrUnavailable)})
				return
			}
			if sig.Sender != "org.freedesktop.DBus" && sig.Sender != t.watcherOwner {
				continue
			}
			t.refreshAvailability()
		case <-retry.C:
			t.refreshAvailability()
		}
	}
}

func (t *linuxTray) refreshAvailability() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v := Availability{Running: true}
	var owner string
	if err := t.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, watcherInterface).Store(&owner); err != nil {
		t.watcherOwner = ""
		t.registeredOwner = ""
		v.Err = fmt.Errorf("%w: no status notifier watcher", ErrUnavailable)
		t.availability.publish(v)
		return
	}
	t.watcherOwner = owner
	if t.registeredOwner != owner {
		t.registeredOwner = ""
		if err := t.conn.Object(owner, watcherPath).CallWithContext(ctx, watcherInterface+".RegisterStatusNotifierItem", 0, t.busName).Err; err != nil {
			v.Err = fmt.Errorf("%w: watcher rejected tray registration", ErrUnavailable)
			t.availability.publish(v)
			return
		}
		t.registeredOwner = owner
	}
	var host dbus.Variant
	hostErr := t.conn.Object(owner, watcherPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, watcherInterface, "IsStatusNotifierHostRegistered").Store(&host)
	var current string
	if err := t.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, watcherInterface).Store(&current); err != nil || current != owner {
		t.watcherOwner = ""
		t.registeredOwner = ""
		v.Err = fmt.Errorf("%w: watcher changed during registration", ErrUnavailable)
	} else {
		v.Registered = true
		var valid bool
		v.HostPresent, valid = host.Value().(bool)
		if hostErr != nil || !valid {
			v.Err = fmt.Errorf("%w: watcher host state unavailable", ErrUnavailable)
		} else if !v.HostPresent {
			v.Err = fmt.Errorf("%w: no status notifier host", ErrUnavailable)
		}
	}
	t.availability.publish(v)
}

func (t *linuxTray) destroy() {
	t.ops <- struct{}{}
	defer func() { <-t.ops }()
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	close(t.quit)
	t.mu.Unlock()
	t.availability.close()
	// Keep conn immutable; native handlers and watcher can race with Close.
	_ = t.conn.Close()
}

// --- config application ---

func (t *linuxTray) setIcon(data []byte) error {
	w, h, argb, err := pngToARGB(data)
	if err != nil {
		return fmt.Errorf("tray: convert PNG to ARGB: %w", err)
	}
	t.publishIcon([]dbusPixmap{{W: int32(w), H: int32(h), Data: argb}})
	return nil
}
func (t *linuxTray) publishIcon(pixmaps []dbusPixmap) {
	if pixmaps == nil {
		pixmaps = []dbusPixmap{}
	}
	t.mu.Lock()
	t.iconPixmap = pixmaps
	t.mu.Unlock()
	t.props.SetMust(sniInterface, "IconPixmap", pixmaps)
	_ = t.conn.Emit(sniPath, sniInterface+".NewIcon")
}
func (t *linuxTray) setText(title, tooltip string) {
	t.mu.Lock()
	t.tooltip = tooltip
	t.mu.Unlock()
	t.props.SetMust(sniInterface, "Title", title)
	t.props.SetMust(sniInterface, "ToolTip", dbusTooltip{Title: title, Text: tooltip})
	_ = t.conn.Emit(sniPath, sniInterface+".NewTitle")
	_ = t.conn.Emit(sniPath, sniInterface+".NewToolTip")
}

// setMenu replaces the whole menu tree, assigning depth-first ids (root
// items start at 1) and bumping the layout revision.
func (t *linuxTray) setMenu(items []Item) {
	t.mu.Lock()
	t.menuRoot = items
	t.menuByID = make(map[int32]*itemState)
	next := t.menuNext
	t.menuBase = next
	var walk func([]Item)
	walk = func(level []Item) {
		for i := range level {
			it := &level[i]
			st := &itemState{item: *it, checked: it.Checked}
			t.menuByID[next] = st
			next++
			if it.Submenu != nil {
				walk(it.Submenu)
			}
		}
	}
	walk(items)
	t.menuNext = next
	t.menuRev++
	rev := t.menuRev
	t.mu.Unlock()
	_ = t.conn.Emit(menuPath, menuInterface+".LayoutUpdated", rev, int32(0))
}

// --- SNI D-Bus service ---

type sniService struct{ tray *linuxTray }

// Activate is the primary (left) click on the tray icon.
func (s *sniService) Activate(_, _ int32) *dbus.Error {
	if s.tray.onClickFn != nil {
		s.tray.onClickFn()
	}
	return nil
}

// SecondaryActivate is a secondary click (the middle button on most
// desktops).
func (s *sniService) SecondaryActivate(_, _ int32) *dbus.Error {
	if s.tray.onDoubleClickFn != nil {
		s.tray.onDoubleClickFn()
	}
	return nil
}

// ContextMenu is the right click on the icon; most desktops open the
// dbusmenu themselves and only some call this.
func (s *sniService) ContextMenu(_, _ int32) *dbus.Error {
	if s.tray.onRightClickFn != nil {
		s.tray.onRightClickFn()
	}
	return nil
}

// Scroll has no counterpart in the declarative API, so it is a no-op.
func (s *sniService) Scroll(_ int32, _ string) *dbus.Error {
	return nil
}

// --- dbusmenu D-Bus service ---

type dbusMenuService struct{ tray *linuxTray }

// GetLayout returns the menu tree below parentID (0 = root), down to
// recursionDepth levels (-1 = unlimited).
func (m *dbusMenuService) GetLayout(parentID int32, recursionDepth int32, _ []string) (uint32, menuLayout, *dbus.Error) {
	m.tray.mu.RLock()
	defer m.tray.mu.RUnlock()
	return m.tray.menuRev, m.buildLayout(parentID, recursionDepth, 0), nil
}

func (m *dbusMenuService) buildLayout(id int32, maxDepth, currentDepth int32) menuLayout {
	if id == 0 {
		rootProps := map[string]dbus.Variant{
			"children-display": dbus.MakeVariant("submenu"),
		}
		var children []dbus.Variant
		if maxDepth != 0 && m.tray.menuRoot != nil {
			children = m.buildChildren(m.tray.menuRoot, m.tray.menuBase, maxDepth, currentDepth+1)
		}
		return menuLayout{V0: 0, V1: rootProps, V2: children}
	}
	st, ok := m.tray.menuByID[id]
	if !ok {
		return menuLayout{V0: id, V1: map[string]dbus.Variant{}, V2: nil}
	}
	props := m.itemProperties(st)
	var children []dbus.Variant
	if st.item.Submenu != nil && maxDepth != 0 {
		// The first child of id sits right after id in the DFS numbering.
		children = m.buildChildren(st.item.Submenu, id+1, maxDepth, currentDepth+1)
	}
	return menuLayout{V0: id, V1: props, V2: children}
}

func (m *dbusMenuService) buildChildren(items []Item, startID, maxDepth, currentDepth int32) []dbus.Variant {
	children := make([]dbus.Variant, 0, len(items))
	nextID := startID
	for range items {
		id := nextID
		nextID++
		st, ok := m.tray.menuByID[id]
		if !ok {
			continue
		}
		props := m.itemProperties(st)
		var subChildren []dbus.Variant
		if st.item.Submenu != nil {
			if maxDepth < 0 || currentDepth < maxDepth {
				subChildren = m.buildChildren(st.item.Submenu, nextID, maxDepth, currentDepth+1)
			}
			nextID = m.advancePastSubmenu(st.item.Submenu, nextID)
		}
		children = append(children, dbus.MakeVariant(menuLayout{V0: id, V1: props, V2: subChildren}))
	}
	return children
}

func (m *dbusMenuService) advancePastSubmenu(items []Item, startID int32) int32 {
	nextID := startID
	for i := range items {
		nextID++
		it := &items[i]
		if it.Submenu != nil {
			nextID = m.advancePastSubmenu(it.Submenu, nextID)
		}
	}
	return nextID
}

// itemProperties renders one menu item as its dbusmenu property map.
func (m *dbusMenuService) itemProperties(st *itemState) map[string]dbus.Variant {
	props := make(map[string]dbus.Variant)
	it := &st.item
	switch {
	case it.Separator:
		props["type"] = dbus.MakeVariant("separator")
	case it.Submenu != nil:
		props["label"] = dbus.MakeVariant(it.Label)
		props["children-display"] = dbus.MakeVariant("submenu")
		if it.Disabled {
			props["enabled"] = dbus.MakeVariant(false)
		}
	case it.Checkbox:
		props["label"] = dbus.MakeVariant(it.Label)
		props["toggle-type"] = dbus.MakeVariant("checkmark")
		if st.checked {
			props["toggle-state"] = dbus.MakeVariant(int32(1))
		} else {
			props["toggle-state"] = dbus.MakeVariant(int32(0))
		}
		if it.Disabled {
			props["enabled"] = dbus.MakeVariant(false)
		}
	default:
		props["label"] = dbus.MakeVariant(it.Label)
		if it.Disabled {
			props["enabled"] = dbus.MakeVariant(false)
		}
		if len(it.Icon) > 0 {
			props["icon-data"] = dbus.MakeVariant(it.Icon)
		}
	}
	return props
}

// GetGroupProperties returns the properties of the requested menu items.
func (m *dbusMenuService) GetGroupProperties(ids []int32, _ []string) ([]menuItemProps, *dbus.Error) {
	m.tray.mu.RLock()
	defer m.tray.mu.RUnlock()
	result := make([]menuItemProps, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			result = append(result, menuItemProps{ID: 0, Props: map[string]dbus.Variant{
				"children-display": dbus.MakeVariant("submenu"),
			}})
			continue
		}
		if st, ok := m.tray.menuByID[id]; ok {
			result = append(result, menuItemProps{ID: id, Props: m.itemProperties(st)})
		}
	}
	return result, nil
}

// Event applies one menu activation: checkbox items toggle before OnClick
// runs.
func (m *dbusMenuService) Event(id int32, eventID string, _ dbus.Variant, _ uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	m.tray.mu.Lock()
	st, ok := m.tray.menuByID[id]
	if !ok || m.tray.closed || st.item.Disabled || st.item.Separator || st.item.Submenu != nil {
		m.tray.mu.Unlock()
		return nil
	}
	toggle := st.item.Checkbox
	if toggle {
		st.checked = !st.checked
	}
	onClick := st.item.OnClick
	checked := st.checked
	m.tray.mu.Unlock()
	if toggle {
		m.publishItemState(id, checked)
	}
	if onClick != nil {
		onClick()
	}
	return nil
}

// publishItemState broadcasts ItemsPropertiesUpdated for a single item so
// the desktop repaints its checkmark in place.
func (m *dbusMenuService) publishItemState(id int32, checked bool) {
	props := make(map[string]dbus.Variant)
	if checked {
		props["toggle-state"] = dbus.MakeVariant(int32(1))
	} else {
		props["toggle-state"] = dbus.MakeVariant(int32(0))
	}
	updated := []menuItemProps{{ID: id, Props: props}}
	removed := []menuItemRemovedProps{}
	_ = m.tray.conn.Emit(menuPath, menuInterface+".ItemsPropertiesUpdated", updated, removed)
}

// AboutToShow tells the desktop that no layout refresh is needed.
func (m *dbusMenuService) AboutToShow(_ int32) (bool, *dbus.Error) {
	return false, nil
}

// EventGroup applies a batch of events, reporting the ids that failed.
func (m *dbusMenuService) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	var idErrors []int32
	for _, ev := range events {
		if err := m.Event(ev.ID, ev.EventID, ev.Data, ev.Timestamp); err != nil {
			idErrors = append(idErrors, ev.ID)
		}
	}
	if idErrors == nil {
		idErrors = []int32{}
	}
	return idErrors, nil
}

// AboutToShowGroup tells the desktop that no layout refresh is needed.
func (m *dbusMenuService) AboutToShowGroup(_ []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

// --- PNG to ARGB32 ---

// pngToARGB decodes a PNG and returns ARGB32 big-endian pixel data as the
// SNI IconPixmap property expects: 4 bytes per pixel in A R G B order.
func pngToARGB(data []byte) (w, h int, argb []byte, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("decode PNG: %w", err)
	}
	bounds := img.Bounds()
	w, h = bounds.Dx(), bounds.Dy()
	argb = make([]byte, w*h*4)
	idx := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			binary.BigEndian.PutUint32(argb[idx:], (a>>8)<<24|(r>>8)<<16|(g>>8)<<8|(b>>8))
			idx += 4
		}
	}
	return w, h, argb, nil
}
