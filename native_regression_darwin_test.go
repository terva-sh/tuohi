package tuohi

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

var resMenuSupplement, resFullscreenHide atomic.Value

// These scenarios run on TestMain's AppKit thread. They use disposable windows,
// synthetic text and native menu dispatch, never an installed consumer app.
func menuSupplementScenario() string {
	var quit objc.ID
	var dispatched bool
	app := &App{Name: "Tuohi menu acceptance", Start: func() error {
		dispatchMain(func() {
			nsapp := class("NSApplication").Send(sel("sharedApplication"))
			dispatched = objc.Send[bool](nsapp, sel("sendAction:to:from:"), sel("tuohiQuit:"), quit.Send(sel("target")), quit)
		})
		return nil
	}}
	v := &View{Frame: true, Width: 400, Height: 250}
	if err := app.Show(v); err != nil {
		return err.Error()
	}
	defer v.Close()
	nsapp := class("NSApplication").Send(sel("sharedApplication"))
	original := nsapp.Send(sel("mainMenu")).Send(sel("retain"))
	defer func() {
		nsapp.Send(sel("setMainMenu:"), original)
		original.Send(sel("release"))
	}()
	var failure string
	autorelease(func() {
		for _, shape := range []string{"absent", "empty", "placeholder", "populated", "existing-edit", "existing-actions", "custom-keys", "actionable-leaves"} {
			bar := newMenu("")
			var host, hostItem, copyItem objc.ID
			if shape != "absent" && shape != "empty" {
				host = addSubmenu(bar, "Synthetic host")
				hostItem = bar.Send(sel("itemAtIndex:"), 0)
			}
			var leaves []objc.ID
			if shape == "actionable-leaves" {
				for _, title := range []string{"Edit", "Window"} {
					addItem(bar, title, "hide:", "j", nsEventModifierFlagOption, nsapp)
					leaves = append(leaves, bar.Send(sel("itemAtIndex:"), objc.Send[int](bar, sel("numberOfItems"))-1))
				}
			}
			if shape == "populated" {
				addItem(host, "Host action", "hide:", "j", nsEventModifierFlagOption, nsapp)
			}
			if shape == "existing-edit" || shape == "existing-actions" {
				// Actions need not live in an English-titled Edit menu. Preserve
				// custom host targets and keys, and find actions in nested menus.
				nested := addSubmenu(host, "Host editing")
				addItem(nested, "Host Copy", "copy:", "k", nsEventModifierFlagOption, nsapp)
				copyItem = nested.Send(sel("itemAtIndex:"), 0)
			}
			if shape == "custom-keys" {
				addItem(host, "Host Quit", "hostQuit:", "q", 0, nsapp)
				addItem(host, "Host Close", "hostClose:", "w", 0, nsapp)
				addItem(host, "Host Copy", "hostCopy:", "c", 0, nsapp)
			}
			if shape == "existing-actions" {
				addItem(host, "Host Quit", "terminate:", "j", nsEventModifierFlagOption, nsapp)
				addItem(host, "Host Close", "performClose:", "l", nsEventModifierFlagOption, nsapp)
			}
			if shape == "absent" {
				nsapp.Send(sel("setMainMenu:"), objc.ID(0))
			} else {
				nsapp.Send(sel("setMainMenu:"), bar)
			}
			installMainMenu()
			actual := nsapp.Send(sel("mainMenu"))
			count := countMenuItems(actual)
			installMainMenu()
			if countMenuItems(actual) != count || shape != "absent" && actual != bar || hostItem != 0 && actual.Send(sel("itemAtIndex:"), 0) != hostItem {
				failure = shape + ": replaced or duplicated host menus"
				return
			}
			for _, leaf := range leaves {
				if leaf.Send(sel("submenu")) != 0 || objc.Send[objc.SEL](leaf, sel("action")) != sel("hide:") || leaf.Send(sel("target")) != nsapp || cstr(leaf.Send(sel("keyEquivalent")).Send(sel("UTF8String"))) != "j" || objc.Send[uint](leaf, sel("keyEquivalentModifierMask")) != nsEventModifierFlagOption {
					failure = "actionable host leaf changed into a submenu"
					return
				}
			}
			for _, action := range []string{"undo:", "redo:", "cut:", "copy:", "paste:", "selectAll:"} {
				items := menuActionItems(actual, action)
				if shape == "custom-keys" && action == "copy:" {
					if len(items) != 0 || len(menuActionItems(actual, "hostCopy:")) != 1 {
						failure = "shadowed host Copy shortcut"
						return
					}
					continue
				}
				if len(items) != 1 {
					failure = shape + ": missing/duplicate " + action
					return
				}
				item := items[0]
				if action == "copy:" && copyItem != 0 {
					if item != copyItem || item.Send(sel("target")) != nsapp || objc.Send[uint](item, sel("keyEquivalentModifierMask")) != nsEventModifierFlagOption || cstr(item.Send(sel("keyEquivalent")).Send(sel("UTF8String"))) != "k" {
						failure = shape + ": changed host Copy"
						return
					}
				} else if item.Send(sel("target")) != 0 {
					failure = shape + ": editing bypassed responder chain"
					return
				}
			}
			want := 1
			if shape == "existing-actions" || shape == "custom-keys" {
				want = 0
			}
			if len(menuActionItems(actual, "tuohiQuit:")) != want || len(menuActionItems(actual, "tuohiClose:")) != want {
				failure = shape + ": missing or overridden lifecycle action"
				return
			}
			if shape == "existing-actions" && (len(menuActionItems(actual, "terminate:")) != 1 || len(menuActionItems(actual, "performClose:")) != 1) {
				failure = shape + ": lost host lifecycle action"
				return
			}
		}
	})
	if failure != "" {
		return failure
	}
	// Verify CloseRequested for both native framed close and frameless menu
	// close. Cancel+Hide retains the renderer, independent of any tray policy.
	for _, frame := range []bool{true, false} {
		calls := 0
		w := &View{Frame: frame, Width: 300, Height: 200}
		w.CloseRequested = func(r *CloseRequest) { calls++; r.Cancel(); w.Hide() }
		if err := app.Show(w); err != nil {
			return err.Error()
		}
		nw := native(w)
		window, renderer := nw.window, nw.webView
		nw.Raise()
		drainMainQueue(100 * time.Millisecond)
		nw.Raise()
		menuClose()
		if calls != 1 || w.WindowState().Closed || nw.window != window || nw.webView != renderer || w.WindowState().Visible {
			detail := fmt.Sprintf("frame=%v: menu Close bypassed policy: calls=%d state=%+v nativeVisible=%v sameWindow=%v sameRenderer=%v menuWindow=%v style=%x pending=%v", frame, calls, w.WindowState(), objc.Send[bool](window, sel("isVisible")), nw.window == window, nw.webView == renderer, menuWindow() == window, objc.Send[uint](window, sel("styleMask")), nw.hidePending)
			w.Close()
			return detail
		}
		w.Close()
	}
	// Install a placeholder, then dispatch its supplemented Quit through
	// NSApplication. Wait must return, allowing the caller to join a worker.
	bar := newMenu("")
	addSubmenu(bar, "Synthetic host")
	nsapp.Send(sel("setMainMenu:"), bar)
	installMainMenu()
	quit = menuActionItems(bar, "tuohiQuit:")[0]
	stop, joined := make(chan struct{}), make(chan struct{})
	go func() { <-stop; close(joined) }()
	watchdog := time.AfterFunc(5*time.Second, app.Quit)
	err := app.Wait()
	watchdog.Stop()
	close(stop)
	<-joined
	if err != nil || !dispatched {
		return fmt.Sprintf("native Quit did not return Wait: %v", err)
	}
	return "ok"
}

func countMenuItems(menu objc.ID) int {
	count := objc.Send[int](menu, sel("numberOfItems"))
	for i := 0; i < objc.Send[int](menu, sel("numberOfItems")); i++ {
		if sub := menu.Send(sel("itemAtIndex:"), i).Send(sel("submenu")); sub != 0 {
			count += countMenuItems(sub)
		}
	}
	return count
}

func menuActionItems(menu objc.ID, action string) []objc.ID {
	var out []objc.ID
	for i := 0; i < objc.Send[int](menu, sel("numberOfItems")); i++ {
		item := menu.Send(sel("itemAtIndex:"), i)
		if objc.Send[objc.SEL](item, sel("action")) == sel(action) {
			out = append(out, item)
		}
		if sub := item.Send(sel("submenu")); sub != 0 {
			out = append(out, menuActionItems(sub, action)...)
		}
	}
	return out
}

// menuSupplementEditScenario reuses the real WKWebView editing test, starting
// with the bundled placeholder shape. Its pasteboard is cleared before the
// first read and carries only the scenario's synthetic text.
func menuSupplementEditScenario() string {
	app := class("NSApplication").Send(sel("sharedApplication"))
	original := app.Send(sel("mainMenu")).Send(sel("retain"))
	defer func() {
		app.Send(sel("setMainMenu:"), original)
		original.Send(sel("release"))
	}()
	bar := newMenu("")
	addSubmenu(bar, "Synthetic host")
	app.Send(sel("setMainMenu:"), bar)
	return menuEditScenario(false)
}

func TestNativeMenuSupplement(t *testing.T) {
	got, _ := resMenuSupplement.Load().(string)
	requireGUI(t, got)
	if got != "ok" {
		t.Fatal(got)
	}
}

func TestNativeFullscreenHide(t *testing.T) {
	got, _ := resFullscreenHide.Load().(string)
	requireGUI(t, got)
	if got != "ok" {
		t.Fatal(got)
	}
}

func fullscreenHideScenario() string {
	watchdog := time.AfterFunc(75*time.Second, func() { panic("fullscreen acceptance stalled") })
	defer watchdog.Stop()
	app := &App{Name: "Tuohi fullscreen acceptance"}
	v := &View{Frame: true, Width: 600, Height: 400, URL: "data:text/html,<input value='synthetic retained text'>"}
	var nw *webview
	premature := false
	v.WindowChanged = func(s WindowState) {
		if nw != nil && !s.Closed && !s.Visible && objc.Send[uint](nw.window, sel("styleMask"))&(1<<14) != 0 {
			premature = true
		}
	}
	v.CloseRequested = func(r *CloseRequest) { r.Cancel(); v.Hide() }
	if err := app.Show(v); err != nil {
		return err.Error()
	}
	defer v.Close()
	nw = native(v)
	window, renderer := nw.window, nw.webView
	entered, exited, exitStarted := 0, 0, 0
	forwardEntry := false
	var lateEntryAction func()
	observerClass, err := objc.RegisterClass("TuohiFullscreenAcceptanceObserver", objc.GetClass("NSObject"), nil, nil,
		[]objc.MethodDef{{Cmd: sel("observe:"), Fn: func(_ objc.ID, _ objc.SEL, notification objc.ID) {
			switch cstr(notification.Send(sel("name")).Send(sel("UTF8String"))) {
			case "NSWindowDidEnterFullScreenNotification":
				entered++
				if forwardEntry {
					// NSNotificationCenter promises no observer order. For the
					// late-host cases, forward the real AppKit notification to
					// tuohi first, then invoke the host while delivery is active.
					fullscreenWindowNotification(nw.fullscreenObserver, 0, notification)
				}
				if lateEntryAction != nil {
					lateEntryAction()
				}
			case "NSWindowWillExitFullScreenNotification":
				exitStarted++
			case "NSWindowDidExitFullScreenNotification":
				exited++
			}
		}}})
	if err != nil {
		return err.Error()
	}
	observer := objc.ID(observerClass).Send(sel("new"))
	center := class("NSNotificationCenter").Send(sel("defaultCenter"))
	defer func() { center.Send(sel("removeObserver:"), observer); observer.Send(sel("release")) }()
	for _, name := range []string{"NSWindowDidEnterFullScreenNotification", "NSWindowWillExitFullScreenNotification", "NSWindowDidExitFullScreenNotification"} {
		center.Send(sel("addObserver:selector:name:object:"), observer, sel("observe:"), nsstr(name), window)
	}
	wait := func(predicate func() bool) bool {
		for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
			if predicate() {
				return true
			}
			drainMainQueue(20 * time.Millisecond)
		}
		return false
	}
	visible := func() bool { return objc.Send[bool](window, sel("isVisible")) }
	enter := func(n int) bool {
		window.Send(sel("toggleFullScreen:"), objc.ID(0))
		return wait(func() bool { return entered == n })
	}
	closeWindow := func() { window.Send(sel("performClose:"), objc.ID(0)) }
	if !enter(1) {
		return "fullscreen entry timed out"
	}
	closeWindow()
	if !wait(func() bool { return exited == 1 && !visible() }) || premature {
		return "hide did not wait for completed fullscreen exit"
	}
	v.Show()
	if !visible() || nw.window != window || nw.webView != renderer || v.WindowState().Closed {
		return "reopen lost retained window/renderer"
	}
	if !enter(2) {
		return "second entry timed out"
	}
	closeWindow()
	if !wait(func() bool { return exitStarted == 2 }) {
		return "second exit never started"
	}
	// Custom chrome can still issue close requests when AppKit has disabled
	// its close button. Neither repetition may toggle back into fullscreen.
	v.RequestClose()
	v.RequestClose()
	if !wait(func() bool { return exited == 2 && !visible() }) || entered != 2 || premature {
		return "repeated close during exit did not coalesce"
	}
	v.Show()
	window.Send(sel("toggleFullScreen:"), objc.ID(0))
	v.RequestClose() // Close while entering; DidEnter must schedule the exit.
	if !wait(func() bool { return entered == 3 && exited == 3 && !visible() }) || premature {
		return fmt.Sprintf("entry-close failed: enter=%d exit=%d started=%d premature=%v", entered, exited, exitStarted, premature)
	}
	v.Show()
	if !enter(4) {
		return "reopen-race entry timed out"
	}
	closeWindow()
	if !wait(func() bool { return exitStarted == 4 }) {
		return "reopen-race exit never started"
	}
	v.Show() // Also the consumer's host-loss recovery path.
	if !wait(func() bool { return exited == 4 }) {
		return "reopen-race exit timed out"
	}
	drainMainQueue(200 * time.Millisecond)
	if !visible() || nw.hidePending || nw.window != window || nw.webView != renderer {
		return "late transition hid a reopened window"
	}
	// Reveal during entry must also invalidate the queued DidEnter exit.
	window.Send(sel("toggleFullScreen:"), objc.ID(0))
	v.RequestClose()
	v.Focus(true) // Raise is another explicit reveal.
	if !wait(func() bool { return entered == 5 }) {
		return "entry reveal timed out"
	}
	drainMainQueue(200 * time.Millisecond)
	if exitStarted != 4 || !visible() || nw.hidePending {
		return "entry reveal failed to cancel deferred exit"
	}
	closeWindow()
	if !wait(func() bool { return exited == 5 && !visible() }) {
		return "exit after entry reveal timed out"
	}
	v.Show()
	// Deliver DidEnter to tuohi before the synthetic host observer, while
	// still in the real AppKit notification. No Hide is pending beforehand.
	center.Send(sel("removeObserver:name:object:"), nw.fullscreenObserver, nsstr("NSWindowDidEnterFullScreenNotification"), window)
	forwardEntry = true
	for i, action := range []func(){v.Hide, v.RequestClose} {
		lateEntryAction = action
		if !enter(6+i) || !wait(func() bool { return exited == 6+i && !visible() }) || premature {
			return fmt.Sprintf("late DidEnter host action %d failed: enter=%d exit=%d pending=%v", i, entered, exited, nw.hidePending)
		}
		lateEntryAction = nil
		v.Show()
	}
	if !enter(8) {
		return "quit-race entry timed out"
	}
	closeWindow()
	if !wait(func() bool { return exitStarted == 8 }) {
		return "quit-race exit never started"
	}
	app.Quit()
	if err := app.Wait(); err != nil { // No teardown wait may depend on the animation.
		return err.Error()
	}
	if !wait(func() bool { return exited == 8 }) || !visible() || nw.hidePending || premature {
		return "quit did not invalidate pending hide"
	}
	// Closing while an exit is pending removes the observer and queued work.
	otherApp := &App{Name: "Tuohi close acceptance"}
	other := &View{Frame: true, Width: 400, Height: 300}
	if err := otherApp.Show(other); err != nil {
		return err.Error()
	}
	ow := native(other)
	otherWindow := ow.window
	otherWindow.Send(sel("toggleFullScreen:"), objc.ID(0))
	if !wait(func() bool {
		return !ow.fullscreenEntering && objc.Send[uint](otherWindow, sel("styleMask"))&(1<<14) != 0
	}) {
		other.Close()
		return "close-race entry timed out"
	}
	other.Hide()
	other.Close()
	drainMainQueue(200 * time.Millisecond)
	if ow.window != 0 || ow.fullscreenObserver != 0 || ow.hidePending {
		return "close left a pending hide or fullscreen observer"
	}
	return "ok"
}
