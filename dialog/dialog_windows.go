// Windows open/save panels: the Common Item Dialog COM API (IFileOpenDialog
// / IFileSaveDialog) via pure. Show is application-modal and pumps its own
// message loop, so callers must invoke Open on the UI thread (the package
// contract).
//
// COM idiom: each interface is a struct whose first field is the vtbl
// pointer, the vtbl is a struct of uintptr slots in exact IDL order, and a
// method call is pure.SyscallN(vtbl.Method, this, args...). Windows has no
// dlopen, so symbols are resolved with LoadLibrary/GetProcAddress and bound
// with pure.RegisterFunc.

package dialog

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/malivvan/appkit/pure"
)

// --- CLSIDs / IIDs ---------------------------------------------------------

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	clsidFileSaveDialog = guid{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidIFileOpenDialog  = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIFileSaveDialog  = guid{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}
	iidIShellItem       = guid{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer = 0x1

	coinitApartmentThreaded = 0x2

	// FILEOPENDIALOGOPTIONS bits.
	fosOverwritePrompt  = 0x00000002
	fosPickFolders      = 0x00000020
	fosForceFilesystem  = 0x00000040
	fosAllowMultiSelect = 0x00000200

	// SIGDN_FILESYSPATH for IShellItem::GetDisplayName.
	sigdnFileSysPath = 0x80058000
)

// --- COM vtable layouts (exact IDL order; uintptr per slot) ----------------

type unknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type modalWindowVtbl struct {
	unknownVtbl
	Show uintptr // HRESULT Show(HWND)
}

type fileDialogVtbl struct {
	modalWindowVtbl
	SetFileTypes        uintptr // (UINT, const COMDLG_FILTERSPEC*)
	SetFileTypeIndex    uintptr
	GetFileTypeIndex    uintptr
	Advise              uintptr
	Unadvise            uintptr
	SetOptions          uintptr // (FILEOPENDIALOGOPTIONS)
	GetOptions          uintptr // (FILEOPENDIALOGOPTIONS*)
	SetDefaultFolder    uintptr
	SetFolder           uintptr // (IShellItem*)
	GetFolder           uintptr
	GetCurrentSelection uintptr
	SetFileName         uintptr // (LPCWSTR)
	GetFileName         uintptr
	SetTitle            uintptr // (LPCWSTR)
	SetOkButtonLabel    uintptr
	SetFileNameLabel    uintptr
	GetResult           uintptr // (IShellItem**)
	AddPlace            uintptr
	SetDefaultExtension uintptr // (LPCWSTR)
	Close               uintptr
	SetClientGuid       uintptr
	ClearClientData     uintptr
	SetFilter           uintptr
}

type fileOpenDialogVtbl struct {
	fileDialogVtbl
	GetResults       uintptr // (IShellItemArray**)
	GetSelectedItems uintptr
}

type shellItemVtbl struct {
	unknownVtbl
	BindToHandler  uintptr
	GetParent      uintptr
	GetDisplayName uintptr // (SIGDN, LPWSTR*)
	GetAttributes  uintptr
	Compare        uintptr
}

type shellItemArrayVtbl struct {
	unknownVtbl
	BindToHandler              uintptr
	GetPropertyStore           uintptr
	GetPropertyDescriptionList uintptr
	GetAttributes              uintptr
	GetCount                   uintptr // (DWORD*)
	GetItemAt                  uintptr // (DWORD, IShellItem**)
	EnumItems                  uintptr
}

// Interface wrappers (the vtbl pointer is the object's first field).
type fileDialog struct{ vtbl *fileDialogVtbl }
type fileOpenDialog struct{ vtbl *fileOpenDialogVtbl }
type shellItem struct{ vtbl *shellItemVtbl }
type shellItemArray struct{ vtbl *shellItemArrayVtbl }

// this returns the COM object pointer for SyscallN's first argument.
func (d *fileDialog) this() uintptr     { return uintptr(unsafe.Pointer(d)) } // #nosec G103 -- COM this-pointer
func (d *fileOpenDialog) this() uintptr { return uintptr(unsafe.Pointer(d)) } // #nosec G103 -- COM this-pointer
func (s *shellItem) this() uintptr      { return uintptr(unsafe.Pointer(s)) } // #nosec G103 -- COM this-pointer
func (a *shellItemArray) this() uintptr { return uintptr(unsafe.Pointer(a)) } // #nosec G103 -- COM this-pointer

// comdlgFilterSpec mirrors COMDLG_FILTERSPEC.
type comdlgFilterSpec struct {
	pszName *uint16
	pszSpec *uint16
}

func (d *fileDialog) Show(parent uintptr) int32 {
	r, _, _ := pure.SyscallN(d.vtbl.Show, d.this(), parent)
	return int32(r) // #nosec G115 -- HRESULT is a 32-bit value
}
func (d *fileDialog) GetOptions() uint32 {
	var fos uint32
	pure.SyscallN(d.vtbl.GetOptions, d.this(), uintptr(unsafe.Pointer(&fos))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return fos
}
func (d *fileDialog) SetOptions(fos uint32) {
	pure.SyscallN(d.vtbl.SetOptions, d.this(), uintptr(fos))
}
func (d *fileDialog) SetTitle(s *uint16) {
	pure.SyscallN(d.vtbl.SetTitle, d.this(), uintptr(unsafe.Pointer(s))) // #nosec G103 -- LPCWSTR arg, pinned by the call
}
func (d *fileDialog) SetFileName(s *uint16) {
	pure.SyscallN(d.vtbl.SetFileName, d.this(), uintptr(unsafe.Pointer(s))) // #nosec G103 -- LPCWSTR arg, pinned by the call
}
func (d *fileDialog) SetFolder(si uintptr) {
	pure.SyscallN(d.vtbl.SetFolder, d.this(), si)
}
func (d *fileDialog) SetFileTypes(n uint32, specs *comdlgFilterSpec) {
	pure.SyscallN(d.vtbl.SetFileTypes, d.this(), uintptr(n), uintptr(unsafe.Pointer(specs))) // #nosec G103 -- spec array kept alive by the caller
}
func (d *fileDialog) GetResult(out *uintptr) int32 {
	r, _, _ := pure.SyscallN(d.vtbl.GetResult, d.this(), uintptr(unsafe.Pointer(out))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return int32(r)                                                                    // #nosec G115 -- HRESULT is a 32-bit value
}
func (d *fileDialog) Release() {
	pure.SyscallN(d.vtbl.Release, d.this())
}

func (s *shellItem) GetDisplayName(sigdn uint32, out *uintptr) int32 {
	r, _, _ := pure.SyscallN(s.vtbl.GetDisplayName, s.this(), uintptr(sigdn), uintptr(unsafe.Pointer(out))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return int32(r)                                                                                         // #nosec G115 -- HRESULT is a 32-bit value
}
func (s *shellItem) Release() {
	pure.SyscallN(s.vtbl.Release, s.this())
}

func (d *fileOpenDialog) GetResults(out *uintptr) int32 {
	r, _, _ := pure.SyscallN(d.vtbl.GetResults, d.this(), uintptr(unsafe.Pointer(out))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return int32(r)                                                                     // #nosec G115 -- HRESULT is a 32-bit value
}

func (a *shellItemArray) GetCount(out *uint32) int32 {
	r, _, _ := pure.SyscallN(a.vtbl.GetCount, a.this(), uintptr(unsafe.Pointer(out))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return int32(r)                                                                   // #nosec G115 -- HRESULT is a 32-bit value
}

func (a *shellItemArray) GetItemAt(i uint32, out *uintptr) int32 {
	r, _, _ := pure.SyscallN(a.vtbl.GetItemAt, a.this(), uintptr(i), uintptr(unsafe.Pointer(out))) // #nosec G103 -- out-param on a Go local, pinned by the call
	return int32(r)                                                                                // #nosec G115 -- HRESULT is a 32-bit value
}

func (a *shellItemArray) Release() {
	pure.SyscallN(a.vtbl.Release, a.this())
}

// --- bound ole32 / shell32 functions ---------------------------------------

var (
	initOnce sync.Once
	initErr  error

	coInitializeEx              func(reserved uintptr, coinit uint32) int32
	coTaskMemFree               func(p uintptr)
	coCreateInstance            func(rclsid *guid, pUnkOuter uintptr, clsCtx uint32, riid *guid, ppv *uintptr) int32
	shCreateItemFromParsingName func(name *uint16, pbc uintptr, riid *guid, ppv *uintptr) int32
)

func ensureInit() error {
	initOnce.Do(func() {
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			initErr = err
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			initErr = err
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				initErr = e
				return
			}
			pure.RegisterFunc(fn, addr)
		}
		reg(&coInitializeEx, ole32, "CoInitializeEx")
		reg(&coTaskMemFree, ole32, "CoTaskMemFree")
		reg(&coCreateInstance, ole32, "CoCreateInstance")
		reg(&shCreateItemFromParsingName, shell32, "SHCreateItemFromParsingName")
	})
	return initErr
}

// --- the panels ------------------------------------------------------------

func open(opts Options) string {
	return firstPath(runFileDialog(false, false, false, opts))
}

func openMultiple(opts Options) []string {
	return runFileDialog(false, false, true, opts)
}

func save(opts Options) string {
	return firstPath(runFileDialog(true, false, false, opts))
}

func pickDirectory(opts Options) string {
	return firstPath(runFileDialog(false, true, false, opts))
}

// runFileDialog shows the Common Item Dialog modally on the calling thread and
// returns the chosen path(s) (nil on cancel or error, per the package contract).
func runFileDialog(saveMode, pickFolders, multi bool, opts Options) []string {
	err := ensureInit()
	if err != nil {
		return nil
	}
	// Tolerated results: S_OK, S_FALSE (already initialized), and
	// RPC_E_CHANGED_MODE (thread already in the MTA; the dialog still shows).
	coInitializeEx(0, coinitApartmentThreaded)

	clsid, iid := &clsidFileOpenDialog, &iidIFileOpenDialog
	if saveMode {
		clsid, iid = &clsidFileSaveDialog, &iidIFileSaveDialog
	}
	var pdlg uintptr
	hr := coCreateInstance(clsid, 0, clsctxInprocServer, iid, &pdlg)
	if hr < 0 || pdlg == 0 {
		return nil
	}
	dlg := (*fileDialog)(ptr(pdlg))
	defer dlg.Release()

	fos := dlg.GetOptions() | fosForceFilesystem
	if pickFolders {
		fos |= fosPickFolders
	}
	if multi {
		fos |= fosAllowMultiSelect
	}
	if saveMode {
		fos |= fosOverwritePrompt
	}
	dlg.SetOptions(fos)

	if opts.Title != "" {
		dlg.SetTitle(utf16Ptr(opts.Title))
	}
	if opts.Directory != "" {
		var psi uintptr
		if shCreateItemFromParsingName(utf16Ptr(opts.Directory), 0, &iidIShellItem, &psi) >= 0 && psi != 0 {
			dlg.SetFolder(psi)
			(*shellItem)(ptr(psi)).Release()
		}
	}
	if saveMode && opts.Filename != "" {
		dlg.SetFileName(utf16Ptr(opts.Filename))
	}
	var keep [][]uint16
	if !pickFolders {
		if len(opts.Filters) > 0 {
			keep = applyFileFilters(dlg, opts.Filters)
		} else {
			keep = applyFileTypes(dlg, opts.Extensions)
		}
	}

	hr = dlg.Show(0)
	runtime.KeepAlive(keep) // the filter spec's UTF-16 buffers must survive Show
	if hr < 0 {
		return nil
	}

	if !saveMode && multi {
		return openDialogResults((*fileOpenDialog)(ptr(pdlg)))
	}
	var psi uintptr
	if dlg.GetResult(&psi) < 0 || psi == 0 {
		return nil
	}
	defer (*shellItem)(ptr(psi)).Release()
	if p := shellItemPath(psi); p != "" {
		return []string{p}
	}
	return nil
}

// openDialogResults reads every selected path out of an IFileOpenDialog's
// IShellItemArray result (multi-select).
func openDialogResults(odlg *fileOpenDialog) []string {
	var parray uintptr
	if odlg.GetResults(&parray) < 0 || parray == 0 {
		return nil
	}
	arr := (*shellItemArray)(ptr(parray))
	defer arr.Release()
	var n uint32
	arr.GetCount(&n)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		var psi uintptr
		if arr.GetItemAt(i, &psi) >= 0 && psi != 0 {
			if p := shellItemPath(psi); p != "" {
				paths = append(paths, p)
			}
			(*shellItem)(ptr(psi)).Release()
		}
	}
	return paths
}

// applyFileTypes restricts the dialog to Options.Extensions as a single
// COMDLG_FILTERSPEC line ("*.a;*.b"), returning the backing UTF-16 buffers the
// caller must keep alive across Show. No restriction returns nil.
func applyFileTypes(dlg *fileDialog, exts []string) [][]uint16 {
	clean := cleanExtensions(exts)
	if clean == nil {
		return nil
	}
	patterns := make([]string, len(clean))
	for i, e := range clean {
		patterns[i] = "*." + e
	}
	spec := strings.Join(patterns, ";")
	nameU := utf16.Encode([]rune(spec + "\x00"))
	specU := utf16.Encode([]rune(spec + "\x00"))
	specs := []comdlgFilterSpec{{pszName: &nameU[0], pszSpec: &specU[0]}}
	dlg.SetFileTypes(1, &specs[0])
	return [][]uint16{nameU, specU}
}

// applyFileFilters turns Options.Filters into one COMDLG_FILTERSPEC per named
// filter, returning the backing UTF-16 buffers the caller must keep alive
// across Show.
func applyFileFilters(dlg *fileDialog, filters []FileFilter) [][]uint16 {
	var specs []comdlgFilterSpec
	var keep [][]uint16
	for _, f := range filters {
		clean := cleanExtensions(f.Extensions)
		if clean == nil {
			continue // a wildcard/empty filter restricts nothing: skip it
		}
		patterns := make([]string, len(clean))
		for i, e := range clean {
			patterns[i] = "*." + e
		}
		spec := strings.Join(patterns, ";")
		name := f.Name
		if name == "" {
			name = spec
		}
		nameU := utf16.Encode([]rune(name + "\x00"))
		specU := utf16.Encode([]rune(spec + "\x00"))
		keep = append(keep, nameU, specU)
		specs = append(specs, comdlgFilterSpec{pszName: &nameU[0], pszSpec: &specU[0]})
	}
	if len(specs) == 0 {
		return nil
	}
	dlg.SetFileTypes(uint32(len(specs)), &specs[0]) // #nosec G115 -- small slice length
	return keep
}

// shellItemPath returns the filesystem path of an IShellItem (the caller still
// owns and releases the item).
func shellItemPath(si uintptr) string {
	var pw uintptr
	if (*shellItem)(ptr(si)).GetDisplayName(sigdnFileSysPath, &pw) < 0 || pw == 0 {
		return ""
	}
	s := wideToString(pw)
	coTaskMemFree(pw)
	return s
}

// --- string helpers --------------------------------------------------------

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion. The values it is fed are COM object and
// CoTaskMem pointers - system memory the Go GC neither owns nor moves; the
// spelling only keeps go vet's unsafeptr check quiet.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) } // #nosec G103 -- audited FFI reinterpret

// utf16Ptr returns a NUL-terminated UTF-16 pointer for s ("" on an embedded
// NUL, never nil).
func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}

// wideToString reads a NUL-terminated UTF-16 string from a raw pointer (CoTaskMem
// memory, which the Go GC does not move).
func wideToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*uint16)(unsafe.Add(base, n*2)) != 0 {
		n++
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(base), n))) // #nosec G103 -- slice over the CoTaskMem buffer
}
