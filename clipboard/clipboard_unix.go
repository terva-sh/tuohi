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
//
// An image is read as the bytes of one offered format, decoded in Go
// (image.go), rather than through gdk_clipboard_read_texture_async or
// gtk_clipboard_wait_for_image: those decode through gdk-pixbuf, whose BMP
// loader is optional, and allocate the whole image before tuohi can see its
// size. GTK 4 passes gdk_clipboard_read_async the types in order of
// preference, and GDK opens a stream on the first one offered, which paste
// reads in chunks with g_input_stream_read_bytes_async and stops reading past
// the byte limit. GTK 3 asks for the offered targets, then waits for the
// contents of the best one, which GTK buffers whole.

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

	gInternString                     func(s string) uintptr
	gObjectUnref                      func(obj uintptr)
	gBytesGetData                     func(bytes uintptr, size *uintptr) uintptr
	gBytesUnref                       func(bytes uintptr)
	gdkAtomIntern                     func(name string, onlyIfExists bool) uintptr                                                 // GTK 3
	gdkAtomName                       func(atom uintptr) uintptr                                                                   // GTK 3
	gtkClipboardWaitForTargets        func(clipboard uintptr, targets *uintptr, n *int32) bool                                     // GTK 3
	gtkClipboardWaitForContents       func(clipboard, target uintptr) uintptr                                                      // GTK 3
	gtkSelectionDataGetDataWithLength func(data uintptr, length *int32) uintptr                                                    // GTK 3
	gtkSelectionDataFree              func(data uintptr)                                                                           // GTK 3
	gdkClipboardReadAsync             func(clipboard uintptr, mimeTypes *uintptr, prio int32, cancellable, callback, data uintptr) // GTK 4
	gdkClipboardReadFinish            func(clipboard, result uintptr, mimeType *uintptr, err *uintptr) uintptr                     // GTK 4
	gInputStreamReadBytesAsync        func(stream, count uintptr, prio int32, cancellable, callback, data uintptr)                 // GTK 4
	gInputStreamReadBytesFinish       func(stream, result uintptr, err *uintptr) uintptr                                           // GTK 4

	// imageMimeTypes is imageTypes as a NULL-terminated array of interned C
	// strings, for gdk_clipboard_read_async. GTK 4.
	imageMimeTypes []uintptr

	asyncFn uintptr // the GAsyncReadyCallback of every GTK 4 read

	// pending holds the GTK 4 calls in flight, keyed by the token passed as
	// the callback's user data, so only an integer crosses into C.
	pendingMu  sync.Mutex
	pending    = map[uintptr]func(source, result uintptr){}
	pendingSeq uintptr
)

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
		need(&gdkClipboardReadAsync, tk.GTK, "gdk_clipboard_read_async")
		need(&gdkClipboardReadFinish, tk.GTK, "gdk_clipboard_read_finish")
		// GIO and GObject are GTK's own dependencies, which dlsym searches
		// through its handle.
		need(&gInputStreamReadBytesAsync, tk.GTK, "g_input_stream_read_bytes_async")
		need(&gInputStreamReadBytesFinish, tk.GTK, "g_input_stream_read_bytes_finish")
		need(&gObjectUnref, tk.GTK, "g_object_unref")
		need(&gBytesGetData, tk.GLib, "g_bytes_get_data")
		need(&gBytesUnref, tk.GLib, "g_bytes_unref")
		need(&gInternString, tk.GLib, "g_intern_string")
		// void (*GAsyncReadyCallback)(GObject *source, GAsyncResult *res,
		// gpointer token).
		asyncFn = purego.NewCallback(func(source, result, token uintptr) uintptr {
			pendingMu.Lock()
			f := pending[token]
			pendingMu.Unlock()
			if f != nil {
				f(source, result)
			}
			return 0
		})
	} else {
		need(&gtkClipboardGet, tk.GTK, "gtk_clipboard_get")
		need(&gtkClipboardSetText, tk.GTK, "gtk_clipboard_set_text")
		need(&gtkClipboardWaitForText, tk.GTK, "gtk_clipboard_wait_for_text")
		need(&gtkClipboardWaitForTargets, tk.GTK, "gtk_clipboard_wait_for_targets")
		need(&gtkClipboardWaitForContents, tk.GTK, "gtk_clipboard_wait_for_contents")
		need(&gtkSelectionDataGetDataWithLength, tk.GTK, "gtk_selection_data_get_data_with_length")
		need(&gtkSelectionDataFree, tk.GTK, "gtk_selection_data_free")
		need(&gdkAtomIntern, tk.GTK, "gdk_atom_intern")
		need(&gdkAtomName, tk.GTK, "gdk_atom_name")
	}
	if len(missing) > 0 {
		return fmt.Errorf("clipboard: the loaded GTK lacks %s", strings.Join(missing, ", "))
	}
	if tk.GTK4 {
		for _, t := range imageTypes {
			imageMimeTypes = append(imageMimeTypes, gInternString(t))
		}
		imageMimeTypes = append(imageMimeTypes, 0)
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

// await starts a GTK 4 asynchronous call with start, passing it the
// callback and its token, and iterates the default main context until the
// callback has run finish. It runs on the UI thread, where the callback
// runs too.
func await(start func(callback, token uintptr), finish func(source, result uintptr)) {
	done := false
	pendingMu.Lock()
	pendingSeq++
	token := pendingSeq
	pending[token] = func(source, result uintptr) {
		finish(source, result)
		done = true
	}
	pendingMu.Unlock()
	defer func() {
		pendingMu.Lock()
		delete(pending, token)
		pendingMu.Unlock()
	}()
	start(asyncFn, token)
	for !done {
		gMainContextIteration(0, true)
	}
}

// readText reads the GTK 4 clipboard's text. A clipboard with no text on it
// is "" and no error, as GTK 3 reports it. It runs on the UI thread.
func readText(clipboard uintptr) (text string, err error) {
	await(func(callback, token uintptr) {
		gdkClipboardReadTextAsync(clipboard, 0, callback, token)
	}, func(source, result uintptr) {
		var gerr uintptr
		p := gdkClipboardReadTextFinish(source, result, &gerr)
		if gerr == 0 {
			text = takeString(p)
			return
		}
		takeNotOffered(gerr, &err)
	})
	return text, err
}

// takeNotOffered frees the GError gerr and reports whether it says the
// clipboard offers nothing in the format asked for. Any other error it
// stores in *err.
func takeNotOffered(gerr uintptr, err *error) bool {
	defer gErrorFree(gerr)
	e := (*gError)(ptr(gerr))
	if e.domain == gQuarkTryString("g-io-error-quark") && (e.code == gIOErrorNotFound || e.code == gIOErrorNotSupported) {
		return true
	}
	*err = fmt.Errorf("clipboard: %s", cstr(e.message))
	return false
}

func pasteImage(limits ImageLimits) ([]byte, error) {
	tk, err := ready()
	if err != nil {
		return nil, err
	}
	var mimeType string
	var data []byte
	err = onUI(tk, func() error {
		var err error
		if !tk.GTK4 {
			mimeType, data, err = waitForImage(limits)
			return err
		}
		cb, err := gdkClipboard()
		if err != nil {
			return err
		}
		mimeType, data, err = readImage(cb, limits)
		return err
	})
	if err != nil {
		return nil, err
	}
	// Decoding runs off the UI thread, wherever the caller is.
	return encodeImage(mimeType, data, limits)
}

// waitForImage reads the bytes of the best image type the GTK 3 clipboard
// offers. It runs on the UI thread.
func waitForImage(limits ImageLimits) (string, []byte, error) {
	clipboard := gtkClipboardGet(gdkSelectionClipboard)
	var atoms uintptr
	var n int32
	if !gtkClipboardWaitForTargets(clipboard, &atoms, &n) || atoms == 0 {
		return "", nil, ErrNoImage
	}
	offered := make([]string, 0, n)
	for _, atom := range unsafe.Slice((*uintptr)(ptr(atoms)), n) { // #nosec G103 -- the GdkAtom array GTK returned
		offered = append(offered, takeString(gdkAtomName(atom)))
	}
	gFree(atoms)
	mimeType := bestImageType(offered)
	if mimeType == "" {
		return "", nil, ErrNoImage
	}
	sel := gtkClipboardWaitForContents(clipboard, gdkAtomIntern(mimeType, false))
	if sel == 0 {
		return "", nil, ErrNoImage
	}
	defer gtkSelectionDataFree(sel)
	var length int32
	p := gtkSelectionDataGetDataWithLength(sel, &length)
	if p == 0 || length <= 0 {
		return "", nil, ErrNoImage
	}
	if int(length) > limits.MaxBytes {
		return "", nil, fmt.Errorf("%w: %s is over %d bytes", ErrImageTooLarge, mimeType, limits.MaxBytes)
	}
	return mimeType, append([]byte(nil), unsafe.Slice((*byte)(ptr(p)), length)...), nil // #nosec G103 -- the selection data GTK returned
}

// readChunk is how much one GTK 4 stream read asks for.
const readChunk = 64 << 10

// readImage reads the bytes of the best image type the GTK 4 clipboard
// offers, stopping once they pass the byte limit. It runs on the UI thread.
func readImage(clipboard uintptr, limits ImageLimits) (mimeType string, data []byte, err error) {
	var stream uintptr
	notOffered := false
	await(func(callback, token uintptr) {
		gdkClipboardReadAsync(clipboard, &imageMimeTypes[0], 0, 0, callback, token)
	}, func(source, result uintptr) {
		var gerr, mime uintptr
		stream = gdkClipboardReadFinish(source, result, &mime, &gerr)
		if gerr != 0 {
			notOffered = takeNotOffered(gerr, &err)
			return
		}
		if mime != 0 {
			mimeType = cstr(mime)
		}
	})
	if notOffered {
		return "", nil, ErrNoImage
	}
	if err != nil {
		return "", nil, err
	}
	if stream == 0 {
		return "", nil, ErrNoImage
	}
	// Dropping the last reference closes the stream, also mid-transfer.
	defer gObjectUnref(stream)
	for eof := false; !eof && err == nil; {
		await(func(callback, token uintptr) {
			gInputStreamReadBytesAsync(stream, readChunk, 0, 0, callback, token)
		}, func(source, result uintptr) {
			var gerr uintptr
			bytes := gInputStreamReadBytesFinish(source, result, &gerr)
			if gerr != 0 {
				defer gErrorFree(gerr)
				err = fmt.Errorf("clipboard: read %s: %s", mimeType, cstr((*gError)(ptr(gerr)).message))
				return
			}
			defer gBytesUnref(bytes)
			var size uintptr
			p := gBytesGetData(bytes, &size)
			if size == 0 {
				eof = true
				return
			}
			if uintptr(len(data))+size > uintptr(limits.MaxBytes) {
				err = fmt.Errorf("%w: %s is over %d bytes", ErrImageTooLarge, mimeType, limits.MaxBytes)
				return
			}
			data = append(data, unsafe.Slice((*byte)(ptr(p)), size)...) // #nosec G103 -- the GBytes GIO returned
		})
	}
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 {
		return "", nil, ErrNoImage
	}
	return mimeType, data, nil
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
