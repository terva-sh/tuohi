// macOS notification backend: posts an NSUserNotification through the shared
// NSUserNotificationCenter, using purego's Objective-C runtime bindings (no
// cgo). The API is deprecated since macOS 10.14 but still functional, and it
// delivers without an app bundle, a window, or a running run loop.
//
// Options: Options.Icon is loaded with [NSImage initWithContentsOfFile:] (any
// format NSImage reads - PNG, JPEG, TIFF, ...) and Options.IconData with
// [NSImage initWithData:]; the image is attached as the notification's
// contentImage (macOS 10.9+; older releases ignore it). A notification with
// UrgencyCritical gets the default sound name attached, which is what makes
// Alert audible - the system plays the notification sound itself. macOS
// derives the source identity from the OS, so the Show name is ignored.
//
// beep plays the system beep through osascript and falls back to the
// terminal bell character when osascript is not installed. Frequency and
// duration are ignored: macOS has no tone API, only the fixed system beep.

package notify

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
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
		panic(fmt.Sprintf("notify: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				initErr = fmt.Errorf("notify: load %s: %w", fw, err)
				return
			}
		}
	})
	return initErr
}

// show builds an NSUserNotification (title + informativeText, optional
// content image and default sound for UrgencyCritical) and hands it to the
// default NSUserNotificationCenter. Safe from any goroutine. The name is
// ignored: macOS derives the source identity from the OS.
func show(name, title, message string, opts Options) error {
	if err := ensureInit(); err != nil {
		return err
	}

	notification := class("NSUserNotification").Send(sel("alloc")).Send(sel("init"))
	if notification == 0 {
		return fmt.Errorf("notify: failed to create NSUserNotification")
	}
	if title != "" {
		notification.Send(sel("setTitle:"), nsstr(title))
	}
	if message != "" {
		notification.Send(sel("setInformativeText:"), nsstr(message))
	}
	if opts.Urgency.level() == 2 {
		// NSUserNotificationDefaultSoundName == "default"; the system plays
		// this sound when the notification is presented.
		notification.Send(sel("setSoundName:"), nsstr("default"))
	}

	// Load the content image, if any. The notification retains it through
	// setContentImage:, so our release after delivery cannot free it early.
	var image objc.ID
	switch {
	case opts.Icon != "":
		image = class("NSImage").Send(sel("alloc")).Send(sel("initWithContentsOfFile:"), nsstr(opts.Icon))
		if image == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: cannot load Options.Icon file %q (macOS needs an image file path, not an icon name)", opts.Icon)
		}
	case len(opts.IconData) > 0:
		data := class("NSData").Send(sel("alloc")).Send(sel("initWithBytes:length:"), unsafe.Pointer(&opts.IconData[0]), len(opts.IconData))
		if data == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: failed to create NSData for Options.IconData")
		}
		image = class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
		data.Send(sel("release"))
		if image == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: Options.IconData is not a decodable image")
		}
	}
	if image != 0 {
		notification.Send(sel("setContentImage:"), image)
	}

	center := class("NSUserNotificationCenter").Send(sel("defaultUserNotificationCenter"))
	if center == 0 {
		if image != 0 {
			image.Send(sel("release"))
		}
		notification.Send(sel("release"))
		// macOS only hands a notification center to a process that has an app
		// identity - a bundled .app the user granted Notification permission.
		// For anything else (an unbundled ./demo, a bare binary) the legacy
		// center is nil here, and the modern UNUserNotificationCenter crashes
		// without a bundle. Report it clearly and never crash.
		return fmt.Errorf("%w (NSUserNotificationCenter is nil for this app: run as a bundled .app and grant Notification access)", ErrUnavailable)
	}
	center.Send(sel("deliverNotification:"), notification)
	if image != 0 {
		image.Send(sel("release"))
	}
	notification.Send(sel("release")) // the center keeps what it needs
	return nil
}

// beep plays the system beep via osascript, falling back to the terminal
// bell character when osascript is missing. Frequency and duration are
// ignored on macOS, which offers no tone API.
func beep(freq float64, duration int) error {
	osa, err := exec.LookPath("osascript")
	if err != nil {
		// Output the only beep we can.
		if _, err := os.Stdout.Write([]byte{7}); err != nil {
			return fmt.Errorf("notify: beep: write bell to stdout: %w", err)
		}
		return nil
	}
	return exec.Command(osa, "-e", "beep").Run()
}

// alertSound backs Alert: the critical notification already carries the
// default sound name, so no separate beep is needed on macOS.
func alertSound() error {
	return nil
}
