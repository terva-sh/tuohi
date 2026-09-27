//go:build linux || freebsd || netbsd

// Unix open/save panels (Linux, FreeBSD, NetBSD): GtkFileChooserNative via pure. The modal is
// driven manually (set_modal + show + "response" signal + main-loop
// iteration) because gtk_native_dialog_run was removed in GTK4; the manual
// sequence is exactly what it did internally and works on both GTK3 and
// GTK4.
//
// Stack selection: loading GTK3 and GTK4 into one process corrupts the
// GObject type system, so the package first probes with RTLD_NOLOAD for a
// GTK the host process ALREADY loaded (a webview app that runs on GTK) and
// joins it. Only when neither is present does it load one fresh: GTK3 first
// (the stack most desktops ship), then GTK4.
//
// Threading: everything here runs on the calling thread, which the package
// contract requires to be the main thread; gtk_init and every later GTK call
// then agree on the thread. A process with no display fails gtk_init_check
// and every panel returns "".

package dialog

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/malivvan/appkit/pure"
)

const (
	// RTLD_NOLOAD (glibc and musl): return the handle only if the library is
	// already mapped, never load it. pure does not export it.
	rtldNoload = 0x4

	gtkFileChooserActionOpen         = 0
	gtkFileChooserActionSave         = 1
	gtkFileChooserActionSelectFolder = 2

	gtkResponseAccept = -3
)

var (
	initOnce sync.Once
	initErr  error
	gtk4     bool

	gtkInitCheck3 func(argc, argv uintptr) bool
	gtkInitCheck4 func() bool

	gtkFileChooserNativeNew         func(title string, parent uintptr, action int, accept, cancel string) uintptr
	gtkNativeDialogShow             func(dialog uintptr)
	gtkNativeDialogHide             func(dialog uintptr)
	gtkNativeDialogSetModal         func(dialog uintptr, modal bool)
	gtkFileChooserSetSelectMultiple func(chooser uintptr, multiple bool)
	gtkFileChooserSetCurrentName    func(chooser uintptr, name string)
	gtkFileFilterNew                func() uintptr
	gtkFileFilterSetName            func(filter uintptr, name string)
	gtkFileFilterAddPattern         func(filter uintptr, pattern string)
	gtkFileChooserAddFilter         func(chooser, filter uintptr)

	// GTK3 path-based results + folder selection.
	gtkFileChooserGetFilename      func(chooser uintptr) uintptr // char*
	gtkFileChooserGetFilenames     func(chooser uintptr) uintptr // GSList* of char*
	gtkFileChooserSetCurrentFolder func(chooser uintptr, path string) bool
	gSListFree                     func(list uintptr)

	// GTK4 GFile/GListModel-based results + folder selection (need GIO).
	gtkFileChooserGetFile           func(chooser uintptr) uintptr // GFile*
	gtkFileChooserGetFiles          func(chooser uintptr) uintptr // GListModel*
	gtkFileChooserSetCurrentFolder4 func(chooser, file, err uintptr) bool
	gFileNewForPath                 func(path string) uintptr
	gFileGetPath                    func(file uintptr) uintptr // char*
	gListModelGetNItems             func(model uintptr) uint32
	gListModelGetItem               func(model uintptr, pos uint32) uintptr

	gFree                 func(p uintptr)
	gObjectUnref          func(obj uintptr)
	gSignalConnectData    func(instance uintptr, signal string, handler, data uintptr, destroy, flags uintptr) uint64
	gMainContextIteration func(ctx uintptr, mayBlock bool) bool

	dialogResponseFn uintptr // GtkNativeDialog "response" callback

	gtkInitOnce sync.Once
	gtkInitOK   bool
)

// dialogResp captures a single modal dialog's response. Dialogs run one at a
// time on the UI thread (modal), keyed by an integer token passed as the
// signal's user_data so only integers cross into C.
type dialogResp struct {
	response int
	done     bool
}

var (
	dialogRespMu     sync.Mutex
	dialogRespStates = map[uintptr]*dialogResp{}
	dialogRespSeq    uintptr
)

// openGTK picks the GTK stack: join the one already mapped into the process if
// any (RTLD_NOLOAD probe - never loads), else load GTK3, else GTK4.
func openGTK() (uintptr, error) {
	lib, err := pure.Dlopen("libgtk-4.so.1", pure.RTLD_LAZY|rtldNoload)
	if err == nil {
		gtk4 = true
		return lib, nil
	}
	lib, err = pure.Dlopen("libgtk-3.so.0", pure.RTLD_LAZY|rtldNoload)
	if err == nil {
		return lib, nil
	}
	lib, err = pure.Dlopen("libgtk-3.so.0", pure.RTLD_LAZY|pure.RTLD_GLOBAL)
	if err == nil {
		return lib, nil
	}
	lib, err = pure.Dlopen("libgtk-4.so.1", pure.RTLD_LAZY|pure.RTLD_GLOBAL)
	if err == nil {
		gtk4 = true
		return lib, nil
	}
	return 0, errors.New("dialog: neither libgtk-3.so.0 nor libgtk-4.so.1 could be loaded")
}

func ensureInit() error {
	initOnce.Do(func() {
		gtkLib, err := openGTK()
		if err != nil {
			initErr = err
			return
		}
		glib, err := pure.Dlopen("libglib-2.0.so.0", pure.RTLD_LAZY|pure.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}
		gobject, err := pure.Dlopen("libgobject-2.0.so.0", pure.RTLD_LAZY|pure.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}

		pure.RegisterLibFunc(&gtkFileChooserNativeNew, gtkLib, "gtk_file_chooser_native_new")
		pure.RegisterLibFunc(&gtkNativeDialogShow, gtkLib, "gtk_native_dialog_show")
		pure.RegisterLibFunc(&gtkNativeDialogHide, gtkLib, "gtk_native_dialog_hide")
		pure.RegisterLibFunc(&gtkNativeDialogSetModal, gtkLib, "gtk_native_dialog_set_modal")
		pure.RegisterLibFunc(&gtkFileChooserSetSelectMultiple, gtkLib, "gtk_file_chooser_set_select_multiple")
		pure.RegisterLibFunc(&gtkFileChooserSetCurrentName, gtkLib, "gtk_file_chooser_set_current_name")
		pure.RegisterLibFunc(&gtkFileFilterNew, gtkLib, "gtk_file_filter_new")
		pure.RegisterLibFunc(&gtkFileFilterSetName, gtkLib, "gtk_file_filter_set_name")
		pure.RegisterLibFunc(&gtkFileFilterAddPattern, gtkLib, "gtk_file_filter_add_pattern")
		pure.RegisterLibFunc(&gtkFileChooserAddFilter, gtkLib, "gtk_file_chooser_add_filter")

		pure.RegisterLibFunc(&gFree, glib, "g_free")
		pure.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		pure.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		pure.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")

		// "response" delivers (GtkNativeDialog*, gint response_id, gpointer
		// token). gint is 32-bit; mask before interpreting so a negative id
		// (ACCEPT = -3) survives the widening into a uintptr register.
		dialogResponseFn = pure.NewCallback(func(_, responseID, token uintptr) uintptr {
			dialogRespMu.Lock()
			st := dialogRespStates[token]
			if st != nil {
				st.response = int(int32(uint32(responseID))) // #nosec G115 -- deliberate gint narrowing
				st.done = true
			}
			dialogRespMu.Unlock()
			return 0
		})

		if gtk4 {
			pure.RegisterLibFunc(&gtkInitCheck4, gtkLib, "gtk_init_check")
			pure.RegisterLibFunc(&gtkFileChooserGetFile, gtkLib, "gtk_file_chooser_get_file")
			pure.RegisterLibFunc(&gtkFileChooserGetFiles, gtkLib, "gtk_file_chooser_get_files")
			pure.RegisterLibFunc(&gtkFileChooserSetCurrentFolder4, gtkLib, "gtk_file_chooser_set_current_folder")
			gio, e := pure.Dlopen("libgio-2.0.so.0", pure.RTLD_LAZY|pure.RTLD_GLOBAL)
			if e != nil {
				initErr = e
				return
			}
			pure.RegisterLibFunc(&gFileNewForPath, gio, "g_file_new_for_path")
			pure.RegisterLibFunc(&gFileGetPath, gio, "g_file_get_path")
			pure.RegisterLibFunc(&gListModelGetNItems, gio, "g_list_model_get_n_items")
			pure.RegisterLibFunc(&gListModelGetItem, gio, "g_list_model_get_item")
			return
		}
		pure.RegisterLibFunc(&gtkInitCheck3, gtkLib, "gtk_init_check")
		pure.RegisterLibFunc(&gtkFileChooserGetFilename, gtkLib, "gtk_file_chooser_get_filename")
		pure.RegisterLibFunc(&gtkFileChooserGetFilenames, gtkLib, "gtk_file_chooser_get_filenames")
		pure.RegisterLibFunc(&gtkFileChooserSetCurrentFolder, gtkLib, "gtk_file_chooser_set_current_folder")
		pure.RegisterLibFunc(&gSListFree, glib, "g_slist_free")
	})
	return initErr
}

// gtkReady loads GTK and initializes it once, on the calling (main) thread.
// false means no usable display (or no GTK), in which case every panel
// degrades to "".
func gtkReady() bool {
	err := ensureInit()
	if err != nil {
		return false
	}
	gtkInitOnce.Do(func() {
		// gtk_init_check's arity differs by major version: GTK3 takes
		// (int *argc, char ***argv), GTK4 takes no arguments.
		if gtk4 {
			gtkInitOK = gtkInitCheck4()
			return
		}
		gtkInitOK = gtkInitCheck3(0, 0)
	})
	return gtkInitOK
}

// --- the panels ------------------------------------------------------------

func open(opts Options) string {
	return firstPath(runFileChooser(gtkFileChooserActionOpen, false, opts))
}

func openMultiple(opts Options) []string {
	return runFileChooser(gtkFileChooserActionOpen, true, opts)
}

func save(opts Options) string {
	return firstPath(runFileChooser(gtkFileChooserActionSave, false, opts))
}

func pickDirectory(opts Options) string {
	return firstPath(runFileChooser(gtkFileChooserActionSelectFolder, false, opts))
}

func runFileChooser(action int, multi bool, opts Options) []string {
	if !gtkReady() {
		return nil
	}
	accept := "_Open"
	if action == gtkFileChooserActionSave {
		accept = "_Save"
	}
	dlg := gtkFileChooserNativeNew(opts.Title, 0, action, accept, "_Cancel")
	if dlg == 0 {
		return nil
	}
	defer gObjectUnref(dlg)

	if multi {
		gtkFileChooserSetSelectMultiple(dlg, true)
	}
	if opts.Directory != "" {
		setChooserFolder(dlg, opts.Directory)
	}
	if action == gtkFileChooserActionSave && opts.Filename != "" {
		gtkFileChooserSetCurrentName(dlg, opts.Filename)
	}
	if action != gtkFileChooserActionSelectFolder {
		if len(opts.Filters) > 0 {
			applyChooserFilters(dlg, opts.Filters)
		} else {
			applyChooserFilter(dlg, opts.Extensions)
		}
	}

	if runNativeDialog(dlg) != gtkResponseAccept {
		return nil
	}
	return chooserPaths(dlg, multi)
}

// runNativeDialog shows a GtkNativeDialog modally and pumps the main loop until
// the user responds, returning the response id.
func runNativeDialog(dlg uintptr) int {
	dialogRespMu.Lock()
	dialogRespSeq++
	token := dialogRespSeq
	st := &dialogResp{}
	dialogRespStates[token] = st
	dialogRespMu.Unlock()
	defer func() {
		dialogRespMu.Lock()
		delete(dialogRespStates, token)
		dialogRespMu.Unlock()
	}()

	gSignalConnectData(dlg, "response", dialogResponseFn, token, 0, 0)
	gtkNativeDialogSetModal(dlg, true)
	gtkNativeDialogShow(dlg)
	for {
		dialogRespMu.Lock()
		done := st.done
		dialogRespMu.Unlock()
		if done {
			break
		}
		gMainContextIteration(0, true)
	}
	gtkNativeDialogHide(dlg)
	return st.response
}

func setChooserFolder(chooser uintptr, dir string) {
	if gtk4 {
		file := gFileNewForPath(dir)
		if file == 0 {
			return
		}
		gtkFileChooserSetCurrentFolder4(chooser, file, 0)
		gObjectUnref(file)
		return
	}
	gtkFileChooserSetCurrentFolder(chooser, dir)
}

// applyChooserFilter restricts the chooser to Options.Extensions as one
// GtkFileFilter ("*.a, *.b"). No restriction adds no filter.
func applyChooserFilter(chooser uintptr, exts []string) {
	clean := cleanExtensions(exts)
	if clean == nil {
		return
	}
	filter := gtkFileFilterNew()
	name := ""
	for i, e := range clean {
		if i > 0 {
			name += ", "
		}
		name += "*." + e
	}
	gtkFileFilterSetName(filter, name)
	for _, e := range clean {
		gtkFileFilterAddPattern(filter, "*."+e)
	}
	gtkFileChooserAddFilter(chooser, filter) // transfers ownership to the chooser
}

// applyChooserFilters turns Options.Filters into one GtkFileFilter per named
// filter, shown as separate choices in the panel's type dropdown. A filter
// with no usable extensions is skipped.
func applyChooserFilters(chooser uintptr, filters []FileFilter) {
	for _, f := range filters {
		clean := cleanExtensions(f.Extensions)
		if clean == nil {
			continue
		}
		name := f.Name
		if name == "" {
			name = ""
			for i, e := range clean {
				if i > 0 {
					name += ", "
				}
				name += "*." + e
			}
		}
		filter := gtkFileFilterNew()
		gtkFileFilterSetName(filter, name)
		for _, e := range clean {
			gtkFileFilterAddPattern(filter, "*."+e)
		}
		gtkFileChooserAddFilter(chooser, filter) // transfers ownership to the chooser
	}
}

// chooserPaths reads the selected path(s) out of a chooser after an accepted
// run, branching on the GTK version.
func chooserPaths(chooser uintptr, multi bool) []string {
	if gtk4 {
		if multi {
			return gListModelPaths(gtkFileChooserGetFiles(chooser))
		}
		file := gtkFileChooserGetFile(chooser) // transfer full
		if file == 0 {
			return nil
		}
		defer gObjectUnref(file)
		if p := gfilePath(file); p != "" {
			return []string{p}
		}
		return nil
	}
	if multi {
		return gSListPaths(gtkFileChooserGetFilenames(chooser))
	}
	cs := gtkFileChooserGetFilename(chooser) // char*, owned by caller
	if cs == 0 {
		return nil
	}
	p := cstr(cs)
	gFree(cs)
	if p == "" {
		return nil
	}
	return []string{p}
}

// gfilePath returns a GFile's local path ("" if it has none).
func gfilePath(file uintptr) string {
	cs := gFileGetPath(file) // char*, owned by caller
	if cs == 0 {
		return ""
	}
	p := cstr(cs)
	gFree(cs)
	return p
}

// gListModelPaths drains a GListModel<GFile> (GTK4 multi-select result) into
// paths, releasing the model and each item.
func gListModelPaths(model uintptr) []string {
	if model == 0 {
		return nil
	}
	defer gObjectUnref(model)
	n := gListModelGetNItems(model)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		file := gListModelGetItem(model, i) // transfer full (a ref)
		if file == 0 {
			continue
		}
		if p := gfilePath(file); p != "" {
			paths = append(paths, p)
		}
		gObjectUnref(file)
	}
	return paths
}

// gSListPaths drains a GSList<char*> (GTK3 multi-select result) into paths,
// freeing each string and the list.
func gSListPaths(list uintptr) []string {
	if list == 0 {
		return nil
	}
	var paths []string
	for node := list; node != 0; {
		data := *(*uintptr)(ptr(node))
		next := *(*uintptr)(ptr(node + unsafe.Sizeof(uintptr(0))))
		if data != 0 {
			if p := cstr(data); p != "" {
				paths = append(paths, p)
			}
			gFree(data)
		}
		node = next
	}
	gSListFree(list)
	return paths
}

// --- C string helpers ------------------------------------------------------

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion. The values it is fed are C heap pointers the Go
// GC neither owns nor moves; the spelling only keeps go vet's unsafeptr check
// quiet.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } // #nosec G103 -- audited FFI reinterpret

// cstr reads a NUL-terminated C string.
func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n)) // #nosec G103 -- slice over the C string buffer
}
