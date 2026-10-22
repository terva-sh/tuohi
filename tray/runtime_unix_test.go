//go:build linux || freebsd || netbsd

package tray

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

func trayTestBus(t *testing.T) (string, func()) {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("private tray protocol tests need dbus-daemon")
	}
	dir, err := os.MkdirTemp("", "db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1", "--address=unix:tmpdir="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case address := <-ready:
		if address == "" {
			t.Fatal("private tray bus failed to start")
		}
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
		return address, stop
	case <-time.After(5 * time.Second):
		t.Fatal("private tray bus timed out")
	}
	return "", nil
}

func trayTestConn(t *testing.T, address string) *dbus.Conn {
	t.Helper()
	c, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type trayWatcher struct {
	conn          *dbus.Conn
	props         *prop.Properties
	fail          atomic.Bool
	registrations atomic.Int32
}

func (w *trayWatcher) RegisterStatusNotifierItem(_ string) *dbus.Error {
	w.registrations.Add(1)
	if w.fail.Load() {
		return dbus.MakeFailedError(errors.New("registration rejected"))
	}
	return nil
}
func serveTrayWatcher(t *testing.T, c *dbus.Conn, host, fail bool) *trayWatcher {
	t.Helper()
	w := &trayWatcher{conn: c}
	w.fail.Store(fail)
	if err := c.Export(w, watcherPath, watcherInterface); err != nil {
		t.Fatal(err)
	}
	p, err := prop.Export(c, watcherPath, prop.Map{watcherInterface: {"IsStatusNotifierHostRegistered": {Value: host, Emit: prop.EmitTrue}}})
	if err != nil {
		t.Fatal(err)
	}
	w.props = p
	r, err := c.RequestName(watcherInterface, dbus.NameFlagDoNotQueue)
	if err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("watcher name: %v %v", r, err)
	}
	return w
}
func (w *trayWatcher) host(present bool) {
	w.props.SetMust(watcherInterface, "IsStatusNotifierHostRegistered", present)
	member := "StatusNotifierHostUnregistered"
	if present {
		member = "StatusNotifierHostRegistered"
	}
	_ = w.conn.Emit(watcherPath, watcherInterface+"."+member)
}

func awaitTrayState(t *testing.T, predicate func(Availability) bool) Availability {
	t.Helper()
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		v := CurrentAvailability()
		if predicate(v) {
			return v
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("tray state did not converge: %+v", v)
		}
	}
}
func testTrayIcon(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, c)
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func trayLayout(t *testing.T, c *dbus.Conn, name string) menuLayout {
	t.Helper()
	var revision uint32
	var layout menuLayout
	if err := c.Object(name, menuPath).Call(menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &layout); err != nil {
		t.Fatal(err)
	}
	return layout
}
func layoutChild(t *testing.T, v dbus.Variant) menuLayout {
	t.Helper()
	var n menuLayout
	if err := dbus.Store([]any{v.Value()}, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
func menuClick(t *testing.T, c *dbus.Conn, name string, id int32) {
	t.Helper()
	if err := c.Object(name, menuPath).Call(menuInterface+".Event", 0, id, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
}

func TestTrayRuntimeWireUpdatesPreserveLifetimeAndCheckbox(t *testing.T) {
	address, _ := trayTestBus(t)
	observer := trayTestConn(t, address)
	serveTrayWatcher(t, trayTestConn(t, address), true, false)
	var oldClicks, newClicks atomic.Int32
	items := []Item{{Label: "check", Checkbox: true, OnClick: func() { oldClicks.Add(1) }}, {Label: "disabled", Disabled: true, OnClick: func() { oldClicks.Add(100) }}}
	icon := testTrayIcon(t, color.RGBA{R: 255, A: 255})
	if err := Set(Config{Icon: icon, Tooltip: "initial", Items: items}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Remove)
	target := linuxT.Load()
	name := target.busName
	generation := CurrentAvailability().Generation
	awaitTrayState(t, func(v Availability) bool { return v.Registered && v.HostPresent })
	// Caller-owned slices may change immediately after Set returns.
	items[0].Label = "caller mutation"
	icon[0] = 0
	layout := trayLayout(t, observer, name)
	first := layoutChild(t, layout.V2[0])
	disabled := layoutChild(t, layout.V2[1])
	if first.V1["label"].Value() != "check" {
		t.Fatal("Set retained mutable caller menu")
	}
	menuClick(t, observer, name, disabled.V0)
	menuClick(t, observer, name, first.V0)
	if oldClicks.Load() != 1 {
		t.Fatal("disabled item fired or checkbox callback missing")
	}
	tooltip := "unread"
	updatedIcon := testTrayIcon(t, color.RGBA{B: 255, A: 255})
	if err := Update(context.Background(), Changes{Icon: updatedIcon, Tooltip: &tooltip}); err != nil {
		t.Fatal(err)
	}
	if linuxT.Load() != target || CurrentAvailability().Generation != generation {
		t.Fatal("update recreated tray lifetime")
	}
	layout = trayLayout(t, observer, name)
	first = layoutChild(t, layout.V2[0])
	if first.V1["toggle-state"].Value() != int32(1) {
		t.Fatal("appearance update reset native checkbox")
	}
	v, err := observer.Object(name, sniPath).GetProperty(sniInterface + ".ToolTip")
	if err != nil {
		t.Fatal(err)
	}
	var tip dbusTooltip
	if err := dbus.Store([]any{v.Value()}, &tip); err != nil || tip.Text != "unread" {
		t.Fatalf("tooltip: %+v %v", tip, err)
	}
	newItems := []Item{{Label: "replacement", Checked: true, Disabled: true, OnClick: func() { newClicks.Add(100) }}, {Label: "enabled", OnClick: func() { newClicks.Add(1) }}}
	if err := Update(context.Background(), Changes{Items: &newItems}); err != nil {
		t.Fatal(err)
	}
	menuClick(t, observer, name, first.V0) // stale ID must never invoke a replacement
	layout = trayLayout(t, observer, name)
	menuClick(t, observer, name, layoutChild(t, layout.V2[0]).V0)
	menuClick(t, observer, name, layoutChild(t, layout.V2[1]).V0)
	if oldClicks.Load() != 1 || newClicks.Load() != 1 {
		t.Fatal("stale/disabled menu event invoked a callback")
	}
	invalid := []byte("not a PNG")
	badTip := "must not apply"
	if err := Update(context.Background(), Changes{Icon: invalid, Tooltip: &badTip}); err == nil {
		t.Fatal("invalid PNG update succeeded")
	}
	empty := ""
	noItems := []Item{}
	if err := Update(context.Background(), Changes{Icon: []byte{}, Title: &empty, Tooltip: &empty, Items: &noItems}); err != nil {
		t.Fatal(err)
	}
	if len(trayLayout(t, observer, name).V2) != 0 {
		t.Fatal("empty menu patch did not remove items")
	}
	v, err = observer.Object(name, sniPath).GetProperty(sniInterface + ".IconPixmap")
	if err != nil {
		t.Fatal(err)
	}
	var pixmaps []dbusPixmap
	if err := dbus.Store([]any{v.Value()}, &pixmaps); err != nil || len(pixmaps) != 0 {
		t.Fatalf("clear icon: %v %v", pixmaps, err)
	}
}

func TestTrayAvailabilityMissingFailingAndReturningHost(t *testing.T) {
	address, stopBus := trayTestBus(t)
	if err := Set(Config{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Remove)
	initial := awaitTrayState(t, func(v Availability) bool { return v.Running && !v.Registered && v.Err != nil })
	w := serveTrayWatcher(t, trayTestConn(t, address), false, true)
	awaitTrayState(t, func(v Availability) bool {
		return w.registrations.Load() > 0 && !v.Registered && v.Err != nil && strings.Contains(v.Err.Error(), "rejected")
	})
	w.fail.Store(false)
	w.host(false)
	awaitTrayState(t, func(v Availability) bool { return v.Registered && !v.HostPresent && v.Err != nil })
	w.host(true)
	awaitTrayState(t, func(v Availability) bool { return v.Registered && v.HostPresent && v.Err == nil })
	w.host(false)
	awaitTrayState(t, func(v Availability) bool { return v.Registered && !v.HostPresent })
	if _, err := w.conn.ReleaseName(watcherInterface); err != nil {
		t.Fatal(err)
	}
	awaitTrayState(t, func(v Availability) bool { return !v.Registered && !v.HostPresent })
	serveTrayWatcher(t, trayTestConn(t, address), true, false)
	v := awaitTrayState(t, func(v Availability) bool { return v.Registered && v.HostPresent })
	if v.Generation != initial.Generation {
		t.Fatal("watcher recovery recreated tray")
	}
	stopBus()
	awaitTrayState(t, func(v Availability) bool { return v.Running && !v.Registered && errors.Is(v.Err, ErrUnavailable) })
	if err := Update(context.Background(), Changes{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("update on disconnected bus: %v", err)
	}
	Remove()
	if CurrentAvailability().Running {
		t.Fatal("removed tray still running")
	}
}

func TestTrayQueuedUpdateDeadlineAndConcurrentRemoval(t *testing.T) {
	trayTestBus(t)
	if err := Set(Config{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Remove)
	target := linuxT.Load()
	target.ops <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	text := "queued"
	if err := Update(ctx, Changes{Tooltip: &text}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-target.ops
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			err := Update(context.Background(), Changes{Tooltip: &text})
			if err != nil && !errors.Is(err, ErrNotRunning) {
				t.Error(err)
			}
		})
	}
	Remove()
	wg.Wait()
	if err := Set(Config{Tooltip: "new lifetime"}); err != nil {
		t.Fatal(err)
	}
	if CurrentAvailability().Generation == target.availability.state.Generation {
		t.Fatal("tray generation reused")
	}
}
