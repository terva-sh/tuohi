//go:build darwin

package tuohi

import (
	"strings"

	"github.com/ebitengine/purego/objc"
)

// The default main menu (TKT-01M3N2Q03HV78EKDSH9E4CG0EY). AppKit routes a
// key equivalent such as ⌘W or ⌘C through the main menu before any window
// sees it, so an app with no main menu has no ⌘W, ⌘Q, ⌘M or ⌘H, and a page's
// text fields have no ⌘C, ⌘V, ⌘X, ⌘A or ⌘Z. tuohi installs a fixed menu when
// it creates the first window: the application menu, Edit, and Window. A
// consumer cannot replace or extend it yet; a menu API waits until one asks.
// An existing host menu is supplemented only with missing Edit, Quit and
// Close actions; its objects, targets and key equivalents remain intact.

// Modifier flags for setKeyEquivalentModifierMask:.
const (
	nsEventModifierFlagOption  = 1 << 19
	nsEventModifierFlagCommand = 1 << 20
)

// menuTargetClass answers the menu items whose behaviour is tuohi's: Quit,
// Close and Minimize. The Edit items and Hide go to the first responder and
// to NSApp, which implement them.
var (
	menuTargetClass objc.Class
	menuTarget      objc.ID
)

func registerMenuTargetClass() error {
	var err error
	menuTargetClass, err = objc.RegisterClass(
		"TuohiMenuTarget", objc.GetClass("NSObject"), nil, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("tuohiQuit:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) { menuQuit() },
			},
			{
				Cmd: sel("tuohiClose:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) { menuClose() },
			},
			{
				Cmd: sel("tuohiMinimize:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) { menuMinimize() },
			},
		})
	return err
}

// menuQuit is ⌘Q: App.Quit, so App.Wait returns and the program ends the way
// it chose to. Without an App scope there is no Wait to return, and AppKit
// terminates the process instead.
func menuQuit() {
	if s := scopePtr.Load(); s != nil {
		s.requestExit()
		return
	}
	class("NSApplication").Send(sel("sharedApplication")).Send(sel("terminate:"), objc.ID(0))
}

// menuClose is ⌘W on the key window. A window with a close button is closed
// as its button would close it. A frameless window has none, so performClose:
// would only beep; it is sent close, which its delegate reports the same way.
func menuClose() {
	win := menuWindow()
	if win == 0 {
		return
	}
	if objc.Send[uint](win, sel("styleMask"))&nsWindowStyleMaskClosable != 0 {
		win.Send(sel("performClose:"), objc.ID(0))
		return
	}
	if w := lookupEngine(win.Send(sel("delegate"))); w != nil && !w.lifecycle.request() {
		return
	}
	win.Send(sel("close"))
}

// menuMinimize is ⌘M on the key window. A frameless tuohi window cannot be
// miniaturized by AppKit, so it uses the engine's own Minimize.
func menuMinimize() {
	win := menuWindow()
	if win == 0 {
		return
	}
	if w := lookupEngine(win.Send(sel("delegate"))); w != nil && w.frameless {
		w.Minimize()
		return
	}
	win.Send(sel("performMiniaturize:"), objc.ID(0))
}

// menuWindow is the window Close and Minimize act on: the key window, or the
// main window when no window is key, as a menu item a person chooses with
// the mouse may find it.
func menuWindow() objc.ID {
	app := class("NSApplication").Send(sel("sharedApplication"))
	if win := app.Send(sel("keyWindow")); win != 0 {
		return win
	}
	return app.Send(sel("mainWindow"))
}

// installMainMenu installs or supplements the main menu on the main thread,
// when a window is created. Bundled launches can supply a placeholder menu.
func installMainMenu() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	if bar := app.Send(sel("mainMenu")); bar != 0 {
		autorelease(func() { supplementMainMenu(bar) })
		return
	}
	if menuTarget == 0 {
		menuTarget = objc.ID(menuTargetClass).Send(sel("new"))
	}
	name := menuAppName()
	autorelease(func() {
		bar := newMenu("")
		appMenu := addSubmenu(bar, name)
		addItem(appMenu, "About "+name, "orderFrontStandardAboutPanel:", "", 0, 0)
		appMenu.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
		addItem(appMenu, "Hide "+name, "hide:", "h", 0, 0)
		addItem(appMenu, "Hide Others", "hideOtherApplications:", "h", nsEventModifierFlagOption|nsEventModifierFlagCommand, 0)
		addItem(appMenu, "Show All", "unhideAllApplications:", "", 0, 0)
		appMenu.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
		addItem(appMenu, "Quit "+name, "tuohiQuit:", "q", 0, menuTarget)

		edit := addSubmenu(bar, "Edit")
		addItem(edit, "Undo", "undo:", "z", 0, 0)
		addItem(edit, "Redo", "redo:", "Z", 0, 0)
		edit.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
		addItem(edit, "Cut", "cut:", "x", 0, 0)
		addItem(edit, "Copy", "copy:", "c", 0, 0)
		addItem(edit, "Paste", "paste:", "v", 0, 0)
		addItem(edit, "Select All", "selectAll:", "a", 0, 0)

		window := addSubmenu(bar, "Window")
		addItem(window, "Minimize", "tuohiMinimize:", "m", 0, menuTarget)
		addItem(window, "Close", "tuohiClose:", "w", 0, menuTarget)

		app.Send(sel("setMainMenu:"), bar)
		app.Send(sel("setWindowsMenu:"), window)
	})
}

// menuAppName is the name the application menu shows: App.Name, or the
// process name when the App has none.
func menuAppName() string {
	if s := scopePtr.Load(); s != nil && s.cfg.Name != "" {
		return s.cfg.Name
	}
	info := class("NSProcessInfo").Send(sel("processInfo"))
	return cstr(info.Send(sel("processName")).Send(sel("UTF8String")))
}

// newMenu returns an autoreleased NSMenu with the given title.
func newMenu(title string) objc.ID {
	return class("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsstr(title)).Send(sel("autorelease"))
}

// addSubmenu adds an item titled title to bar with a new menu of the same
// title under it, and returns that menu.
func addSubmenu(bar objc.ID, title string) objc.ID {
	sub := newMenu(title)
	item := class("NSMenuItem").Send(sel("alloc")).Send(
		sel("initWithTitle:action:keyEquivalent:"), nsstr(title), objc.SEL(0), nsstr("")).Send(sel("autorelease"))
	item.Send(sel("setSubmenu:"), sub)
	bar.Send(sel("addItem:"), item)
	return sub
}

// menuItemTag marks the items tuohi adds, so they can be told from those
// AppKit adds to an Edit or Window menu by itself.
const menuItemTag = 0x7475 // "tu"

// addItem adds an item to menu. A key equivalent's modifiers default to ⌘,
// and an upper-case key adds ⇧. A zero target sends the action to the first
// responder.
func addItem(menu objc.ID, title, action, key string, mods uint, target objc.ID) {
	item := class("NSMenuItem").Send(sel("alloc")).Send(
		sel("initWithTitle:action:keyEquivalent:"), nsstr(title), sel(action), nsstr(key)).Send(sel("autorelease"))
	item.Send(sel("setTag:"), menuItemTag)
	if mods != 0 {
		item.Send(sel("setKeyEquivalentModifierMask:"), mods)
	}
	if target != 0 {
		item.Send(sel("setTarget:"), target)
	}
	menu.Send(sel("addItem:"), item)
}

// supplementMainMenu preserves host actions wherever they live in the menu
// tree. A host's terminate: remains its own policy; only a missing Quit is
// routed through tuohi so App.Wait can return for joined Go teardown.
func supplementMainMenu(bar objc.ID) {
	if menuTarget == 0 {
		menuTarget = objc.ID(menuTargetClass).Send(sel("new"))
	}
	if !menuHasCommand(bar, "q", "tuohiQuit:", "terminate:") {
		var application objc.ID
		if objc.Send[int](bar, sel("numberOfItems")) > 0 {
			application = bar.Send(sel("itemAtIndex:"), 0).Send(sel("submenu"))
		}
		if application == 0 {
			application = newMenu(menuAppName())
			item := class("NSMenuItem").Send(sel("alloc")).Send(
				sel("initWithTitle:action:keyEquivalent:"), nsstr(menuAppName()), objc.SEL(0), nsstr("")).Send(sel("autorelease"))
			item.Send(sel("setSubmenu:"), application)
			bar.Send(sel("insertItem:atIndex:"), item, 0)
		}
		addItem(application, "Quit "+menuAppName(), "tuohiQuit:", "q", 0, menuTarget)
	}
	for _, command := range []struct{ title, action, key string }{
		{"Undo", "undo:", "z"}, {"Redo", "redo:", "Z"},
		{"Cut", "cut:", "x"}, {"Copy", "copy:", "c"},
		{"Paste", "paste:", "v"}, {"Select All", "selectAll:", "a"},
	} {
		if !menuHasCommand(bar, command.key, command.action) {
			addItem(ensureSubmenu(bar, "Edit"), command.title, command.action, command.key, 0, 0)
		}
	}
	if !menuHasCommand(bar, "w", "tuohiClose:", "performClose:") {
		addItem(ensureSubmenu(bar, "Window"), "Close", "tuohiClose:", "w", 0, menuTarget)
	}
}

func ensureSubmenu(bar objc.ID, title string) objc.ID {
	for i := 0; i < objc.Send[int](bar, sel("numberOfItems")); i++ {
		item := bar.Send(sel("itemAtIndex:"), i)
		if !objc.Send[bool](item.Send(sel("title")), sel("isEqualToString:"), nsstr(title)) {
			continue
		}
		if sub := item.Send(sel("submenu")); sub != 0 {
			return sub
		}
		sub := newMenu(title)
		item.Send(sel("setSubmenu:"), sub)
		return sub
	}
	return addSubmenu(bar, title)
}

func menuHasAction(menu objc.ID, actions ...string) bool {
	for i := 0; i < objc.Send[int](menu, sel("numberOfItems")); i++ {
		item := menu.Send(sel("itemAtIndex:"), i)
		for _, action := range actions {
			if objc.Send[objc.SEL](item, sel("action")) == sel(action) {
				return true
			}
		}
		if sub := item.Send(sel("submenu")); sub != 0 && menuHasAction(sub, actions...) {
			return true
		}
	}
	return false
}

// A host may use its own selector for a standard key. Do not shadow that
// shortcut merely because its action has a different name.
func menuHasCommand(menu objc.ID, key string, actions ...string) bool {
	if menuHasAction(menu, actions...) {
		return true
	}
	return menuHasKey(menu, key)
}

func menuHasKey(menu objc.ID, key string) bool {
	wantMods := uint(nsEventModifierFlagCommand)
	if key != strings.ToLower(key) {
		wantMods |= 1 << 17 // NSEventModifierFlagShift
	}
	for i := 0; i < objc.Send[int](menu, sel("numberOfItems")); i++ {
		item := menu.Send(sel("itemAtIndex:"), i)
		actual := cstr(item.Send(sel("keyEquivalent")).Send(sel("UTF8String")))
		mods := objc.Send[uint](item, sel("keyEquivalentModifierMask"))
		if actual != strings.ToLower(actual) {
			mods |= 1 << 17
		}
		if actual != "" && strings.EqualFold(actual, key) && mods == wantMods {
			return true
		}
		if sub := item.Send(sel("submenu")); sub != 0 && menuHasKey(sub, key) {
			return true
		}
	}
	return false
}
