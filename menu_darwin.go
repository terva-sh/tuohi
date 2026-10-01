//go:build darwin

package tuohi

import (
	"github.com/ebitengine/purego/objc"
)

// The default main menu (TKT-01M3N2Q03HV78EKDSH9E4CG0EY). AppKit routes a
// key equivalent such as ⌘W or ⌘C through the main menu before any window
// sees it, so an app with no main menu has no ⌘W, ⌘Q, ⌘M or ⌘H, and a page's
// text fields have no ⌘C, ⌘V, ⌘X, ⌘A or ⌘Z. tuohi installs a fixed menu when
// it creates the first window: the application menu, Edit, and Window. A
// consumer cannot replace or extend it yet; a menu API waits until one asks.
// A main menu someone else already installed, such as an embedding host's,
// is left as it is.

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

// installMainMenu installs the default main menu unless NSApp already has
// one. It runs on the main thread, when a window is created.
func installMainMenu() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	if app.Send(sel("mainMenu")) != 0 {
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

// addItem adds an item to menu. A key equivalent's modifiers default to ⌘,
// and an upper-case key adds ⇧. A zero target sends the action to the first
// responder.
func addItem(menu objc.ID, title, action, key string, mods uint, target objc.ID) {
	item := class("NSMenuItem").Send(sel("alloc")).Send(
		sel("initWithTitle:action:keyEquivalent:"), nsstr(title), sel(action), nsstr(key)).Send(sel("autorelease"))
	if mods != 0 {
		item.Send(sel("setKeyEquivalentModifierMask:"), mods)
	}
	if target != 0 {
		item.Send(sel("setTarget:"), target)
	}
	menu.Send(sel("addItem:"), item)
}
