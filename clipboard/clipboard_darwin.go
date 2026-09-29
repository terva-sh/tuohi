// The macOS clipboard: the general NSPasteboard, through purego's Objective-C
// runtime. AppKit is already in the process once a tuohi window exists, and
// the pasteboard is read and written on the main thread, through
// toolkit.Call, because AppKit does not document NSPasteboard as safe from
// other threads.
//
// The text type is AppKit's NSPasteboardTypeString, read from the loaded
// framework, rather than its value "public.utf8-plain-text" written out
// here: the constant is what AppKit documents and what other applications
// read and write, so looking it up cannot drift from it.

package clipboard

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/terva-sh/tuohi/internal/toolkit"
)

var (
	typeOnce   sync.Once
	typeString objc.ID // NSPasteboardTypeString
	typeErr    error
)

// ready returns the toolkit and the pasteboard's text type, or an error when
// no tuohi app has started AppKit.
func ready() (*toolkit.Toolkit, objc.ID, error) {
	tk := toolkit.Get()
	if tk == nil {
		return nil, 0, ErrNoApp
	}
	typeOnce.Do(func() {
		// NSPasteboardTypeString is an NSString * variable, so the symbol's
		// address is where the string's pointer is stored.
		addr, err := purego.Dlsym(purego.RTLD_DEFAULT, "NSPasteboardTypeString")
		if err != nil {
			typeErr = fmt.Errorf("clipboard: resolve NSPasteboardTypeString: %w", err)
			return
		}
		typeString = *(*objc.ID)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))) // #nosec G103 -- reads an AppKit global
		if typeString == 0 {
			typeErr = errors.New("clipboard: NSPasteboardTypeString is nil")
		}
	})
	return tk, typeString, typeErr
}

// onUI runs f on the main thread through tk.Call, inside an autorelease pool
// so the pasteboard's autoreleased objects are freed when f returns even
// before a run loop drains one. A Call that finds no loop to run f is
// ErrNoApp.
func onUI(tk *toolkit.Toolkit, f func() error) error {
	var err error
	callErr := tk.Call(func() {
		pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("alloc")).Send(sel("init"))
		defer pool.Send(sel("drain"))
		err = f()
	})
	if callErr != nil {
		return fmt.Errorf("%w: %v", ErrNoApp, callErr)
	}
	return err
}

func copyText(text string) error {
	tk, typ, err := ready()
	if err != nil {
		return err
	}
	return onUI(tk, func() error {
		pb := generalPasteboard()
		s := objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), text)
		if pb == 0 || s == 0 {
			return errors.New("clipboard: NSPasteboard is not available")
		}
		pb.Send(sel("clearContents"))
		if !objc.Send[bool](pb, sel("setString:forType:"), s, typ) {
			return errors.New("clipboard: NSPasteboard refused the text")
		}
		return nil
	})
}

func paste() (string, error) {
	tk, typ, err := ready()
	if err != nil {
		return "", err
	}
	var text string
	err = onUI(tk, func() error {
		pb := generalPasteboard()
		if pb == 0 {
			return errors.New("clipboard: NSPasteboard is not available")
		}
		// stringForType: is nil when the pasteboard holds no text.
		if s := pb.Send(sel("stringForType:"), typ); s != 0 {
			text = cstr(s.Send(sel("UTF8String")))
		}
		return nil
	})
	return text, err
}

func generalPasteboard() objc.ID {
	return objc.ID(objc.GetClass("NSPasteboard")).Send(sel("generalPasteboard"))
}

func sel(name string) objc.SEL { return objc.RegisterName(name) }

// cstr reads a Go string from a C string returned by -UTF8String, which
// stays valid while the NSString does.
func cstr(id objc.ID) string {
	if id == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103 -- C string memory, not a Go pointer
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n)) // #nosec G103 -- slice over the C string buffer
}
