// macOS backend: an NSStatusItem in the menu bar with an NSMenu, via purego's
// Objective-C runtime (no cgo). The status item and its menus are AppKit
// objects, so everything here runs on the main thread; Run drives
// NSApplication's run loop and Stop wakes it from any thread through
// -performSelectorOnMainThread:.
//
// Clicks come back through a small Objective-C target class registered once
// (NativeTrayTarget): the status item button carries the trayClicked: action
// (the Config.OnClick - reachable only when no menu is attached, because
// AppKit opens the status item's menu on click instead), and each clickable
// NSMenuItem carries its flat index as a tag routed by menuItemClicked:.
// Checkbox items toggle their own state (setState:) before the OnClick runs,
// so a Checkbox item behaves like a native checkbox. macOS does not deliver
// double clicks or a separate right click for status items, so OnDoubleClick
// and OnRightClick never fire here.

package tray

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	targetClassName = "NativeTrayTarget"

	nsApplicationActivationPolicyRegular   = 0    // Dock icon and app switcher (Config.Dock)
	nsApplicationActivationPolicyAccessory = 1    // menu-bar app, no Dock icon
	nsEventTypeApplicationDefined          = 15   // for the run-loop wake event
	nsVariableStatusItemLength             = -1.0 // NSVariableStatusItemLength
	statusIconSize                         = 18   // menu-bar icon side, in points
)

type cgPoint struct{ X, Y float64 }
type nsSize struct{ W, H float64 }

// clickEntry is one clickable (non-separator, non-submenu-container) menu
// item, flattened depth-first across submenus. The native item's tag is its
// index in this slice; the runtime copy of Item tracks checkbox toggles.
type clickEntry struct {
	item    Item
	onClick func()
}

var (
	mu              sync.Mutex
	running         bool
	ownsLoop        bool // run() owns NSApplication's loop; only it may stop it
	trayClickFn     func()
	activeClicks    []clickEntry
	activeClickBase int
	nextClickTag    int
	macConfig       Config
	macAvailability *availabilitySource
	trayDispatcher  objc.ID
	macUpdates      updateQueue
	trayTarget      objc.ID
	trayItem        objc.ID

	initOnce sync.Once
	initErr  error
	selCache sync.Map // selector name -> objc.SEL
)

func sel(name string) objc.SEL {
	v, ok := selCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID {
	c := objc.GetClass(name)
	if c == 0 {
		panic(fmt.Sprintf("tray: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// ensureInit loads AppKit (for NSStatusBar/NSMenu/NSImage) and registers the
// menu-action target class once. Foundation and AppKit are usually already
// mapped inside a GUI app, but dlopen'ing them makes a bare CLI binary work
// too.
func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				initErr = fmt.Errorf("tray: load %s: %w", fw, err)
				return
			}
		}
		_, err := objc.RegisterClass(
			targetClassName, objc.GetClass("NSObject"), nil, nil,
			[]objc.MethodDef{
				{
					Cmd: sel("trayClicked:"),
					Fn: func(_self objc.ID, _cmd objc.SEL, _sender objc.ID) {
						mu.Lock()
						fn := trayClickFn
						if _self != trayTarget {
							fn = nil
						}
						mu.Unlock()
						if fn != nil {
							fn()
						}
					},
				},
				{
					Cmd: sel("menuItemClicked:"),
					Fn: func(_self objc.ID, _cmd objc.SEL, sender objc.ID) {
						tag := int(sender.Send(sel("tag"))) // #nosec G115 -- tag is an index we set
						mu.Lock()
						tag -= activeClickBase
						if _self != trayTarget || tag < 0 || tag >= len(activeClicks) {
							mu.Unlock()
							return
						}
						entry := &activeClicks[tag]
						onClick := entry.onClick
						if entry.item.Disabled {
							mu.Unlock()
							return
						}
						if entry.item.Checkbox {
							// Toggle the mark, then run OnClick, matching
							// native checkbox order.
							if entry.item.Checked {
								entry.item.Checked = false
								sender.Send(sel("setState:"), 0)
							} else {
								entry.item.Checked = true
								sender.Send(sel("setState:"), 1)
							}
						}
						mu.Unlock()
						if onClick != nil {
							onClick()
						}
					},
				},
				{
					Cmd: sel("trayStop"),
					Fn: func(self objc.ID, _ objc.SEL) {
						mu.Lock()
						current := self == trayTarget
						own := ownsLoop
						mu.Unlock()
						if !current {
							return
						}
						removeTray()
						if own {
							stopRunLoop()
						}
					},
				},
				{Cmd: sel("applyTrayUpdate:"), Fn: func(_ objc.ID, _ objc.SEL, number objc.ID) {
					token := objc.Send[uint64](number, sel("unsignedLongLongValue"))
					if r := macUpdates.take(uintptr(token)); r != nil {
						if err := r.ctx.Err(); err != nil {
							r.result <- err
							return
						}
						r.result <- applyMacUpdate(r.source, r.changes)
					}
				}},
			})
		if err != nil {
			initErr = fmt.Errorf("tray: register target class: %w", err)
			return
		}
		// A process-lifetime dispatcher is independent of status-item lifetimes.
		trayDispatcher = class(targetClassName).Send(sel("alloc")).Send(sel("init"))
	})
	return initErr
}

// set creates the status item and menu without owning the run loop (Set). It
// must be called from the UI thread.
func set(cfg Config) error {
	mu.Lock()
	if running {
		mu.Unlock()
		return ErrAlreadyRunning
	}
	err := ensureInit()
	if err != nil {
		mu.Unlock()
		return err
	}
	running = true
	ownsLoop = false
	mu.Unlock()

	app := class("NSApplication").Send(sel("sharedApplication"))
	policy := nsApplicationActivationPolicyAccessory
	if cfg.Dock {
		policy = nsApplicationActivationPolicyRegular
	}
	app.Send(sel("setActivationPolicy:"), policy)
	// Creating a status item before the app finishes launching crashes with a
	// CGSConnectionByID assertion when the process has no window-server
	// connection yet; finishLaunching is idempotent, so repeating it is safe.
	app.Send(sel("finishLaunching"))

	target := class(targetClassName).Send(sel("alloc")).Send(sel("init"))
	target.Send(sel("retain"))

	bar := class("NSStatusBar").Send(sel("systemStatusBar"))
	item := bar.Send(sel("statusItemWithLength:"), float64(nsVariableStatusItemLength))
	item.Send(sel("retain")) // the status bar does not keep it alive for us

	btn := item.Send(sel("button"))
	applyButton(btn, cfg)
	// Route a plain button click (no menu attached) to Config.OnClick. With a
	// menu attached, AppKit opens the menu on click instead of firing this.
	if btn != 0 {
		btn.Send(sel("setTarget:"), target)
		btn.Send(sel("setAction:"), sel("trayClicked:"))
	}

	// Build the NSMenu hierarchy (if any), appending each clickable item to
	// clicks. No menu means AppKit keeps delivering trayClicked: to OnClick.
	var clicks []clickEntry
	base := nextClickTag
	if len(cfg.Items) > 0 {
		nsMenu := buildMenu(cfg.Items, &clicks, target)
		if nsMenu != 0 {
			item.Send(sel("setMenu:"), nsMenu)
			nsMenu.Send(sel("release"))
		}
	}

	mu.Lock()
	trayTarget = target
	trayItem = item
	trayClickFn = cfg.OnClick
	activeClicks = clicks
	activeClickBase = base
	macConfig = cfg
	macAvailability = newAvailability(cfg.OnAvailability)
	source := macAvailability
	mu.Unlock()
	source.publish(Availability{Running: true, Registered: true, HostPresent: true})
	return nil
}

// run shows the tray and drives NSApplication's loop until Stop is called.
func run(cfg Config) error {
	if err := set(cfg); err != nil {
		return err
	}
	mu.Lock()
	ownsLoop = true
	mu.Unlock()

	runtime.LockOSThread()

	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("run")) // blocks until trayStop stops the loop

	removeTray()
	return nil
}

// removeTray detaches the status item from the system status bar and releases
// the AppKit objects. It must run on the main thread; the trayStop selector
// routes it there from any thread.
func removeTray() {
	mu.Lock()
	t, it, r, source := trayTarget, trayItem, running, macAvailability
	if !r || t == 0 {
		mu.Unlock()
		return
	}
	running = false
	ownsLoop = false
	trayTarget = 0
	trayItem = 0
	trayClickFn = nil
	activeClicks = nil
	macAvailability = nil
	macConfig = Config{}
	mu.Unlock()
	source.close()
	macUpdates.close(source)
	bar := class("NSStatusBar").Send(sel("systemStatusBar"))
	if it != 0 {
		bar.Send(sel("removeStatusItem:"), it)
		it.Send(sel("release"))
	}
	t.Send(sel("release"))
}

func update(ctx context.Context, c Changes) error {
	mu.Lock()
	source := macAvailability
	dispatcher := trayDispatcher
	mu.Unlock()
	if source == nil {
		return ErrNotRunning
	}
	if objc.Send[bool](class("NSThread"), sel("isMainThread")) {
		return applyMacUpdate(source, c)
	}
	token, r := macUpdates.add(ctx, c, source)
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	number := class("NSNumber").Send(sel("numberWithUnsignedLongLong:"), uint64(token))
	dispatcher.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("applyTrayUpdate:"), number, false)
	pool.Send(sel("drain"))
	return macUpdates.wait(token, r)
}

func applyMacUpdate(source *availabilitySource, c Changes) error {
	mu.Lock()
	if !running || macAvailability != source {
		mu.Unlock()
		return ErrNotRunning
	}
	cfg := applyChanges(macConfig, c)
	macConfig = cfg
	item, target := trayItem, trayTarget
	mu.Unlock()
	applyButton(item.Send(sel("button")), cfg)
	if c.Items != nil {
		var clicks []clickEntry
		base := nextClickTag
		menu := objc.ID(0)
		if len(cfg.Items) > 0 {
			menu = buildMenu(cfg.Items, &clicks, target)
		}
		item.Send(sel("setMenu:"), menu)
		if menu != 0 {
			menu.Send(sel("release"))
		}
		mu.Lock()
		activeClicks = clicks
		activeClickBase = base
		mu.Unlock()
	}
	return nil
}

// applyButton sets the status item's icon, title, and tooltip, guaranteeing a
// visible button: with neither icon nor title it falls back to a bullet
// glyph. On macOS the template icon (monochrome, theme-adaptive) takes
// precedence over the plain icon.
func applyButton(button objc.ID, cfg Config) {
	if button == 0 {
		return
	}
	var png []byte
	template := false
	switch {
	case len(cfg.TemplateIcon) > 0:
		png, template = cfg.TemplateIcon, true
	case len(cfg.Icon) > 0:
		png = cfg.Icon
	}
	if len(png) > 0 {
		img := imageFromPNG(png, statusIconSize, template)
		if img != 0 {
			button.Send(sel("setImage:"), img)
			img.Send(sel("release")) // the button retains it
		}
	} else {
		button.Send(sel("setImage:"), objc.ID(0))
	}
	switch {
	case cfg.Title != "":
		button.Send(sel("setTitle:"), nsstr(cfg.Title))
	case len(cfg.Icon) == 0 && len(cfg.TemplateIcon) == 0:
		button.Send(sel("setTitle:"), nsstr("●")) // an icon-less tray stays clickable
	default:
		button.Send(sel("setTitle:"), nsstr(""))
	}
	button.Send(sel("setToolTip:"), nsstr(cfg.Tooltip))
}

// buildMenu builds an NSMenu whose clickable items target the shared target
// instance and carry their flat index as tag; every clickable item is also
// appended to clicks. autoenablesItems is off so Disabled is honored and
// enabled items stay clickable without a validateMenuItem: implementation.
func buildMenu(items []Item, clicks *[]clickEntry, target objc.ID) objc.ID {
	menu := class("NSMenu").Send(sel("alloc")).Send(sel("init"))
	menu.Send(sel("setAutoenablesItems:"), false)
	for _, it := range items {
		if it.Separator {
			menu.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
			continue
		}
		if it.Submenu != nil {
			// Submenu container: recurse and attach the child menu.
			mi := class("NSMenuItem").Send(sel("alloc")).Send(
				sel("initWithTitle:action:keyEquivalent:"), nsstr(it.Label), objc.SEL(0), nsstr(""))
			if sub := buildMenu(it.Submenu, clicks, target); sub != 0 {
				mi.Send(sel("setSubmenu:"), sub)
				sub.Send(sel("release"))
			}
			if it.Disabled {
				mi.Send(sel("setEnabled:"), false)
			}
			menu.Send(sel("addItem:"), mi)
			mi.Send(sel("release")) // the menu retains it
			continue
		}
		// Plain or checkbox item: its tag is the next flat index.
		tag := nextClickTag
		nextClickTag++
		*clicks = append(*clicks, clickEntry{item: it, onClick: it.OnClick})
		mi := class("NSMenuItem").Send(sel("alloc")).Send(
			sel("initWithTitle:action:keyEquivalent:"), nsstr(it.Label), sel("menuItemClicked:"), nsstr(""))
		mi.Send(sel("setTarget:"), target)
		mi.Send(sel("setTag:"), tag)
		if it.Checked {
			mi.Send(sel("setState:"), 1)
		}
		if it.Disabled {
			mi.Send(sel("setEnabled:"), false)
		}
		if len(it.Icon) > 0 {
			if img := imageFromPNG(it.Icon, 16, false); img != 0 {
				mi.Send(sel("setImage:"), img)
				img.Send(sel("release")) // the menu retains it
			}
		}
		menu.Send(sel("addItem:"), mi)
		mi.Send(sel("release")) // the menu retains it
	}
	return menu
}

// imageFromPNG decodes a PNG into an NSImage sized to side points, optionally
// marked as a template image. The caller owns the returned object; release it
// after the view that retains it has taken it.
func imageFromPNG(png []byte, side float64, template bool) objc.ID {
	data := class("NSData").Send(sel("dataWithBytes:length:"),
		unsafe.Pointer(&png[0]), uint(len(png))) // #nosec G103 -- NSData copies synchronously
	img := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
	if img == 0 {
		return 0
	}
	img.Send(sel("setSize:"), nsSize{side, side})
	if template {
		img.Send(sel("setTemplate:"), true)
	}
	return img
}

// stopRunLoop stops NSApplication and posts a dummy event so the run loop
// wakes up and returns even when idle. Must run on the main thread.
func stopRunLoop() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("stop:"), objc.ID(0))
	event := class("NSEvent").Send(
		sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(sel("postEvent:atStart:"), event, true)
}

// stop ends a tray started by Run: when the tray owns the loop it wakes the
// loop (Run then cleans up), and when the tray was started by Set it hides the
// tray outright. Safe to call from any goroutine.
func stop() {
	mu.Lock()
	t := trayTarget
	r := running
	if r && t != 0 {
		t.Send(sel("retain"))
	}
	mu.Unlock()
	if !r || t == 0 {
		return
	}
	defer t.Send(sel("release"))
	// Route the AppKit teardown onto the main thread; Stop may be called from a
	// menu callback (already main) or any other goroutine. Async: Run's loop
	// wakes, runs the teardown, and returns.
	t.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("trayStop"), objc.ID(0), false)
}

// remove hides a tray started by Set, leaving the host's run loop running.
// Safe from any goroutine; the teardown is routed to the main thread
// synchronously so the icon is gone by the time Remove returns.
func remove() {
	mu.Lock()
	t := trayTarget
	r := running
	if r && t != 0 {
		t.Send(sel("retain"))
	}
	mu.Unlock()
	if !r || t == 0 {
		return
	}
	defer t.Send(sel("release"))
	t.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("trayStop"), objc.ID(0), true)
}

// bounds returns zeros: NSStatusItem does not expose its on-screen rectangle.
func bounds() (x, y, w, h int) { return 0, 0, 0, 0 }
