package tuohi

import "github.com/ebitengine/purego/objc"

// Fullscreen notifications are observed separately from NSWindowDelegate:
// an embedded host keeps its own delegate. All fields run on AppKit's thread.
var fullscreenObserverClass objc.Class

func registerFullscreenObserverClass() error {
	var err error
	fullscreenObserverClass, err = objc.RegisterClass("TuohiFullscreenObserver", objc.GetClass("NSObject"), nil, nil,
		[]objc.MethodDef{{Cmd: sel("windowNotification:"), Fn: fullscreenWindowNotification}})
	return err
}

func (w *webview) observeFullscreen() {
	w.fullscreenObserver = objc.ID(fullscreenObserverClass).Send(sel("new"))
	registerEngine(w.fullscreenObserver, w)
	center := class("NSNotificationCenter").Send(sel("defaultCenter"))
	for _, name := range []string{"NSWindowWillEnterFullScreenNotification", "NSWindowDidEnterFullScreenNotification", "NSWindowWillExitFullScreenNotification", "NSWindowDidExitFullScreenNotification", "NSWindowWillCloseNotification"} {
		center.Send(sel("addObserver:selector:name:object:"), w.fullscreenObserver, sel("windowNotification:"), nsstr(name), w.window)
	}
}

func (w *webview) stopObservingFullscreen() {
	w.cancelPendingHide()
	w.fullscreenTransition++
	if w.fullscreenObserver == 0 {
		return
	}
	class("NSNotificationCenter").Send(sel("defaultCenter")).Send(sel("removeObserver:"), w.fullscreenObserver)
	unregisterEngine(w.fullscreenObserver)
	w.fullscreenObserver.Send(sel("release"))
	w.fullscreenObserver = 0
}

func (w *webview) cancelPendingHide() {
	w.hidePending = false
}

func (w *webview) hasPendingHide() bool {
	// App.Quit invalidates lifecycle requests immediately, even off the UI
	// thread. A late animation callback must not hide during joined teardown.
	w.lifecycle.mu.Lock()
	quitting := w.lifecycle.quitting
	w.lifecycle.mu.Unlock()
	if quitting || w.window == 0 {
		w.cancelPendingHide()
	}
	return w.hidePending
}

func (w *webview) exitFullscreenToHide() {
	if !w.hasPendingHide() || w.fullscreenEntering || w.fullscreenExiting {
		return
	}
	if objc.Send[uint](w.window, sel("styleMask"))&(1<<14) == 0 { // NSWindowStyleMaskFullScreen
		w.hidePending = false
		w.window.Send(sel("orderOut:"), objc.ID(0))
		w.lifecycle.update(w.windowState())
		return
	}
	// Coalesce repeated Hide/close requests even before WillExit arrives.
	w.fullscreenExiting = true
	w.window.Send(sel("toggleFullScreen:"), objc.ID(0))
}

func fullscreenWindowNotification(self objc.ID, _ objc.SEL, notification objc.ID) {
	w := lookupEngine(self)
	if w == nil || notification.Send(sel("object")) != w.window {
		return
	}
	switch cstr(notification.Send(sel("name")).Send(sel("UTF8String"))) {
	case "NSWindowWillEnterFullScreenNotification":
		w.fullscreenTransition++
		w.fullscreenEntering = true
	case "NSWindowWillExitFullScreenNotification":
		w.fullscreenTransition++
		w.fullscreenEntering, w.fullscreenExiting = false, true
	case "NSWindowDidEnterFullScreenNotification":
		// Keep the entry guard through the entire notification delivery. A
		// host observer called AFTER this one can still request Hide, even
		// when no hide was pending when tuohi received DidEnter. AppKit rejects
		// a toggle until all observers return, so always settle on a later turn.
		window, transition := w.window, w.fullscreenTransition
		dispatchMain(func() {
			if w.window == window && w.fullscreenTransition == transition && w.fullscreenObserver != 0 {
				w.fullscreenEntering = false
				w.exitFullscreenToHide()
			}
		})
	case "NSWindowDidExitFullScreenNotification":
		w.fullscreenTransition++
		w.fullscreenEntering, w.fullscreenExiting = false, false
		w.exitFullscreenToHide()
	case "NSWindowWillCloseNotification":
		w.stopObservingFullscreen()
	}
}
