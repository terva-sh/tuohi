//go:build linux || freebsd || netbsd

// The GTK clipboard (Linux, FreeBSD, NetBSD), through purego. It joins the
// GTK stack the root package loaded, through the handles internal/toolkit
// carries, and never loads a GTK of its own: GTK 3 and GTK 4 in one process
// corrupt the GObject type system, and a GTK nobody initialized has no
// display to reach the clipboard through.
//
// GTK 3 has a blocking read, gtk_clipboard_wait_for_text, which runs a
// nested main loop until the owner answers. GTK 4 only reads
// asynchronously, so paste starts gdk_clipboard_read_text_async and
// iterates the default main context itself until the callback has fired,
// the way GTK 3 does internally and dialog_unix.go does for a modal panel.
// Both run on the UI thread inside toolkit.Call. The nested iteration also
// dispatches other queued UI work, as the GTK 3 read already does.

package clipboard

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/terva-sh/tuohi/internal/toolkit"
)

const (
	// gdkSelectionClipboard is GDK_SELECTION_CLIPBOARD, the predefined GdkAtom
	// 69 that GTK 3 names the CLIPBOARD selection with.
	gdkSelectionClipboard = 69

	// GTK 4 reports a clipboard that offers no text, empty or holding
	// something else, as G_IO_ERROR_NOT_FOUND when the X11 owner cannot
	// convert to text or there is no owner, and as G_IO_ERROR_NOT_SUPPORTED
	// when no offered format is text.
	gIOErrorNotFound     = 1
	gIOErrorNotSupported = 15
)

var (
	bindOnce sync.Once
	bindErr  error

	gFree                      func(p uintptr)
	gMainContextIteration      func(ctx uintptr, mayBlock bool) bool
	gErrorFree                 func(err uintptr)
	gQuarkTryString            func(s string) uint32
	gtkClipboardGet            func(selection uintptr) uintptr                       // GTK 3
	gtkClipboardSetText        func(clipboard uintptr, text string, length int32)    // GTK 3
	gtkClipboardWaitForText    func(clipboard uintptr) uintptr                       // GTK 3
	gdkDisplayGetDefault       func() uintptr                                        // GTK 4
	gdkDisplayGetClipboard     func(display uintptr) uintptr                         // GTK 4
	gdkClipboardSetText        func(clipboard uintptr, text string)                  // GTK 4
	gdkClipboardReadTextAsync  func(clipboard, cancellable, callback, data uintptr)  // GTK 4
	gdkClipboardReadTextFinish func(clipboard, result uintptr, err *uintptr) uintptr // GTK 4

	readTextFn uintptr // GAsyncReadyCallback for gdk_clipboard_read_text_async

	// reads holds the GTK 4 reads in flight, keyed by the token passed as the
	// callback's user data, so only an integer crosses into C.
	readsMu sync.Mutex
	reads   = map[uintptr]*textRead{}
	readSeq uintptr
)

// textRead is one GTK 4 read. Its callback and the loop waiting for it both
// run on the UI thread.
type textRead struct {
	done bool
	text string
	err  error
}

// ready returns the toolkit with the clipboard functions bound, or an error
// when no tuohi app has started GTK or a function is missing.
func ready() (*toolkit.Toolkit, error) {
	tk := toolkit.Get()
	if tk == nil || tk.GTK == 0 || tk.GLib == 0 {
		return nil, ErrNoApp
	}
	bindOnce.Do(func() { bindErr = bind(tk) })
	return tk, bindErr
}

// bind resolves the functions for the loaded GTK. A missing one is reported
// in the error rather than as the panic purego.RegisterLibFunc would raise.
func bind(tk *toolkit.Toolkit) error {
	var missing []string
	need := func(fptr any, lib uintptr, name string) {
		addr, err := purego.Dlsym(lib, name)
		if err != nil {
			missing = append(missing, name)
			return
		}
		purego.RegisterFunc(fptr, addr)
	}
	need(&gFree, tk.GLib, "g_free")
	need(&gMainContextIteration, tk.GLib, "g_main_context_iteration")
	need(&gErrorFree, tk.GLib, "g_error_free")
	need(&gQuarkTryString, tk.GLib, "g_quark_try_string")
	if tk.GTK4 {
		need(&gdkDisplayGetDefault, tk.GTK, "gdk_display_get_default")
		need(&gdkDisplayGetClipboard, tk.GTK, "gdk_display_get_clipboard")
		need(&gdkClipboardSetText, tk.GTK, "gdk_clipboard_set_text")
		need(&gdkClipboardReadTextAsync, tk.GTK, "gdk_clipboard_read_text_async")
		need(&gdkClipboardReadTextFinish, tk.GTK, "gdk_clipboard_read_text_finish")
		// void (*GAsyncReadyCallback)(GObject *source, GAsyncResult *res,
		// gpointer token).
		readTextFn = purego.NewCallback(func(source, result, token uintptr) uintptr {
			readsMu.Lock()
			r := reads[token]
			readsMu.Unlock()
			if r != nil {
				r.text, r.err = finishRead(source, result)
				r.done = true
			}
			return 0
		})
	} else {
		need(&gtkClipboardGet, tk.GTK, "gtk_clipboard_get")
		need(&gtkClipboardSetText, tk.GTK, "gtk_clipboard_set_text")
		need(&gtkClipboardWaitForText, tk.GTK, "gtk_clipboard_wait_for_text")
	}
	if len(missing) > 0 {
		return fmt.Errorf("clipboard: the loaded GTK lacks %s", strings.Join(missing, ", "))
	}
	return nil
}

// onUI runs f on the UI thread through tk.Call and returns f's error. A
// Call that finds no loop to run f is ErrNoApp.
func onUI(tk *toolkit.Toolkit, f func() error) error {
	var err error
	if callErr := tk.Call(func() { err = f() }); callErr != nil {
		return fmt.Errorf("%w: %v", ErrNoApp, callErr)
	}
	return err
}

func copyText(text string) error {
	tk, err := ready()
	if err != nil {
		return err
	}
	return onUI(tk, func() error {
		if !tk.GTK4 {
			gtkClipboardSetText(gtkClipboardGet(gdkSelectionClipboard), text, -1)
			return nil
		}
		cb, err := gdkClipboard()
		if err != nil {
			return err
		}
		gdkClipboardSetText(cb, text)
		return nil
	})
}

func paste() (string, error) {
	tk, err := ready()
	if err != nil {
		return "", err
	}
	var text string
	err = onUI(tk, func() error {
		if !tk.GTK4 {
			text = takeString(gtkClipboardWaitForText(gtkClipboardGet(gdkSelectionClipboard)))
			return nil
		}
		cb, err := gdkClipboard()
		if err != nil {
			return err
		}
		text, err = readText(cb)
		return err
	})
	return text, err
}

// gdkClipboard returns the default display's clipboard. It runs on the UI
// thread.
func gdkClipboard() (uintptr, error) {
	display := gdkDisplayGetDefault()
	if display == 0 {
		return 0, errors.New("clipboard: GTK has no default display")
	}
	return gdkDisplayGetClipboard(display), nil
}

// readText starts a GTK 4 read and iterates the default main context until
// its callback has fired. It runs on the UI thread.
func readText(clipboard uintptr) (string, error) {
	r := &textRead{}
	readsMu.Lock()
	readSeq++
	token := readSeq
	reads[token] = r
	readsMu.Unlock()
	defer func() {
		readsMu.Lock()
		delete(reads, token)
		readsMu.Unlock()
	}()
	gdkClipboardReadTextAsync(clipboard, 0, readTextFn, token)
	for !r.done {
		gMainContextIteration(0, true)
	}
	return r.text, r.err
}

// finishRead completes a GTK 4 read in its callback. A clipboard with no
// text on it is "" and no error, as GTK 3 reports it.
func finishRead(clipboard, result uintptr) (string, error) {
	var gerr uintptr
	text := gdkClipboardReadTextFinish(clipboard, result, &gerr)
	if gerr == 0 {
		return takeString(text), nil
	}
	defer gErrorFree(gerr)
	e := (*gError)(ptr(gerr))
	if e.domain == gQuarkTryString("g-io-error-quark") && (e.code == gIOErrorNotFound || e.code == gIOErrorNotSupported) {
		return "", nil
	}
	return "", fmt.Errorf("clipboard: %s", cstr(e.message))
}

// gError mirrors the C GError struct.
type gError struct {
	domain  uint32 // GQuark
	code    int32
	message uintptr // gchar*
}

// takeString copies a C string GTK handed over and frees it with g_free.
func takeString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := cstr(p)
	gFree(p)
	return s
}

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion. The values it is fed are C heap pointers the Go
// GC neither owns nor moves; the spelling only keeps go vet's unsafeptr check
// quiet.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } // #nosec G103 -- audited FFI reinterpret

// cstr reads a NUL-terminated C string.
func cstr(p uintptr) string {
	base := ptr(p)
	var n int
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n)) // #nosec G103 -- slice over the C string buffer
}
