// macOS backends for the app-scope services: the application icon (an
// NSImage handed to AppKit, which the Dock draws) and Open/Reveal
// (NSWorkspace) - all via purego's Objective-C runtime (no cgo).
package tuohi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	iconInitOnce sync.Once
	iconInitErr  error
	// lastIcon is the most recent PNG applied via setAppIcon (App.Icon). macOS
	// only honors setApplicationIconImage: once the app has finished launching
	// (the Dock builds its process tile then), so the darwin engine re-applies
	// this in onApplicationDidFinishLaunching after setting the .regular policy.
	lastIcon []byte
)

// iconEnsureInit loads Foundation + AppKit so the objc lookups below find their
// classes. They are usually already mapped inside a GUI app, but dlopen'ing
// them makes a bare CLI binary work too.
func iconEnsureInit() error {
	iconInitOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				iconInitErr = fmt.Errorf("appkit: load %s: %w", fw, err)
				return
			}
		}
	})
	return iconInitErr
}

// setAppIcon hands the bytes to AppKit as an NSImage and makes it the
// application's icon, which is what the Dock draws.
//
// It is safe before or after a window exists: sharedApplication returns the
// one NSApplication a process may have, creating it if the program has not got
// there yet, and the icon set on it survives whoever finishes the launch -
// including a toolkit (Ebitengine, say) that goes on to build its own windows.
func setAppIcon(png []byte, _ string, _ bool) error {
	if len(png) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	err := iconEnsureInit()
	if err != nil {
		return err
	}
	var failed bool
	autorelease(func() {
		// #nosec G103 -- dataWithBytes:length: copies the buffer before it returns
		data := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&png[0]), len(png))
		image := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
		if image == 0 {
			failed = true
			return
		}
		image.Send(sel("autorelease"))
		app := class("NSApplication").Send(sel("sharedApplication"))
		app.Send(sel("setApplicationIconImage:"), image)
	})
	if failed {
		return errors.New("appkit: the application icon is not an image AppKit can read")
	}
	lastIcon = png
	return nil
}

// reapplyAppIcon re-applies the icon last given to setAppIcon (App.Icon) once
// the application has actually finished launching. macOS ignores
// setApplicationIconImage: before applicationDidFinishLaunching - the Dock
// establishes the process tile during launch and would otherwise keep the
// default - so the darwin engine calls this right after launching (and after
// setting the .regular activation policy). It is a no-op when no App.Icon was
// configured.
func reapplyAppIcon() {
	if len(lastIcon) == 0 {
		return
	}
	_ = setAppIcon(lastIcon, "", false)
}

var (
	openInitOnce sync.Once
	openInitErr  error
)

// ensureInit loads the frameworks that vend NSURL/NSArray/NSWorkspace. They
// are usually already mapped, but dlopen'ing them is cheap and makes the
// package self-sufficient when used from a bare CLI binary.
func openEnsureInit() error {
	openInitOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				openInitErr = fmt.Errorf("open: load %s: %w", fw, err)
				return
			}
		}
	})
	return openInitErr
}

// checkedClass returns the objc class with the given name, or an error when
// the loaded frameworks do not provide it. Unlike class, it reports failure
// instead of panicking.
func checkedClass(name string) (objc.ID, error) {
	c := objc.GetClass(name)
	if c == 0 {
		return 0, fmt.Errorf("open: objc class %q not found", name)
	}
	return objc.ID(c), nil
}

func openURL(rawurl string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	wsCls, err := checkedClass("NSWorkspace")
	if err != nil {
		return err
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		return err
	}
	var ok bool
	autorelease(func() {
		ws := wsCls.Send(sel("sharedWorkspace"))
		nsurl := urlCls.Send(sel("URLWithString:"), nsstr(rawurl))
		if nsurl != 0 {
			ok = ws.Send(sel("openURL:"), nsurl) != 0
		}
	})
	if !ok {
		return fmt.Errorf("open: NSWorkspace openURL: failed for %q", rawurl)
	}
	return nil
}

func revealFile(absPath string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	wsCls, err := checkedClass("NSWorkspace")
	if err != nil {
		return err
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		return err
	}
	arrCls, err := checkedClass("NSArray")
	if err != nil {
		return err
	}
	autorelease(func() {
		ws := wsCls.Send(sel("sharedWorkspace"))
		fileURL := urlCls.Send(sel("fileURLWithPath:"), nsstr(absPath))
		urls := arrCls.Send(sel("arrayWithObject:"), fileURL)
		ws.Send(sel("activateFileViewerSelectingURLs:"), urls)
	})
	return nil
}

// appExitRequested reports whether the active app scope has been asked to
// exit (App.Quit or last-window-close with App.Exit set). Only the darwin
// engine's appUIWait consults it: when an external owner (the tray package,
// say) already runs the NSApplication loop, Wait's plain loop is not what is
// dispatching events, so the queue-service branch polls this flag to learn
// when to stop.
func appExitRequested() bool {
	if s := scopePtr.Load(); s != nil {
		return atomic.LoadInt32(&s.exitFlag) != 0
	}
	return false
}
