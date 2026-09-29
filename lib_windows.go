// Windows View engine over WebView2/COM.
//
// The engine creates the WebView2 environment and controller asynchronously
// through completion-handler COM objects implemented in Go, wires
// add_WebMessageReceived + AddScriptToExecuteOnDocumentCreated, and offers
// Navigate/loadHTML/Eval/Bind over the same wire-compatible JS bridge as the
// macOS/Linux backends.
//
// COM idiom (outbound): each interface is a `struct{ vtbl *...Vtbl }`; the
// vtbl is a struct of uintptr slots in exact IDL order; a method call is
// purego.SyscallN(i.vtbl.Method, this, args...). Inbound handler objects we
// implement use a Go-built vtable {QueryInterface, AddRef, Release, Invoke}
// of purego.NewCallback pointers; the objects live in package-global memory
// (kept alive, and Go's GC is non-moving) so the pointers handed to WebView2
// stay valid across the async creation window.

package tuohi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

var errNoWindow = errors.New("webview2: failed to create window")

// bridgePostFn for the Windows WebView2 backend: the chrome.webview channel
// (WebKit's messageHandlers used on macOS/Linux do not exist here).
const bridgePostFn = `function(message) {
  return window.chrome.webview.postMessage(message);
}`

// dbg prints spike diagnostics to stderr when WEBVIEW2_DEBUG is set (the
// headless CI self-test asserts on stdout, so stderr stays out of the way).
var debugEnabled = os.Getenv("WEBVIEW2_DEBUG") != ""

func dbg(format string, a ...any) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "[webview2] "+format+"\n", a...)
	}
}

// dbgURL shortens a URL for a debug line: a data: URL can be the whole page.
func dbgURL(u string) string {
	if len(u) > 48 {
		return u[:48] + "..."
	}
	return u
}

// ptr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion (keeps go vet happy).
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// --- GUID / IID ------------------------------------------------------------

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func guidEqual(a, b *guid) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && a.Data4 == b.Data4
}

var (
	iidIUnknown             = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidEnvironmentComplete  = guid{0x4E8A3389, 0xC9D8, 0x4BD2, [8]byte{0xB6, 0xB5, 0x12, 0x4F, 0xEE, 0x6C, 0xC1, 0x4D}}
	iidControllerComplete   = guid{0x6C4819F3, 0xC9B7, 0x4260, [8]byte{0x81, 0x27, 0xC9, 0xF5, 0xBD, 0xE7, 0xF6, 0x8C}}
	iidController2          = guid{0xF0EC8882, 0x7EC5, 0x4118, [8]byte{0xA7, 0xC8, 0x59, 0x54, 0x0C, 0xFF, 0x17, 0xED}}
	iidMessageReceived      = guid{0x57213F19, 0x00E6, 0x49FA, [8]byte{0x8E, 0x07, 0x89, 0x8E, 0xA0, 0x1E, 0xCB, 0xD2}}
	iidScriptAdded          = guid{0xB99369F3, 0x9B11, 0x47B5, [8]byte{0xBC, 0x6F, 0x8E, 0x78, 0x95, 0xFC, 0xEA, 0x17}}
	iidWebResourceRequested = guid{0xAB00B74C, 0x15F1, 0x4646, [8]byte{0x80, 0xE8, 0xE7, 0x63, 0x41, 0xD2, 0x5D, 0x71}}
	// ICoreWebView2NavigationCompletedEventHandler (View.Ready).
	iidNavigationCompleted = guid{0xD33A35BF, 0x1C49, 0x4F98, [8]byte{0x93, 0xAB, 0x00, 0x6E, 0x05, 0x33, 0xFE, 0x1C}}
	// ICoreWebView2NavigationStartingEventHandler and
	// ICoreWebView2NewWindowRequestedEventHandler (the navigation policy).
	iidNavigationStarting = guid{0x9ADBE429, 0xF36D, 0x432B, [8]byte{0x9D, 0xDC, 0xF8, 0x88, 0x1F, 0xBD, 0x76, 0xE3}}
	iidNewWindowRequested = guid{0xD4C185FE, 0xC81C, 0x4989, [8]byte{0x97, 0xAF, 0x2D, 0x3F, 0xA7, 0xAB, 0x56, 0x51}}
	iidContentLoading     = guid{0x364471E7, 0xF2BE, 0x4910, [8]byte{0xBD, 0xBA, 0xD7, 0x20, 0x77, 0xD5, 0x1C, 0x4B}}
	// ICoreWebView2PermissionRequestedEventHandler
	iidPermissionRequested = guid{0x15E1C6A3, 0xC72A, 0x4DF3, [8]byte{0x91, 0xD7, 0xD0, 0x97, 0xFB, 0xEC, 0x6B, 0xFD}}

	// The ICoreWebView2Settings extension interfaces. Values come verbatim from
	// Microsoft's WebView2.idl: the Base settings object implements all of them
	// on a current Evergreen Runtime, so each is fetched with a QueryInterface
	// on the base interface and, if the running runtime predates it, the QI
	// returns E_NOINTERFACE and the property is left at its native default.
	iidSettings3 = guid{0xFDB5AB74, 0xAF33, 0x4854, [8]byte{0x84, 0xF0, 0x0A, 0x63, 0x1D, 0xEB, 0x5E, 0xBA}}
	iidSettings4 = guid{0xCB56846C, 0x4168, 0x4D53, [8]byte{0xB0, 0x4F, 0x03, 0xB6, 0xD6, 0x79, 0x6F, 0xF2}}
	iidSettings5 = guid{0x183E7052, 0x1D03, 0x43A0, [8]byte{0xAB, 0x99, 0x98, 0xE0, 0x43, 0xB6, 0x6B, 0x39}}
	iidSettings6 = guid{0x11CB3ACD, 0x9BC8, 0x43B8, [8]byte{0x83, 0xBF, 0xF4, 0x07, 0x53, 0x71, 0x4F, 0x87}}
	iidSettings7 = guid{0x488DC902, 0x35EF, 0x42D2, [8]byte{0xBC, 0x7D, 0x94, 0xB6, 0x5C, 0x4B, 0xC4, 0x9C}}
	iidSettings8 = guid{0x9E6B0E8F, 0x86AD, 0x4E81, [8]byte{0x81, 0x47, 0xA9, 0xB5, 0xED, 0xB6, 0x86, 0x50}}
	iidSettings9 = guid{0x0528A73B, 0xE92D, 0x49F4, [8]byte{0x92, 0x7A, 0xE5, 0x47, 0xDD, 0xDA, 0xA3, 0x7D}}
)

// --- COM vtable layouts (exact IDL order; uintptr per slot) ----------------

type unknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type coreWebView2EnvironmentVtbl struct {
	unknownVtbl
	CreateCoreWebView2Controller  uintptr
	CreateWebResourceResponse     uintptr
	GetBrowserVersionString       uintptr
	AddNewBrowserVersionAvailable uintptr
	RemoveNewBrowserVersionAvail  uintptr
}

type coreWebView2ControllerVtbl struct {
	unknownVtbl
	GetIsVisible                   uintptr
	PutIsVisible                   uintptr
	GetBounds                      uintptr
	PutBounds                      uintptr
	GetZoomFactor                  uintptr
	PutZoomFactor                  uintptr
	AddZoomFactorChanged           uintptr
	RemoveZoomFactorChanged        uintptr
	SetBoundsAndZoomFactor         uintptr
	MoveFocus                      uintptr
	AddMoveFocusRequested          uintptr
	RemoveMoveFocusRequested       uintptr
	AddGotFocus                    uintptr
	RemoveGotFocus                 uintptr
	AddLostFocus                   uintptr
	RemoveLostFocus                uintptr
	AddAcceleratorKeyPressed       uintptr
	RemoveAcceleratorKeyPressed    uintptr
	GetParentWindow                uintptr
	PutParentWindow                uintptr
	NotifyParentWindowPositionChng uintptr
	Close                          uintptr
	GetCoreWebView2                uintptr
}

// coreWebView2Controller2Vtbl is the controller with the two
// DefaultBackgroundColor accessors appended (ICoreWebView2Controller2's only
// addition to ICoreWebView2Controller). The slot offsets follow this file's
// base controller layout above.
type coreWebView2Controller2Vtbl struct {
	coreWebView2ControllerVtbl
	GetDefaultBackgroundColor uintptr
	PutDefaultBackgroundColor uintptr
}

type coreWebView2Vtbl struct {
	unknownVtbl
	GetSettings                         uintptr
	GetSource                           uintptr
	Navigate                            uintptr
	NavigateToString                    uintptr
	AddNavigationStarting               uintptr
	RemoveNavigationStarting            uintptr
	AddContentLoading                   uintptr
	RemoveContentLoading                uintptr
	AddSourceChanged                    uintptr
	RemoveSourceChanged                 uintptr
	AddHistoryChanged                   uintptr
	RemoveHistoryChanged                uintptr
	AddNavigationCompleted              uintptr
	RemoveNavigationCompleted           uintptr
	AddFrameNavigationStarting          uintptr
	RemoveFrameNavigationStarting       uintptr
	AddFrameNavigationCompleted         uintptr
	RemoveFrameNavigationCompleted      uintptr
	AddScriptDialogOpening              uintptr
	RemoveScriptDialogOpening           uintptr
	AddPermissionRequested              uintptr
	RemovePermissionRequested           uintptr
	AddProcessFailed                    uintptr
	RemoveProcessFailed                 uintptr
	AddScriptToExecuteOnDocumentCreated uintptr
	RemoveScriptToExecuteOnDocCreated   uintptr
	ExecuteScript                       uintptr
	CapturePreview                      uintptr
	Reload                              uintptr
	PostWebMessageAsJSON                uintptr
	PostWebMessageAsString              uintptr
	AddWebMessageReceived               uintptr
	RemoveWebMessageReceived            uintptr
	// The interface continues; we declare through AddWebResourceRequestedFilter
	// (needed for custom-scheme serving) so its vtbl offset is correct. Later
	// methods are omitted; none are called.
	CallDevToolsProtocolMethod             uintptr
	GetBrowserProcessID                    uintptr
	GetCanGoBack                           uintptr
	GetCanGoForward                        uintptr
	GoBack                                 uintptr
	GoForward                              uintptr
	GetDevToolsProtocolEventReceiver       uintptr
	Stop                                   uintptr
	AddNewWindowRequested                  uintptr
	RemoveNewWindowRequested               uintptr
	AddDocumentTitleChanged                uintptr
	RemoveDocumentTitleChanged             uintptr
	GetDocumentTitle                       uintptr
	AddHostObjectToScript                  uintptr
	RemoveHostObjectFromScript             uintptr
	OpenDevToolsWindow                     uintptr
	AddContainsFullScreenElementChanged    uintptr
	RemoveContainsFullScreenElementChanged uintptr
	GetContainsFullScreenElement           uintptr
	AddWebResourceRequested                uintptr
	RemoveWebResourceRequested             uintptr
	AddWebResourceRequestedFilter          uintptr
	RemoveWebResourceRequestedFilter       uintptr
}

// coreWebView2SettingsVtbl is the full Base ICoreWebView2Settings surface:
// nine get/put pairs in exact IDL order, appended after IUnknown's three
// inherited slots. ICoreWebView2Settings2..9 each add one or more pairs behind
// this base, so their vtbl structs embed this one and extend it.
type coreWebView2SettingsVtbl struct {
	unknownVtbl
	GetIsScriptEnabled                uintptr
	PutIsScriptEnabled                uintptr
	GetIsWebMessageEnabled            uintptr
	PutIsWebMessageEnabled            uintptr
	GetAreDefaultScriptDialogsEnabled uintptr
	PutAreDefaultScriptDialogsEnabled uintptr
	GetIsStatusBarEnabled             uintptr
	PutIsStatusBarEnabled             uintptr
	GetDevTools                       uintptr
	PutDevTools                       uintptr
	GetAreDefaultContextMenusEnabled  uintptr
	PutAreDefaultContextMenusEnabled  uintptr
	GetAreHostObjectsAllowed          uintptr
	PutAreHostObjectsAllowed          uintptr
	GetIsZoomControlEnabled           uintptr
	PutIsZoomControlEnabled           uintptr
	GetIsBuiltInErrorPageEnabled      uintptr
	PutIsBuiltInErrorPageEnabled      uintptr
}

// The ICoreWebView2Settings extension interfaces each extend the previous one
// and own a handful of get/put pairs. They are listed here with the exact IDL
// order of their own pairs so per-interface vtable indexing stays correct.
// The IID counterparts live with the per-interface vtbl structs below and are
// obtained from the Base settings object via QueryInterface (see settingsN()).
type coreWebView2Settings2Vtbl struct { // ICoreWebView2Settings2 : Base (UserAgent)
	coreWebView2SettingsVtbl
	GetUserAgent uintptr
	PutUserAgent uintptr
}
type coreWebView2Settings3Vtbl struct { // ICoreWebView2Settings3 : Settings2 (AreBrowserAcceleratorKeysEnabled)
	coreWebView2Settings2Vtbl
	GetAreBrowserAcceleratorKeysEnabled uintptr
	PutAreBrowserAcceleratorKeysEnabled uintptr
}
type coreWebView2Settings4Vtbl struct { // ICoreWebView2Settings4 : Settings3 (IsPasswordAutosaveEnabled, IsGeneralAutofillEnabled)
	coreWebView2Settings3Vtbl
	GetIsPasswordAutosaveEnabled uintptr
	PutIsPasswordAutosaveEnabled uintptr
	GetIsGeneralAutofillEnabled  uintptr
	PutIsGeneralAutofillEnabled  uintptr
}
type coreWebView2Settings5Vtbl struct { // ICoreWebView2Settings5 : Settings4 (IsPinchZoomEnabled)
	coreWebView2Settings4Vtbl
	GetIsPinchZoomEnabled uintptr
	PutIsPinchZoomEnabled uintptr
}
type coreWebView2Settings6Vtbl struct { // ICoreWebView2Settings6 : Settings5 (IsSwipeNavigationEnabled)
	coreWebView2Settings5Vtbl
	GetIsSwipeNavigationEnabled uintptr
	PutIsSwipeNavigationEnabled uintptr
}
type coreWebView2Settings7Vtbl struct { // ICoreWebView2Settings7 : Settings6 (HiddenPdfToolbarItems COREWEBVIEW2_PDF_TOOLBAR_ITEMS bitmask)
	coreWebView2Settings6Vtbl
	GetHiddenPdfToolbarItems uintptr
	PutHiddenPdfToolbarItems uintptr
}
type coreWebView2Settings8Vtbl struct { // ICoreWebView2Settings8 : Settings7 (IsReputationCheckingRequired)
	coreWebView2Settings7Vtbl
	GetIsReputationCheckingRequired uintptr
	PutIsReputationCheckingRequired uintptr
}
type coreWebView2Settings9Vtbl struct { // ICoreWebView2Settings9 : Settings8 (IsNonClientRegionSupportEnabled)
	coreWebView2Settings8Vtbl
	GetIsNonClientRegionSupportEnabled uintptr
	PutIsNonClientRegionSupportEnabled uintptr
}

type messageArgsVtbl struct {
	unknownVtbl
	GetSource             uintptr
	GetWebMessageAsJSON   uintptr
	TryGetWebMessageAsStr uintptr
}

// Interface pointer wrappers (the vtbl pointer is the object's first field).
type environment struct{ vtbl *coreWebView2EnvironmentVtbl }
type controller struct{ vtbl *coreWebView2ControllerVtbl }
type controller2 struct{ vtbl *coreWebView2Controller2Vtbl }
type coreWebView2 struct{ vtbl *coreWebView2Vtbl }
type settings struct{ vtbl *coreWebView2SettingsVtbl }
type settings3i struct{ vtbl *coreWebView2Settings3Vtbl }
type settings4i struct{ vtbl *coreWebView2Settings4Vtbl }
type settings5i struct{ vtbl *coreWebView2Settings5Vtbl }
type settings6i struct{ vtbl *coreWebView2Settings6Vtbl }
type settings7i struct{ vtbl *coreWebView2Settings7Vtbl }
type settings8i struct{ vtbl *coreWebView2Settings8Vtbl }
type settings9i struct{ vtbl *coreWebView2Settings9Vtbl }
type messageArgs struct {
	vtbl *messageArgsVtbl
}

// navigationCompletedArgsVtbl mirrors ICoreWebView2NavigationCompletedEventArgs:
// IUnknown's three slots plus get_IsSuccess. Only IsSuccess is read (Ready
// fires on any completed top-level navigation).
type navigationCompletedArgsVtbl struct {
	unknownVtbl
	GetIsSuccess uintptr
}

// contentLoadingArgsVtbl mirrors ICoreWebView2ContentLoadingEventArgs in IDL
// order: get_IsErrorPage, get_NavigationId.
type contentLoadingArgsVtbl struct {
	unknownVtbl
	GetIsErrorPage  uintptr
	GetNavigationID uintptr
}

type contentLoadingArgs struct {
	vtbl *contentLoadingArgsVtbl
}

func asContentLoadingArgs(p uintptr) *contentLoadingArgs {
	return (*contentLoadingArgs)(ptr(p))
}

// NavigationID returns the ID NavigationStarting gave the navigation whose
// document is loading, and false when WebView2 cannot say.
func (i *contentLoadingArgs) NavigationID() (uint64, bool) {
	var id uint64
	r, _, _ := purego.SyscallN(i.vtbl.GetNavigationID, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&id)))
	return id, int32(r) >= 0
}

// IsErrorPage reports whether the document is WebView2's own error page, and
// true when WebView2 cannot say.
func (i *contentLoadingArgs) IsErrorPage() bool {
	var v int32
	r, _, _ := purego.SyscallN(i.vtbl.GetIsErrorPage, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&v)))
	return int32(r) < 0 || v != 0
}

type navigationCompletedArgs struct {
	vtbl *navigationCompletedArgsVtbl
}

// navigationStartingArgsVtbl mirrors ICoreWebView2NavigationStartingEventArgs
// in IDL order: get_Uri, get_IsUserInitiated, get_IsRedirected,
// get_RequestHeaders, get_Cancel, put_Cancel, get_NavigationId. Only the URI
// and put_Cancel are used.
type navigationStartingArgsVtbl struct {
	unknownVtbl
	GetUri             uintptr
	GetIsUserInitiated uintptr
	GetIsRedirected    uintptr
	GetRequestHeaders  uintptr
	GetCancel          uintptr
	PutCancel          uintptr
	GetNavigationID    uintptr
}

type navigationStartingArgs struct {
	vtbl *navigationStartingArgsVtbl
}

func asNavigationStartingArgs(p uintptr) *navigationStartingArgs {
	return (*navigationStartingArgs)(ptr(p))
}

// URI returns the navigation's target, or "" when WebView2 cannot say.
func (i *navigationStartingArgs) URI() string {
	return comString(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)))
}

// NavigationID returns the navigation's ID, shared by its redirects and its
// NavigationCompleted, and false when WebView2 cannot say.
func (i *navigationStartingArgs) NavigationID() (uint64, bool) {
	var id uint64
	r, _, _ := purego.SyscallN(i.vtbl.GetNavigationID, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&id)))
	return id, int32(r) >= 0
}

func (i *navigationStartingArgs) Cancel() {
	purego.SyscallN(i.vtbl.PutCancel, uintptr(unsafe.Pointer(i)), 1)
}

// newWindowRequestedArgsVtbl mirrors ICoreWebView2NewWindowRequestedEventArgs
// in IDL order: get_Uri, put_NewWindow, get_NewWindow, put_Handled,
// get_Handled, get_IsUserInitiated, GetDeferral, get_WindowFeatures. Only the
// URI and put_Handled are used.
type newWindowRequestedArgsVtbl struct {
	unknownVtbl
	GetUri             uintptr
	PutNewWindow       uintptr
	GetNewWindow       uintptr
	PutHandled         uintptr
	GetHandled         uintptr
	GetIsUserInitiated uintptr
	GetDeferral        uintptr
	GetWindowFeatures  uintptr
}

type newWindowRequestedArgs struct {
	vtbl *newWindowRequestedArgsVtbl
}

func asNewWindowRequestedArgs(p uintptr) *newWindowRequestedArgs {
	return (*newWindowRequestedArgs)(ptr(p))
}

// URI returns the new window's target, or "" when WebView2 cannot say.
func (i *newWindowRequestedArgs) URI() string {
	return comString(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)))
}

// Handle marks the request handled, so WebView2 opens no popup window.
func (i *newWindowRequestedArgs) Handle() {
	purego.SyscallN(i.vtbl.PutHandled, uintptr(unsafe.Pointer(i)), 1)
}

// permissionRequestedArgsVtbl mirrors
// ICoreWebView2PermissionRequestedEventArgs in IDL order: get_Uri,
// get_PermissionKind, get_IsUserInitiated, get_State, put_State, GetDeferral.
type permissionRequestedArgsVtbl struct {
	unknownVtbl
	GetUri             uintptr
	GetPermissionKind  uintptr
	GetIsUserInitiated uintptr
	GetState           uintptr
	PutState           uintptr
	GetDeferral        uintptr
}

type permissionRequestedArgs struct {
	vtbl *permissionRequestedArgsVtbl
}

func asPermissionRequestedArgs(p uintptr) *permissionRequestedArgs {
	return (*permissionRequestedArgs)(ptr(p))
}

// COREWEBVIEW2_PERMISSION_KIND values tuohi decides by View.Permissions, and
// the COREWEBVIEW2_PERMISSION_STATE it answers every request with.
const (
	permissionKindMicrophone    = 1
	permissionKindCamera        = 2
	permissionKindClipboardRead = 6

	permissionStateAllow = 1
	permissionStateDeny  = 2
)

// URI returns the origin of the content asking, a frame's own when a frame
// asks, or "" when WebView2 cannot say.
func (i *permissionRequestedArgs) URI() string {
	return comString(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)))
}

// Kind returns the COREWEBVIEW2_PERMISSION_KIND asked for, or -1.
func (i *permissionRequestedArgs) Kind() int {
	var k int32 = -1
	r, _, _ := purego.SyscallN(i.vtbl.GetPermissionKind, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&k)))
	if int32(r) < 0 {
		return -1
	}
	return int(k)
}

// SetState answers the request, so WebView2 shows no prompt of its own.
func (i *permissionRequestedArgs) SetState(state int) {
	purego.SyscallN(i.vtbl.PutState, uintptr(unsafe.Pointer(i)), uintptr(state))
}

// comString calls a COM getter that returns an LPWSTR the caller frees with
// CoTaskMemFree, and returns it as a Go string, or "" on failure.
func comString(getter, this uintptr) string {
	var p uintptr
	r, _, _ := purego.SyscallN(getter, this, uintptr(unsafe.Pointer(&p)))
	if int32(r) < 0 || p == 0 {
		return ""
	}
	defer coTaskMemFree(p)
	return wideToString(p)
}

func asNavigationCompletedArgs(p uintptr) *navigationCompletedArgs {
	return (*navigationCompletedArgs)(ptr(p))
}

func (i *navigationCompletedArgs) IsSuccess() bool {
	var ok int32
	purego.SyscallN(i.vtbl.GetIsSuccess, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&ok)))
	return ok != 0
}

func asEnvironment(p uintptr) *environment { return (*environment)(ptr(p)) }
func asController(p uintptr) *controller   { return (*controller)(ptr(p)) }
func asController2(p uintptr) *controller2 { return (*controller2)(ptr(p)) }
func asWebView2(p uintptr) *coreWebView2   { return (*coreWebView2)(ptr(p)) }
func asSettings(p uintptr) *settings       { return (*settings)(ptr(p)) }

// asSettingsSafeRelease lets the Settings2..9 interfaces be released through
// the same code path as the Base interface. Every ICoreWebView2Settings*
// vtbl begins with IUnknown's three inherited slots (QueryInterface, AddRef,
// Release) at identical offsets; asSettings(p).Release() reads only that third
// slot, so it is layout-safe for a pointer obtained from settingsQI no matter
// which extension interface it actually is.
func asSettingsSafeRelease(p uintptr) *settings { return asSettings(p) }
func asSettings3(p uintptr) *settings3i         { return (*settings3i)(ptr(p)) }
func asSettings4(p uintptr) *settings4i         { return (*settings4i)(ptr(p)) }
func asSettings5(p uintptr) *settings5i         { return (*settings5i)(ptr(p)) }
func asSettings6(p uintptr) *settings6i         { return (*settings6i)(ptr(p)) }
func asSettings7(p uintptr) *settings7i         { return (*settings7i)(ptr(p)) }
func asSettings8(p uintptr) *settings8i         { return (*settings8i)(ptr(p)) }
func asSettings9(p uintptr) *settings9i         { return (*settings9i)(ptr(p)) }
func asMessageArgs(p uintptr) *messageArgs      { return (*messageArgs)(ptr(p)) }

// settingsQI asks the Base settings object for its ICoreWebView2Settings2..9
// view. It returns the AddRef'd interface pointer, or 0 (with the interface
// unreferenced) when the running WebView2 Runtime predates that interface and
// answers E_NOINTERFACE - the same tolerance the applyDefaultBackgroundColor
// path uses for ICoreWebView2Controller2. The caller must Release the pointer.
func (i *settings) settingsQI(iid *guid) uintptr {
	var out uintptr
	hr := i.QueryInterface(iid, &out)
	if out == 0 || int32(hr) < 0 {
		if uint32(hr) != 0x80004002 /* E_NOINTERFACE */ {
			dbg("settingsQI(iid=%08x) hr=0x%08x", iid.Data1, uint32(hr))
		}
		return 0
	}
	return out
}
func (i *settings) QueryInterface(riid *guid, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.QueryInterface, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(riid)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *settings) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *environment) CreateController(hwnd, handler uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateCoreWebView2Controller, uintptr(unsafe.Pointer(i)), hwnd, handler)
	return r
}

// AddRef/Release are the environment's IUnknown lifetime methods. The
// environment is used long after its creation callback returns - at request
// time, by CreateWebResourceResponse for custom schemes - so a reference is
// held for the life of the webview (taken in handlerInvoke, dropped in Destroy)
// rather than relying on the callback's transient one.
func (i *environment) AddRef()  { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }
func (i *environment) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }
func (i *controller) GetCoreWebView2(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetCoreWebView2, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *controller) QueryInterface(riid *guid, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.QueryInterface, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(riid)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *controller) PutIsVisible(v bool) {
	purego.SyscallN(i.vtbl.PutIsVisible, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// moveFocusReasonProgrammatic is COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC:
// focus moved by the host, not by a Tab key. The reason is a plain enum (a
// scalar), so MoveFocus is not arch-specific the way putBounds is.
const moveFocusReasonProgrammatic = 0

// MoveFocus pushes keyboard focus into the hosted WebView2 content. Without it,
// the content stays unfocused until the user clicks the page - a keyboard and
// screen-reader accessibility gap, since focus on the host HWND does not reach
// the WebView2 child HWND on its own.
func (i *controller) MoveFocus(reason uintptr) {
	purego.SyscallN(i.vtbl.MoveFocus, uintptr(unsafe.Pointer(i)), reason)
}

// getBounds reads the controller's current bounds. Unlike putBounds (RECT by
// value, arch-specific), the getter takes a RECT* out-param, so one signature
// serves both arches. Used by the embed regression test to assert the bounds
// follow the host window.
func (i *controller) getBounds(r *rect) {
	purego.SyscallN(i.vtbl.GetBounds, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(r)))
}

// AddRef/Close/Release are the controller's IUnknown/lifetime methods.
// (putBounds is arch-specific; see putbounds_amd64.go and putbounds_arm64.go.)
func (i *controller) AddRef()  { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }
func (i *controller) Close()   { purego.SyscallN(i.vtbl.Close, uintptr(unsafe.Pointer(i))) }
func (i *controller) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

// PutDefaultBackgroundColor sets the color - alpha included - painted behind
// the page. COREWEBVIEW2_COLOR is a {A, R, G, B} byte struct passed by value,
// which travels as a little-endian uint32 (A in the low byte). It returns the
// HRESULT so callers can detect E_INVALIDARG / failures (transparency).
func (i *controller2) PutDefaultBackgroundColor(color uint32) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.PutDefaultBackgroundColor, uintptr(unsafe.Pointer(i)), uintptr(color))
	return r
}

func (i *controller2) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *coreWebView2) GetSettings(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetSettings, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func (i *coreWebView2) Navigate(url *uint16) {
	purego.SyscallN(i.vtbl.Navigate, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(url)))
}
func (i *coreWebView2) NavigateToString(html *uint16) {
	purego.SyscallN(i.vtbl.NavigateToString, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(html)))
}
func (i *coreWebView2) ExecuteScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.ExecuteScript, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *coreWebView2) AddScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.AddScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *coreWebView2) RemoveScript(id *uint16) {
	purego.SyscallN(i.vtbl.RemoveScriptToExecuteOnDocCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(id)))
}
func (i *coreWebView2) Release() {
	purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i)))
}
func (i *coreWebView2) AddWebMessageReceived(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebMessageReceived, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddNavigationStarting(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddNavigationStarting, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddContentLoading(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddContentLoading, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddNewWindowRequested(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddNewWindowRequested, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddPermissionRequested(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddPermissionRequested, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddNavigationCompleted(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddNavigationCompleted, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddWebResourceRequested(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebResourceRequested, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddWebResourceRequestedFilter(uri *uint16, ctx uint32) {
	purego.SyscallN(i.vtbl.AddWebResourceRequestedFilter, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(uri)), uintptr(ctx))
}
func (i *coreWebView2) AddRef() { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }

// The put_* accessors below apply each setting through the interface that owns
// its offset. Base (ICoreWebView2Settings) properties live on *settings; the
// Settings2..9 additions live on their own wrapper type so the correct
// embedded-vtbl slot is selected at the call site. Each takes its value as a
// bool, except HiddenPdfToolbarItems (a COREWEBVIEW2_PDF_TOOLBAR_ITEMS bitmask
// u32) and UserAgent (an LPCWSTR NUL-terminated string).

// ---- Base (ICoreWebView2Settings) ----
func (i *settings) PutIsScriptEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsScriptEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutIsWebMessageEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsWebMessageEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutAreDefaultScriptDialogsEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreDefaultScriptDialogsEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutIsStatusBarEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsStatusBarEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutDevTools(v bool) {
	purego.SyscallN(i.vtbl.PutDevTools, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutAreDefaultContextMenusEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreDefaultContextMenusEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutAreHostObjectsAllowed(v bool) {
	purego.SyscallN(i.vtbl.PutAreHostObjectsAllowed, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutIsZoomControlEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsZoomControlEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings) PutIsBuiltInErrorPageEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsBuiltInErrorPageEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings3 ----
func (i *settings3i) PutAreBrowserAcceleratorKeysEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreBrowserAcceleratorKeysEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings4 ----
func (i *settings4i) PutIsPasswordAutosaveEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsPasswordAutosaveEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}
func (i *settings4i) PutIsGeneralAutofillEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsGeneralAutofillEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings5 ----
func (i *settings5i) PutIsPinchZoomEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsPinchZoomEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings6 ----
func (i *settings6i) PutIsSwipeNavigationEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsSwipeNavigationEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings7 (COREWEBVIEW2_PDF_TOOLBAR_ITEMS bitmask u32) ----
func (i *settings7i) PutHiddenPdfToolbarItems(mask uint32) {
	purego.SyscallN(i.vtbl.PutHiddenPdfToolbarItems, uintptr(unsafe.Pointer(i)), uintptr(mask))
}

// ---- ICoreWebView2Settings8 ----
func (i *settings8i) PutIsReputationCheckingRequired(v bool) {
	purego.SyscallN(i.vtbl.PutIsReputationCheckingRequired, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// ---- ICoreWebView2Settings9 ----
func (i *settings9i) PutIsNonClientRegionSupportEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsNonClientRegionSupportEnabled, uintptr(unsafe.Pointer(i)), boolToUintptr(v))
}

// GetSource returns the URI of the document that posted the message.
func (i *messageArgs) GetSource(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetSource, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *messageArgs) TryGetWebMessageAsString(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.TryGetWebMessageAsStr, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func boolToUintptr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// --- inbound COM handler objects we implement ------------------------------

const (
	kindEnv = iota
	kindController
	kindMessage
	kindScript
	kindWebResourceRequested
	kindNavigationCompleted
	kindNavigationStarting
	kindNewWindowRequested
	kindContentLoading
	kindPermissionRequested
)

type comHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

// comHandler is a COM object we hand to WebView2. Its first field MUST be the
// vtbl pointer. Instances are kept alive in handlerKeepAlive (Go's GC is
// non-moving, so the address stays valid for WebView2).
type comHandler struct {
	vtbl     *comHandlerVtbl
	iid      *guid
	engineID uintptr
	kind     int
	refCount int32
}

var (
	sharedHandlerVtbl *comHandlerVtbl
	handlerMu         sync.Mutex
	handlerKeepAlive  []*comHandler
)

func newHandler(engineID uintptr, kind int, iid *guid) *comHandler {
	h := &comHandler{vtbl: sharedHandlerVtbl, iid: iid, engineID: engineID, kind: kind, refCount: 1}
	handlerMu.Lock()
	handlerKeepAlive = append(handlerKeepAlive, h)
	handlerMu.Unlock()
	return h
}

func handlerPtr(h *comHandler) uintptr     { return uintptr(unsafe.Pointer(h)) }
func handlerFrom(this uintptr) *comHandler { return (*comHandler)(ptr(this)) }

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return 0x80004003 // E_POINTER
	}
	h := handlerFrom(this)
	want := (*guid)(ptr(riid))
	if guidEqual(want, h.iid) || guidEqual(want, &iidIUnknown) {
		*(*uintptr)(ptr(ppv)) = this
		atomic.AddInt32(&h.refCount, 1)
		return 0 // S_OK
	}
	*(*uintptr)(ptr(ppv)) = 0
	return 0x80004002 // E_NOINTERFACE
}

func handlerAddRef(this uintptr) uintptr {
	h := handlerFrom(this)
	return uintptr(atomic.AddInt32(&h.refCount, 1))
}

func handlerRelease(this uintptr) uintptr {
	h := handlerFrom(this)
	// Never free: the object is owned by handlerKeepAlive for the app lifetime.
	n := atomic.AddInt32(&h.refCount, -1)
	if n < 1 {
		n = 1
	}
	return uintptr(n)
}

// handlerInvoke is the single Invoke for all handler kinds. The C signatures
// all reduce to (this, uintptr, uintptr) since every argument is pointer- or
// int-sized; we dispatch on the handler kind.
func handlerInvoke(this, a, b uintptr) uintptr {
	h := handlerFrom(this)
	dbg("invoke kind=%d a=0x%x b=0x%x", h.kind, a, b)
	w := lookupEngine(h.engineID)
	if w == nil {
		return 0
	}
	switch h.kind {
	case kindEnv:
		// Invoke(this, HRESULT res, ICoreWebView2Environment* env)
		if int32(a) >= 0 && b != 0 { // SUCCEEDED(res)
			// Hold our own reference: the environment is used later, at request
			// time, by CreateWebResourceResponse (custom schemes). Released in
			// Destroy. Matches the controller/webview2 references below.
			asEnvironment(b).AddRef()
			w.environment = b
			if int32(asEnvironment(b).CreateController(w.window, handlerPtr(w.ctrlH))) < 0 {
				w.ready = true // CreateController failed synchronously; unblock embed.
			}
		} else {
			w.ready = true // environment creation failed; unblock embed (controller stays 0).
		}
	case kindController:
		// Invoke(this, HRESULT res, ICoreWebView2Controller* controller)
		if int32(a) >= 0 && b != 0 {
			ctrl := asController(b)
			var wv uintptr
			ctrl.GetCoreWebView2(&wv)
			ctrl.AddRef()
			w.controller = b
			w.webview2 = wv
			if wv != 0 {
				cw := asWebView2(wv)
				cw.AddRef()
				var token uint64
				cw.AddWebMessageReceived(handlerPtr(w.msgH), &token)
				// Ready: NavigationCompleted fires on every finished top-level
				// navigation; the handler filters to the first success.
				w.navH = newHandler(w.id, kindNavigationCompleted, &iidNavigationCompleted)
				var navTok uint64
				cw.AddNavigationCompleted(handlerPtr(w.navH), &navTok)
				// The navigation policy: NavigationStarting fires for the
				// top-level document only (frames have their own event,
				// which is left alone), before any request is sent.
				w.navStartH = newHandler(w.id, kindNavigationStarting, &iidNavigationStarting)
				var navStartTok uint64
				cw.AddNavigationStarting(handlerPtr(w.navStartH), &navStartTok)
				w.contentH = newHandler(w.id, kindContentLoading, &iidContentLoading)
				var contentTok uint64
				cw.AddContentLoading(handlerPtr(w.contentH), &contentTok)
				w.newWinH = newHandler(w.id, kindNewWindowRequested, &iidNewWindowRequested)
				var newWinTok uint64
				cw.AddNewWindowRequested(handlerPtr(w.newWinH), &newWinTok)
				// Every permission a page asks for is answered here, so
				// WebView2 never shows its own prompt (see permissionRequested).
				w.permH = newHandler(w.id, kindPermissionRequested, &iidPermissionRequested)
				var permTok uint64
				cw.AddPermissionRequested(handlerPtr(w.permH), &permTok)
				// App-content serving: intercept the app scheme's https vhost
				// and answer from the app-scope resolver (serveSchemeWindows).
				if w.serve != nil {
					w.wrrH = newHandler(w.id, kindWebResourceRequested, &iidWebResourceRequested)
					var wrrTok uint64
					cw.AddWebResourceRequested(handlerPtr(w.wrrH), &wrrTok)
					cw.AddWebResourceRequestedFilter(utf16(appSchemeVHost+"/*"), 0) // 0 = ALL
				}
			}
		}
		w.ready = true
	case kindMessage:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2WebMessageReceivedEventArgs* args)
		if b != 0 {
			var pwstr uintptr
			args := asMessageArgs(b)
			if int32(args.TryGetWebMessageAsString(&pwstr)) >= 0 && pwstr != 0 {
				msg := wideToString(pwstr)
				coTaskMemFree(pwstr)
				// The sender is the document that posted, as the event
				// reports it. A failed read names no origin, and the gate
				// refuses it.
				sender := ""
				var psrc uintptr
				hr := args.GetSource(&psrc)
				if int32(hr) >= 0 && psrc != 0 {
					sender = wideToString(psrc)
					coTaskMemFree(psrc)
				}
				dbg("message: source hr=0x%x %q committed=%q", uint32(hr), dbgURL(sender), dbgURL(w.committedURI))
				// WebView2 names a data: document about:blank here (GitHub
				// run 36383745514), and so does the view's own get_Source
				// (run 36465210066). This event carries only the top-level
				// document's messages, frames having their own, so the
				// sender is the document the view last committed, whose URI
				// NavigationStarting named (see kindContentLoading). One
				// that has committed since may already name the next page,
				// as WebKitGTK's URI does; the token covers that, since only
				// the page it was given to can put it in front of a message.
				if sender == "about:blank" {
					sender = w.committedURI
				}
				w.onMessage(msg, sender, true)
			}
		}
	case kindNavigationCompleted:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2NavigationCompletedEventArgs* args)
		// Ready fires once per view on the first successfully completed
		// navigation.
		if b != 0 && asNavigationCompletedArgs(b).IsSuccess() {
			w.fireReady()
		}
	case kindContentLoading:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2ContentLoadingEventArgs* args)
		// A navigation's document has committed, and none of its scripts
		// has run yet, so none of its messages has arrived: messages come
		// before NavigationCompleted (GitHub run 36466440440). The URI
		// NavigationStarting recorded for it becomes the sender a message
		// from about:blank is read as. A navigation not recorded, or
		// WebView2's error page, clears it. Navigations can overlap, so
		// each is looked up by its ID; IDs rise, so every navigation that
		// started before this one is also dropped, having been replaced.
		if b != 0 {
			args := asContentLoadingArgs(b)
			id, ok := args.NavigationID()
			uri, recorded := w.pendingNavs[id]
			errorPage := args.IsErrorPage()
			w.committedURI = ""
			if ok && recorded && !errorPage {
				w.committedURI = uri
			}
			dbg("content loading: id=%d ok=%v recorded=%v errorPage=%v pending=%d committed=%q",
				id, ok, recorded, errorPage, len(w.pendingNavs), dbgURL(w.committedURI))
			if ok {
				for pending := range w.pendingNavs {
					if pending <= id {
						delete(w.pendingNavs, pending)
					}
				}
			}
		}
	case kindNavigationStarting:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2NavigationStartingEventArgs* args)
		// A navigation the policy does not let proceed is cancelled here,
		// before its request is sent, and the page stays as it was.
		if b != 0 {
			args := asNavigationStartingArgs(b)
			uri := args.URI()
			id, ok := args.NavigationID()
			dbg("navigation starting: id=%d ok=%v %q", id, ok, dbgURL(uri))
			if action := w.navigationPolicy(uri); action != navProceed {
				args.Cancel()
				refuseNavigation(uri, action)
				// A redirect the policy refuses ends a navigation recorded
				// when it started, and no document of its commits.
				if ok {
					delete(w.pendingNavs, id)
				}
			} else if ok {
				// A redirect keeps its navigation's ID, so the last URI
				// recorded is where the document came from.
				if w.pendingNavs == nil {
					w.pendingNavs = make(map[uint64]string)
				}
				w.pendingNavs[id] = uri
			}
		}
	case kindNewWindowRequested:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2NewWindowRequestedEventArgs* args)
		// WebView2 would open its own popup window, with none of this
		// view's scripts or handlers. Mark every request handled so it
		// never does, and let handleNewWindow decide what loads instead.
		if b != 0 {
			args := asNewWindowRequestedArgs(b)
			args.Handle()
			w.handleNewWindow(args.URI())
		}
	case kindPermissionRequested:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2PermissionRequestedEventArgs* args)
		if b != 0 {
			w.permissionRequested(asPermissionRequestedArgs(b))
		}
	case kindScript:
		// Invoke(this, HRESULT res, LPCWSTR id)
		if int32(a) >= 0 && b != 0 {
			w.lastScript = wideToString(b)
		}
		w.scriptDone = true
	case kindWebResourceRequested:
		// Invoke(this, ICoreWebView2* sender, ICoreWebView2WebResourceRequestedEventArgs* args)
		if b != 0 {
			w.serveSchemeWindows(b)
		}
	}
	return 0 // S_OK
}

// permissionRequested answers a page's permission request, on the UI thread.
// The microphone, the camera, and clipboard reads are decided by the view's
// policy (viewCore.permits) for the origin asking, which WebView2 gives per
// frame. Every other kind, such as geolocation or notifications, is denied.
func (w *webview) permissionRequested(args *permissionRequestedArgs) {
	state := permissionStateDeny
	uri := args.URI()
	switch kind := args.Kind(); kind {
	case permissionKindMicrophone:
		if w.permits(uri, PermissionMicrophone) {
			state = permissionStateAllow
		}
	case permissionKindCamera:
		if w.permits(uri, PermissionCamera) {
			state = permissionStateAllow
		}
	case permissionKindClipboardRead:
		if w.permits(uri, PermissionClipboard) {
			state = permissionStateAllow
		}
	default:
		log.Printf("tuohi: permission kind %d for %q denied", kind, uri)
	}
	args.SetState(state)
}

// --- extra Win32 / COM functions (ole32, advapi32, user32 RECT) ------------

var (
	coInitializeEx func(reserved uintptr, coinit uint32) int32
	coTaskMemFree  func(p uintptr)

	regOpenKeyExW    func(key uintptr, subkey *uint16, opts, desired uint32, out *uintptr) int32
	regQueryValueExW func(key uintptr, name *uint16, reserved uintptr, typ *uint32, data *byte, dataLen *uint32) int32
	regCloseKey      func(key uintptr) int32

	getClientRect func(hwnd uintptr, r *rect) int32

	comInitOnce sync.Once
	comInitErr  error
)

type rect struct{ Left, Top, Right, Bottom int32 }

func ensureCOMInit() error {
	comInitOnce.Do(func() {
		err := ensureWinInit()
		if err != nil {
			comInitErr = err
			return
		}
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load ole32.dll: %w", err)
			return
		}
		advapi32, err := syscall.LoadLibrary("advapi32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load advapi32.dll: %w", err)
			return
		}
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load user32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if comInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				comInitErr = fmt.Errorf("resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&coInitializeEx, ole32, "CoInitializeEx")
		reg(&coTaskMemFree, ole32, "CoTaskMemFree")
		reg(&regOpenKeyExW, advapi32, "RegOpenKeyExW")
		reg(&regQueryValueExW, advapi32, "RegQueryValueExW")
		reg(&regCloseKey, advapi32, "RegCloseKey")
		reg(&getClientRect, user32, "GetClientRect")
		if comInitErr != nil {
			return
		}
		sharedHandlerVtbl = &comHandlerVtbl{
			QueryInterface: purego.NewCallback(handlerQueryInterface),
			AddRef:         purego.NewCallback(handlerAddRef),
			Release:        purego.NewCallback(handlerRelease),
			Invoke:         purego.NewCallback(handlerInvoke),
		}
	})
	return comInitErr
}

// --- WebView2 loader: registry discovery, zero bundled DLL -----------------

const (
	hkeyLocalMachine = 0x80000002
	hkeyCurrentUser  = 0x80000001
	keyRead          = 0x20019
	keyWow6432Key    = 0x0200

	edgeClientStateKey = `SOFTWARE\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	minAPIVersion      = 1150
)

// findEmbeddedBrowserDLL locates the installed Edge WebView2 Runtime's
// EmbeddedBrowserWebView.dll via the registry (HKLM then HKCU), reimplementing
// loader.hh's built-in discovery so no DLL is bundled. This is a Go translation
// of github.com/webview/webview's WebView2 loader (MIT, Copyright (c) 2017 Serge
// Zaitsev, Copyright (c) 2022 Steffen André Langnes). See NOTICE.
func findEmbeddedBrowserDLL() (string, error) {
	for _, root := range []uintptr{hkeyLocalMachine, hkeyCurrentUser} {
		val, err := regReadString(root, edgeClientStateKey, "EBWebView")
		if err != nil || val == "" {
			continue
		}
		// The value's last path component is the runtime version (e.g. 120.0.2210.91).
		version := filepath.Base(val)
		if !versionBuildAtLeast(version, minAPIVersion) {
			continue
		}
		dll := filepath.Join(val, "EBWebView", arch(), "EmbeddedBrowserWebView.dll")
		_, err = os.Stat(dll)
		if err == nil {
			return dll, nil
		}
	}
	return "", errors.New("webview2: Edge WebView2 Runtime not found (install it)")
}

func arch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	}
	return "x64"
}

// versionBuildAtLeast reports whether the build field (3rd) of a dotted version
// string is >= min.
func versionBuildAtLeast(v string, min int) bool {
	parts := splitDots(v)
	if len(parts) < 3 {
		return false
	}
	return atoiSafe(parts[2]) >= min
}

func regReadString(root uintptr, subkey, name string) (string, error) {
	var key uintptr
	if regOpenKeyExW(root, utf16(subkey), 0, keyRead|keyWow6432Key, &key) != 0 {
		return "", errors.New("regOpenKeyExW failed")
	}
	defer regCloseKey(key)
	namePtr := utf16(name)
	var size uint32
	if regQueryValueExW(key, namePtr, 0, nil, nil, &size) != 0 || size == 0 {
		return "", errors.New("regQueryValueExW size query failed")
	}
	buf := make([]byte, size)
	if regQueryValueExW(key, namePtr, 0, nil, &buf[0], &size) != 0 {
		return "", errors.New("regQueryValueExW read failed")
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), size/2)
	// Trim trailing NUL(s).
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16Decode(u16)), nil
}

// createEnvironment loads the discovered runtime DLL and calls its
// CreateWebViewEnvironmentWithOptionsInternal export.
//
// Trade-off (deliberate): this is the internal/undocumented export that
// WebView2Loader.dll itself wraps, and calling it directly is what lets appkit
// bundle ZERO native DLLs. Microsoft documents that it may change or be removed,
// and that the stable, supported entry point is
// CreateCoreWebView2EnvironmentWithOptions -- but that one is only exported by
// WebView2Loader.dll, which would have to be shipped alongside the binary. appkit
// favors the zero-DLL design; if a future Edge runtime drops this export, the
// GetProcAddress below fails with a clear error rather than misbehaving.
func createEnvironment(userDataDir string, envHandler *comHandler) error {
	// The WebView2 backing surface defaults to opaque WHITE, and calling
	// put_DefaultBackgroundColor alone can leave the window white until it
	// takes effect. Setting the WEBVIEW2_DEFAULT_BACKGROUND_COLOR environment
	// variable (documented by Microsoft) lets the transparent background
	// apply from the very first composition - the value is parsed as AARRGGBB
	// hex, so an alpha of 00 (first two digits) gives full transparency
	// (e.g. "00000000"). Must be set before the environment is created and
	// stays effective for this process.
	if os.Getenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR") == "" {
		if err := os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "00000000"); err != nil {
			return err
		}
	}
	dll, err := findEmbeddedBrowserDLL()
	if err != nil {
		dbg("findEmbeddedBrowserDLL: %v", err)
		return err
	}
	dbg("runtime dll: %s", dll)
	mod, err := syscall.LoadLibrary(dll)
	if err != nil {
		return fmt.Errorf("load %s: %w", dll, err)
	}
	addr, err := syscall.GetProcAddress(mod, "CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		// This internal export is how appkit avoids bundling WebView2Loader.dll; an
		// incompatible/too-new Edge runtime that renamed or removed it lands here.
		return fmt.Errorf("resolve CreateWebViewEnvironmentWithOptionsInternal (internal WebView2 loader export; installed Edge runtime may be incompatible): %w", err)
	}
	// HRESULT(bool, webview2_runtime_type, PCWSTR userDataDir, IUnknown* options,
	//         ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler*)
	r, _, _ := purego.SyscallN(addr,
		1, // bool: true
		0, // runtime_type: installed
		uintptr(unsafe.Pointer(utf16(userDataDir))),
		0, // options: null
		handlerPtr(envHandler),
	)
	dbg("CreateWebViewEnvironmentWithOptionsInternal -> HRESULT 0x%08X", uint32(r))
	if int32(r) < 0 {
		return fmt.Errorf("CreateWebViewEnvironmentWithOptionsInternal: HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func userDataFolder() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	exe, _ := os.Executable()
	return filepath.Join(base, filepath.Base(exe))
}

// --- embed + the WebView2-backed View methods ---------------------------

const coinitApartmentThreaded = 0x2

func (w *webview) embed(v *View) error {
	err := ensureCOMInit()
	if err != nil {
		return err
	}
	coInitializeEx(0, coinitApartmentThreaded) // tolerate S_OK/S_FALSE/RPC_E_CHANGED_MODE

	w.envH = newHandler(w.id, kindEnv, &iidEnvironmentComplete)
	w.ctrlH = newHandler(w.id, kindController, &iidControllerComplete)
	w.msgH = newHandler(w.id, kindMessage, &iidMessageReceived)
	w.scriptH = newHandler(w.id, kindScript, &iidScriptAdded)

	dbg("embed: requesting environment (userDataFolder=%s)", userDataFolder())
	err = createEnvironment(userDataFolder(), w.envH)
	if err != nil {
		return err
	}

	// Pump the message loop until the controller + webview are ready.
	dbg("embed: pumping until ready")
	var m msgStruct
	for !w.ready {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			return errors.New("webview2: canceled before init")
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	dbg("embed: ready=%v controller=0x%x webview2=0x%x", w.ready, w.controller, w.webview2)
	if w.controller == 0 || w.webview2 == 0 {
		return errors.New("webview2: environment/controller creation failed")
	}

	// WebView2 settings: page JavaScript is always enabled (WebView2's native
	// TRUE; the JS-disable knob was removed), and window.chrome.webview must
	// stay on - appkit's JS bridge posts through it. appkit's tuned
	// divergences from WebView2's native defaults: the status bar is hidden
	// (custom chrome) and dev tools open only when the view's resolved Debug
	// flag is set (View.Debug OR App.Debug / TUOHI_DEBUG). Settings apply
	// from the next top-level navigation. Each property is written through
	// the interface that owns its offset, obtained from the Base settings
	// object by QueryInterface; a Runtime older than that interface skips the
	// property (settingsQI returns nil on E_NOINTERFACE) and leaves the
	// native default.
	devTools := v.Debug

	var base uintptr
	if int32(asWebView2(w.webview2).GetSettings(&base)) < 0 || base == 0 {
		dbg("embed: could not obtain ICoreWebView2Settings")
	} else {
		b := asSettings(base)
		b.PutIsScriptEnabled(true)
		b.PutIsWebMessageEnabled(true)
		b.PutAreDefaultScriptDialogsEnabled(true)
		b.PutIsStatusBarEnabled(false) // appkit hides WebView2's status bar (custom chrome)
		b.PutDevTools(devTools)
		b.PutAreDefaultContextMenusEnabled(true)
		b.PutAreHostObjectsAllowed(true)
		b.PutIsZoomControlEnabled(true)
		b.PutIsBuiltInErrorPageEnabled(true)

		applySettingsExtension := func(iid *guid, apply func(p uintptr)) {
			if p := b.settingsQI(iid); p != 0 {
				defer asSettingsSafeRelease(p)
				apply(p)
			}
		}
		// UserAgent stays empty (WebView2's own default); no Settings2 push.
		applySettingsExtension(&iidSettings3, func(p uintptr) {
			asSettings3(p).PutAreBrowserAcceleratorKeysEnabled(true)
		})
		applySettingsExtension(&iidSettings4, func(p uintptr) {
			asSettings4(p).PutIsPasswordAutosaveEnabled(false)
			asSettings4(p).PutIsGeneralAutofillEnabled(true)
		})
		applySettingsExtension(&iidSettings5, func(p uintptr) { asSettings5(p).PutIsPinchZoomEnabled(true) })
		applySettingsExtension(&iidSettings6, func(p uintptr) { asSettings6(p).PutIsSwipeNavigationEnabled(true) })
		applySettingsExtension(&iidSettings7, func(p uintptr) { asSettings7(p).PutHiddenPdfToolbarItems(0) }) // 0 = no toolbar buttons hidden
		applySettingsExtension(&iidSettings8, func(p uintptr) { asSettings8(p).PutIsReputationCheckingRequired(true) })
		applySettingsExtension(&iidSettings9, func(p uintptr) { asSettings9(p).PutIsNonClientRegionSupportEnabled(false) })

		b.Release()
	}

	// Make the WebView2 default background transparent before the first frame.
	w.applyDefaultBackgroundColor()

	w.rebuildScripts() // installs the bridge

	w.resizeWebView()
	asController(w.controller).PutIsVisible(true)
	// Set the application icon (App.Icon or the embedded default) on the
	// top-level window before it is first shown, so the taskbar button and
	// Alt-Tab entry carry it from the start.
	if w.ownsWindow {
		applyWindowAppIcon(w.window)
	}
	showWindow(w.window, swShow)
	updateWindow(w.window)
	// Re-assert the default background color now that the window is visible:
	// the first composition can otherwise flash opaque before the initial
	// put_DefaultBackgroundColor takes effect.
	w.applyDefaultBackgroundColor()
	// Pull keyboard focus into the content now that the controller exists. The
	// window already took WM_SETFOCUS during creation - before the controller was
	// ready, so that path could not move focus inward - so do it once here.
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)

	// The window is now visible with the WebView2 controller laid out and
	// focused. Note: WebView2's own Chrome_* child input windows are NOT
	// subclassed to forward drag-box hit tests to the top level - that
	// forwarder reenters Chromium's original window procs from a Go winproc
	// and faults (access violation + Chrome_WidgetWin_0 class-unregister
	// error 1412). Drag on Windows is started from the page instead (the
	// JS mouse-down -> beginMoveDrag/WM_NCLBUTTONDOWN path shared with
	// macOS/Linux), so no native child hit-test forwarding is needed.
	return nil
}

func (w *webview) resizeWebView() {
	if w.controller == 0 || w.window == 0 {
		return
	}
	var r rect
	if getClientRect(w.window, &r) != 0 {
		asController(w.controller).putBounds(r)
	}
}

// applyDefaultBackgroundColor sets the WebView2 default background to fully
// applyDefaultBackgroundColor makes the WebView2 default background match the
// window: fully transparent on frameless windows (page-transparent areas show
// through to the desktop) and opaque white on framed windows, via
// ICoreWebView2Controller2::put_DefaultBackgroundColor. Best-effort: runtimes
// without Controller2 (very old Edge) keep the default background.
func (w *webview) applyDefaultBackgroundColor() {
	if w.controller == 0 {
		return
	}
	var ctrl2 uintptr
	hr := asController(w.controller).QueryInterface(&iidController2, &ctrl2)
	if ctrl2 == 0 {
		dbg("applyDefaultBackgroundColor: no Controller2 (hr=0x%x ctrl2=0x%x); background color unavailable", uint32(hr), ctrl2)
		return
	}
	defer asController2(ctrl2).Release()
	// COREWEBVIEW2_COLOR is a {A, R, G, B} byte struct passed by value; on the
	// little-endian calling convention that is A in the low byte. A fully
	// transparent background (alpha 0) with the WS_EX_NOREDIRECTIONBITMAP
	// window style lets the desktop show through the page's transparent pixels;
	// a fully opaque one (alpha 0xFF) paints the usual white behind the page. A framed
	// window is opaque white; there is no per-window opaque-background option.
	var color uint32
	if w.frameless {
		color = 0 // A=0 -> fully transparent
	} else {
		color = 0x000000FF // A=0xFF -> opaque white
	}
	hr = asController2(ctrl2).PutDefaultBackgroundColor(color)
	dbg("applyDefaultBackgroundColor: color=0x%08x hr=0x%x", color, uint32(hr))
}

func (w *webview) Focus() {
	if w.controller == 0 {
		return
	}
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	// Restore first: a minimised window cannot come to the foreground, and
	// SetForegroundWindow would report failure rather than un-minimise it.
	showWindow(w.window, swRestore)
	// Windows refuses the foreground to a process that has not interacted with
	// the user recently, and flashes the taskbar button instead. That is the
	// OS's anti-focus-stealing policy working as intended, so the result is not
	// treated as an error.
	setForegroundWin(w.window)
}

// Show brings a hidden or minimized window back: SW_RESTORE un-minimizes /
// un-maximizes to the previous state, SW_SHOW makes it visible again (and
// puts it back into the taskbar list), and SetForegroundWindow activates it.
// w.Dispatch makes the call safe from any goroutine (see the View doc).
func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() {
		showWindow(w.window, swRestore)
		showWindow(w.window, swShow)
		setForegroundWin(w.window)
	})
}

// Hide removes the window from the screen with SW_HIDE, which also removes its
// taskbar button - the classic hide-to-tray behavior.
func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swHide) })
}

// Maximize enlarges the window to fill the work area (SW_MAXIMIZE).
func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swMaximize) })
}

// Minimize shrinks the window to its taskbar button (SW_MINIMIZE).
func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swMinimize) })
}

// Unminimize restores a minimized window to its normal state (SW_RESTORE).
// No-op when the window is not minimized.
func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swRestore) })
}

// setTitle sets the window's title (see applyTitle), on the UI thread. It
// names the taskbar button and the Alt-Tab entry of a frameless window too.
func (w *webview) setTitle(title string) {
	if w.window == 0 {
		return
	}
	setWindowTextW(w.window, utf16(title))
}

// Unmaximize restores a maximized window to its previous normal size
// (SW_RESTORE). No-op when the window is not maximized.
func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swRestore) })
}

// toggleMaximize flips the window between maximized and normal: SW_MAXIMIZE
// when IsZoomed reports false, SW_RESTORE otherwise. The OS state is the
// authority (the window may have been maximized by the user via the keyboard
// or by another path), so no Go-side bookkeeping is needed. The app-region
// tracker asks for it when a "drag" box is double-clicked (see view.go).
func (w *webview) toggleMaximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() {
		if isZoomed(w.window) != 0 {
			showWindow(w.window, swRestore)
		} else {
			showWindow(w.window, swMaximize)
		}
	})
}

// resolveURL maps the uniform app:// origin onto this view's serving origin:
// this window's loopback-server base configured at creation under
// App.HTTP, or - no server up - the URL
// unchanged, so the scheme rewrite below maps app:// onto the per-scheme
// https vhost. Every other URL passes through untouched.
func (w *webview) resolveURL(url string) string {
	if w.contentBase != "" {
		if w.transient != nil && w.transient.isClosed() {
			w.transient = nil
			w.contentBase = ""
			return url
		}
		return rewriteAppURL(w.contentBase, url)
	}
	return url
}

func (w *webview) Navigate(url string) {
	// First resolve the uniform app:// origin onto this view's serving
	// origin: this window's loopback server's http://localhost base under
	// App.HTTP, or - no server up - pass
	// through to the scheme rewrite below, which maps app:// onto the
	// per-scheme https vhost.
	url = w.resolveURL(url)
	url = w.rewriteSchemeURL(url) // map a registered scheme:// to its https vhost
	if w.webview2 == 0 {
		return
	}
	if url == "" {
		url = "about:blank"
	}
	url = canonicalNavigateURL(url)
	w.trust(url)
	asWebView2(w.webview2).Navigate(utf16(url))
}

// trust rebuilds the scripts without holding w.mu (see rebuildScripts).
func (w *webview) trust(urls ...string) {
	if w.trustURLs(urls) {
		w.rebuildScripts()
	}
}

// loadHTML loads html from a loopback server of its own, used by tests only.
// NavigateToString would put the page at about:blank, which cannot be
// trusted (see originOf). A data: URL did not work either: its bridge calls
// never reached the gate as a trusted sender on WebView2. A loopback page has
// an ordinary http origin, and Navigate trusts it. The server lives until the
// next loadHTML or until the window is destroyed.
func (w *webview) loadHTML(html string) {
	body := []byte(html)
	srv, base, err := listenLoopbackHTTP(func(*request) *response {
		return &response{Body: body, MIME: "text/html; charset=utf-8"}
	})
	if err != nil {
		log.Printf("tuohi: loadHTML: %v", err)
		return
	}
	stopLoopback(w.htmlServer.Swap(srv))
	w.Navigate(base + "/")
}

func (w *webview) Eval(js string) {
	if w.webview2 != 0 {
		asWebView2(w.webview2).ExecuteScript(utf16(js), 0)
	}
}

func (w *webview) Init(js string) { w.pushUserScript(js) }

// installDocScript adds one document-start script, pumping the message loop
// until WebView2 returns the script's id synchronously, and records that id
// in installedScriptIDs so it can be removed on rebuild.
//
// It must NOT be called with w.mu held: it pumps the loop, which can dispatch a
// WebMessageReceived into onMessage() (which locks w.mu). installedScriptIDs and
// userScriptSrcs are only ever touched on the UI thread.
func (w *webview) installDocScript(src string) {
	if w.webview2 == 0 {
		return
	}
	w.scriptDone = false
	w.lastScript = ""
	asWebView2(w.webview2).AddScript(utf16(src), handlerPtr(w.scriptH))
	var m msgStruct
	for !w.scriptDone {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			postQuitMessage(0) // re-post so Run() also terminates
			break
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	if w.lastScript != "" {
		w.installedScriptIDs = append(w.installedScriptIDs, w.lastScript)
	}
}

// pushUserScript records a persistent document-start script (an Init()
// script) and installs it, matching the macOS/Linux backends' method of the
// same name. The bridge and bind scripts are NOT recorded here; rebuildScripts
// generates them from the view's token, origins and bindings. Unlike
// macOS/Linux (which bulk-rebuild through a WebKit content manager), WebView2
// installs scripts one at a time through an async completion, so
// #installDocScript pumps the message loop until the script is installed.
func (w *webview) pushUserScript(src string) {
	w.mu.Lock()
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.mu.Unlock()
	w.installDocScript(src)
}

// removeAllDocScripts removes every installed document-start script by id.
func (w *webview) removeAllDocScripts() {
	if w.webview2 != 0 {
		cw := asWebView2(w.webview2)
		for _, id := range w.installedScriptIDs {
			cw.RemoveScript(utf16(id))
		}
	}
	w.installedScriptIDs = nil
}

// rebuildScripts re-installs the bridge, the persistent scripts, and a single
// bind script for the currently-bound names. It is the Windows counterpart of
// the macOS/Linux backends' rebuildScriptsLocked (removeAllUserScripts +
// createBindScript). Unlike WebKit, WebView2 installs each script through an
// async AddScriptToExecuteOnDocumentCreated completion, so each install pumps
// the message loop and rebuildScripts must run without w.mu held - which is
// why it intentionally carries no "_Locked" suffix, unlike the WebKit twin.
func (w *webview) rebuildScripts() {
	w.removeAllDocScripts()
	w.mu.Lock()
	bridge := w.bridgeScriptLocked(bridgePostFn)
	srcs := append([]string(nil), w.userScriptSrcs...)
	entries := w.bindingEntriesLocked()
	w.mu.Unlock()
	w.installDocScript(bridge)
	for _, src := range srcs {
		w.installDocScript(src)
	}
	w.installDocScript(createBindScript(entries))
}

// updateBindings changes the binding table under mu, then rebuilds the
// document-start scripts without holding it, because rebuildScripts pumps the
// message loop (see engine.updateBindings).
func (w *webview) updateBindings(mutate func(bindings map[string]binding) error) error {
	w.mu.Lock()
	err := mutate(w.bindings)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	w.rebuildScripts()
	return nil
}

// interceptOutsideLinks: WebView2 decides a top-level navigation in
// NavigationStarting, but a navigation cancelled there has still been
// requested from the server (GitHub run 36513098161, runtime 153), so the
// bridge hands the navigations a page visibly starts to Go first, as on
// WebKitGTK (see initOutsideLinks).
const interceptOutsideLinks = true

func (w *webview) handleInternal(method string, params json.RawMessage) bool {
	switch method {
	case internalAppRegions:
		// The app-region tracker reports the page's drag and no-drag boxes in
		// device pixels. The WndProc hit test consumes them.
		w.setRegions(parseAppRegionSet(params))
	case internalWindowDrag:
		// The page reported a mouse-down inside a drag box: start a native
		// window move.
		w.beginMoveDrag(parseDragRequest(params))
	case internalWindowResize:
		// The page reported a mouse-down on an edge band: start a native edge
		// resize.
		w.beginResizeDrag(parseDragRequest(params))
	case internalWindowToggleMaximize:
		// The tracker saw a double-click inside a "drag" box. The tracker
		// exists only on frameless windows, so a framed window ignores the
		// message.
		if w.frameless {
			w.toggleMaximize()
		}
	default:
		return false
	}
	return true
}

// --- wide-string + small helpers -------------------------------------------

func wideToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*uint16)(unsafe.Add(base, uintptr(n)*2)) != 0 {
		n++
	}
	return string(utf16Decode(unsafe.Slice((*uint16)(base), n)))
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u):
			lo := u[i+1]
			out = append(out, (rune(c-0xD800)<<10|rune(lo-0xDC00))+0x10000)
			i++
		default:
			out = append(out, rune(c))
		}
	}
	return out
}

func splitDots(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func atoiSafe(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// appSchemeVHost is the secure https origin the app scheme is served from on
// Windows (WebView2 has no per-scheme secure flag, so the scheme rides on an
// https virtual host instead).
const appSchemeVHost = "https://" + appSchemeName + ".localhost"

// --- COM wrappers for the WebResourceRequested event -----------------------

type webResourceRequestedArgsVtbl struct {
	unknownVtbl
	GetRequest  uintptr
	GetResponse uintptr
	PutResponse uintptr
}
type webResourceRequestedArgs struct{ vtbl *webResourceRequestedArgsVtbl }

func asWebResourceRequestedArgs(p uintptr) *webResourceRequestedArgs {
	return (*webResourceRequestedArgs)(ptr(p))
}

func (i *webResourceRequestedArgs) GetRequest(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetRequest, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *webResourceRequestedArgs) PutResponse(resp uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.PutResponse, uintptr(unsafe.Pointer(i)), resp)
	return r
}

type webResourceRequestVtbl struct {
	unknownVtbl
	GetUri uintptr
}
type webResourceRequest struct{ vtbl *webResourceRequestVtbl }

func asWebResourceRequest(p uintptr) *webResourceRequest { return (*webResourceRequest)(ptr(p)) }

// unknown is the prefix every COM interface starts with; releaseUnknown drops
// one reference on any interface pointer, so acquired objects (request, stream,
// response) do not leak - COM out-params and factories hand back AddRef'd
// references the caller owns.
type unknown struct{ vtbl *unknownVtbl }

func releaseUnknown(p uintptr) {
	if p == 0 {
		return
	}
	u := (*unknown)(ptr(p))
	purego.SyscallN(u.vtbl.Release, p)
}

func (i *webResourceRequest) GetUri(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func (i *environment) CreateWebResourceResponse(stream uintptr, status int, reason, headers *uint16, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateWebResourceResponse,
		uintptr(unsafe.Pointer(i)), stream, uintptr(status),
		uintptr(unsafe.Pointer(reason)), uintptr(unsafe.Pointer(headers)), uintptr(unsafe.Pointer(out)))
	return r
}

// --- SHCreateMemStream (shlwapi) -------------------------------------------

var (
	memStreamOnce sync.Once
	memStreamProc uintptr
)

// shCreateMemStream wraps SHCreateMemStream, which copies the bytes into a
// COM-owned IStream - so the Go slice need not outlive the call.
func shCreateMemStream(data []byte) uintptr {
	memStreamOnce.Do(func() {
		mod, err := syscall.LoadLibrary("shlwapi.dll")
		if err != nil {
			return
		}
		addr, err := syscall.GetProcAddress(mod, "SHCreateMemStream")
		if err != nil {
			return
		}
		memStreamProc = addr
	})
	if memStreamProc == 0 {
		return 0
	}
	var p *byte
	if len(data) > 0 {
		p = &data[0]
	}
	r, _, _ := purego.SyscallN(memStreamProc, uintptr(unsafe.Pointer(p)), uintptr(uint32(len(data)))) // #nosec G103 -- SHCreateMemStream copies the buffer
	return r
}

// --- request handling ------------------------------------------------------

// serveSchemeWindows answers one WebResourceRequested event for the app
// scheme's https vhost from the app-scope resolver. A nil response is left
// for WebView2 to turn into its default 404 (put no response).
func (w *webview) serveSchemeWindows(args uintptr) {
	a := asWebResourceRequestedArgs(args)
	var reqPtr uintptr
	if int32(a.GetRequest(&reqPtr)) < 0 || reqPtr == 0 {
		return
	}
	defer releaseUnknown(reqPtr)
	var uriPtr uintptr
	if int32(asWebResourceRequest(reqPtr).GetUri(&uriPtr)) < 0 || uriPtr == 0 {
		return
	}
	uri := wideToString(uriPtr)
	coTaskMemFree(uriPtr)

	if w.serve == nil || !strings.HasPrefix(uri, appSchemeVHost+"/") {
		return
	}
	resp := callServe(w.serve, &request{URL: w.canonicalSchemeURL(uri)})
	if resp == nil || w.environment == 0 {
		return
	}

	stream := shCreateMemStream(resp.Body)
	if stream == 0 {
		return
	}
	// The response object retains the stream, and PutResponse's WebView2 side
	// retains the response; the references acquired here are dropped once the
	// handoff is done (deferred, LIFO), so served assets stop leaking COM
	// objects.
	defer releaseUnknown(stream)
	// The document is delivered over WebView2's https vhost via
	// CreateWebResourceResponse with the asset's Content-Type and the
	// cross-origin-isolation headers (COOP: same-origin + COEP:
	// require-corp turn the vhost origin cross-origin isolated, which makes
	// SharedArrayBuffer available to the page; CORP: same-origin keeps COEP
	// from blocking the page's own vhost subresources). Chromium-based
	// WebView2 honours these headers on the https origin.
	headers := "Content-Type: " + schemeMIME(resp) +
		"\r\n" + headerCOOP + ": " + valSameOrigin +
		"\r\n" + headerCOEP + ": " + valRequireCorp +
		"\r\n" + headerCORP + ": " + valSameOrigin
	var respObj uintptr
	if int32(asEnvironment(w.environment).CreateWebResourceResponse(stream, 200, utf16("OK"), utf16(headers), &respObj)) < 0 || respObj == 0 {
		return
	}
	defer releaseUnknown(respObj)
	a.PutResponse(respObj)
}

// rewriteSchemeURL maps the app scheme's "<scheme>://<authority>/path" URL to
// its https vhost so the one uniform scheme URL works on every platform;
// other URLs pass through.
func (w *webview) rewriteSchemeURL(raw string) string {
	if w.serve == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme != appSchemeName {
		return raw
	}
	// The vhost origin has no place for the scheme's authority, so remember it
	// here; serveSchemeWindows uses it to rebuild the original URL for the
	// resolver (the other backends preserve the authority natively).
	if u.Host != "" {
		w.mu.Lock()
		w.schemeAuthority = u.Host
		w.mu.Unlock()
	}
	out := appSchemeVHost + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	// Preserve the fragment: it is client-side (never sent to the handler), but
	// dropping it here would break hash/path routing on the initial Navigate.
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// canonicalSchemeURL turns the internal https vhost URL WebView2 delivers back
// into the "app://<authority>/path?query" form the macOS/Linux backends pass
// to a resolver, so Request.URL has one shape on every platform. The
// authority is the one the app navigated with (recorded in rewriteSchemeURL),
// falling back to the scheme name if none was seen. Fragments are client-side
// and never reach a resource request, so none is reconstructed.
func (w *webview) canonicalSchemeURL(vhostURL string) string {
	u, err := url.Parse(vhostURL)
	if err != nil {
		return vhostURL
	}
	w.mu.Lock()
	authority := w.schemeAuthority
	w.mu.Unlock()
	if authority == "" {
		authority = appSchemeName
	}
	out := appSchemeName + "://" + authority + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

// --- Win32 windowing layer -------------------------------------------------
//
// The other half of the Windows View engine: the native window that hosts the
// ICoreWebView2Controller (owned or a caller-provided HWND), the engine
// registry the WebView2/COM handlers resolve through, the WM_APP dispatch
// queue, the drag-region hit testing and the View lifecycle methods (Run,
// Close, Window).
//
// Safety choice (per the COM/Win32 risk review): no Go pointer ever crosses
// into C. The engine is identified by an integer id stored in GWLP_USERDATA
// (seeded from CreateWindowExW's lpCreateParams in WM_NCCREATE) and looked up
// in a Go map; dispatched closures are keyed by an integer id passed via
// WM_APP's LPARAM; the WndProc trampolines come from purego.NewCallback. Only
// integers cross the boundary.

const (
	cwUseDefault = ^int32(0x7fffffff) // CW_USEDEFAULT (0x80000000)
	swHide       = 0                  // SW_HIDE
	swMaximize   = 3                  // SW_MAXIMIZE
	swShow       = 5                  // SW_SHOW
	swMinimize   = 6                  // SW_MINIMIZE
	swRestore    = 9                  // SW_RESTORE

	wsOverlappedWindow      = 0x00CF0000
	wsThickFrame            = 0x00040000
	wsMaximizeBox           = 0x00010000
	wsMinimizeBox           = 0x00020000
	wsPopup                 = 0x80000000
	wsExNoRedirectionBitmap = 0x00200000 // lets the desktop show through transparent pixels

	gwlpUserData = -21
	gwlStyle     = -16
	gwlWndProc   = -4

	wmNCCreate      = 0x0081
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmNCCalcSize    = 0x0083
	wmNCHitTest     = 0x0084
	wmNCLButtonDown = 0x00A1
	wmApp           = 0x8000
	wmQuit          = 0x0012
	wmSetIcon       = 0x0080 // WM_SETICON: set the window/taskbar icon

	iconSmall = 0 // ICON_SMALL (16px, title bar + taskbar button)
	iconBig   = 1 // ICON_BIG (32px, Alt-Tab / large views)

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002

	// WM_NCHITTEST results used by the drag-region machinery.
	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
)

// defaultWidth/defaultHeight is the fallback window size used by newView when
// the caller never drives geometry at runtime (mirrors the other backends).
const (
	defaultWidth  = 640
	defaultHeight = 480
)

// --- bound Win32 functions -------------------------------------------------

var (
	getModuleHandleW   func(name uintptr) uintptr
	registerClassExW   func(wc *wndClassExW) uint16
	createWindowExW    func(exStyle uint32, class, name *uint16, style uint32, x, y, w, h int32, parent, menu, inst, param uintptr) uintptr
	setWindowTextW     func(hwnd uintptr, text *uint16) int32
	getWindowTextW     func(hwnd uintptr, buf *uint16, max int32) int32
	defWindowProcW     func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	callWindowProcW    func(prev uintptr, hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	getMessageW        func(m *msgStruct, hwnd uintptr, min, max uint32) int32
	translateMessage   func(m *msgStruct) int32
	dispatchMessageW   func(m *msgStruct) uintptr
	postQuitMessage    func(code int32)
	postThreadMessageW func(thread uint32, msg uint32, wp, lp uintptr) int32
	getCurrentThreadID func() uint32
	postMessageW       func(hwnd uintptr, msg uint32, wp, lp uintptr) int32
	sendMessageW       func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	// createIconFromResourceEx builds an HICON from an ICO payload; the
	// payload may be a PNG image on Vista+ (see windowIconHandles).
	createIconFromResourceEx func(resource *byte, bytes uint32, isIcon int32, ver uint32, cx, cy int32, flags uint32) uintptr
	showWindow               func(hwnd uintptr, cmd int32) int32
	updateWindow             func(hwnd uintptr) int32
	destroyWindow            func(hwnd uintptr) int32
	setWindowLongPtrW        func(hwnd uintptr, index int32, val uintptr) uintptr
	getWindowLongPtrW        func(hwnd uintptr, index int32) uintptr
	setWindowPos             func(hwnd, after uintptr, x, y, w, h int32, flags uint32) int32
	screenToClient           func(hwnd uintptr, pt *point) int32
	setForegroundWin         func(hwnd uintptr) int32
	releaseCapture           func() int32
	getSystemMetrics         func(index int32) int32
	isZoomed                 func(hwnd uintptr) int32
)

// wndClassExW mirrors WNDCLASSEXW. Blank fields are left zero and unread by Go
// but kept so the struct's size/layout matches what RegisterClassExW expects.
type wndClassExW struct {
	cbSize        uint32
	_             uint32 // style
	lpfnWndProc   uintptr
	_             int32 // cbClsExtra
	_             int32 // cbWndExtra
	hInstance     uintptr
	_             uintptr // hIcon
	_             uintptr // hCursor
	_             uintptr // hbrBackground
	_             *uint16 // lpszMenuName
	lpszClassName *uint16
	_             uintptr // hIconSm
}

type point struct{ X, Y int32 }

// minMaxInfo mirrors Win32 MINMAXINFO; wndProc fills it on WM_GETMINMAXINFO to
// enforce the StateMin/StateMax sizes (the equivalent of win32_edge.hh's
// m_minsz/m_maxsz handling).
type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

// msgStruct mirrors MSG. Only message is read by Go; the blank fields are
// filled by GetMessageW and kept for the struct's C layout.
type msgStruct struct {
	_       uintptr // hwnd
	message uint32
	_       uint32  // padding after message
	_       uintptr // wParam
	_       uintptr // lParam
	_       uint32  // time
	_       point   // pt
	_       uint32  // lPrivate
}

var (
	winInitOnce sync.Once
	winInitErr  error

	wndProcCB      uintptr // window class proc for owned windows
	hostProcCB     uintptr // subclass proc for embedded (caller-owned) host HWNDs
	dispatchProcCB uintptr // window proc for the UI thread's dispatch window
)

func ensureWinInit() error {
	winInitOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load user32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load kernel32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if winInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				winInitErr = fmt.Errorf("webview: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&setWindowTextW, user32, "SetWindowTextW")
		reg(&getWindowTextW, user32, "GetWindowTextW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&callWindowProcW, user32, "CallWindowProcW")
		reg(&getMessageW, user32, "GetMessageW")
		reg(&translateMessage, user32, "TranslateMessage")
		reg(&dispatchMessageW, user32, "DispatchMessageW")
		reg(&postQuitMessage, user32, "PostQuitMessage")
		reg(&postMessageW, user32, "PostMessageW")
		reg(&postThreadMessageW, user32, "PostThreadMessageW")
		reg(&getCurrentThreadID, kernel32, "GetCurrentThreadId")
		reg(&sendMessageW, user32, "SendMessageW")
		reg(&createIconFromResourceEx, user32, "CreateIconFromResourceEx")
		reg(&showWindow, user32, "ShowWindow")
		reg(&updateWindow, user32, "UpdateWindow")
		reg(&destroyWindow, user32, "DestroyWindow")
		// SetWindowLongPtrW/GetWindowLongPtrW only exist as 64-bit exports: in
		// the Win32 headers they are macros that map to SetWindowLongW /
		// GetWindowLongW on 32-bit Windows, whose exports have no "...PtrW"
		// names. Resolve the platform's real procedure name (the GWL_/GWLP_
		// index constants used at the call sites have identical values on both
		// pointer widths - -21/-16/-4 - so no index change is needed).
		setLongPtr, getLongPtr := "SetWindowLongPtrW", "GetWindowLongPtrW"
		if unsafe.Sizeof(uintptr(0)) == 4 {
			setLongPtr, getLongPtr = "SetWindowLongW", "GetWindowLongW"
		}
		reg(&setWindowLongPtrW, user32, setLongPtr)
		reg(&getWindowLongPtrW, user32, getLongPtr)
		reg(&setWindowPos, user32, "SetWindowPos")
		reg(&screenToClient, user32, "ScreenToClient")
		reg(&setForegroundWin, user32, "SetForegroundWindow")
		reg(&releaseCapture, user32, "ReleaseCapture")
		reg(&getSystemMetrics, user32, "GetSystemMetrics")
		reg(&isZoomed, user32, "IsZoomed")
		if winInitErr != nil {
			return
		}
		wndProcCB = purego.NewCallback(wndProc)
		hostProcCB = purego.NewCallback(hostProc)
		dispatchProcCB = purego.NewCallback(dispatchProc)
	})
	return winInitErr
}

// utf16 returns a NUL-terminated UTF-16 pointer for s.
func utf16(s string) *uint16 {
	u := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r < 0x10000 {
			u = append(u, uint16(r))
		} else {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		}
	}
	u = append(u, 0)
	return &u[0]
}

// lParamPoint decodes the screen-space client point Win32 packs into the
// LPARAM of a WM_NCHITTEST / WM_NCLBUTTONDOWN message (two signed 16-bit
// halves; coordinates may be negative on a multi-monitor desktop).
func lParamPoint(lp uintptr) point {
	return point{
		X: int32(int16(lp & 0xffff)),
		Y: int32(int16((lp >> 16) & 0xffff)),
	}
}

// lParamXY packs screen coordinates into the LPARAM shape Win32 expects.
func lParamXY(x, y int32) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

// --- engine registry (integer id <-> engine; no Go pointer crosses to C) ---

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	uiThreadOnce sync.Once
	// uiThreadApp is the thread the first newView pinned, which owns every
	// window and the dispatch window. Zero until the first view is created.
	uiThreadApp atomic.Uint32

	// windowCount tracks live owned windows so a user-initiated close of the
	// last one ends Run() with WM_QUIT.
	windowCount int32
)

func registerEngine(w *webview) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = w
	regMu.Unlock()
	return id
}

func unregisterEngine(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id uintptr) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

// --- webview (Windows implementation behind the View struct) ----------------

// webview is the Windows implementation behind the View struct: it owns the
// native window (or wraps a caller-provided HWND), the WebView2/COM state and
// the document-start scripts, exactly like its macOS/Linux counterparts.
type webview struct {
	id         uintptr // registry key; crosses into C, never the Go pointer
	hinst      uintptr
	window     uintptr // HWND (owned, or the embedded host)
	ownsWindow bool

	// WebView2 / COM state (see the embed section above).
	controller  uintptr // ICoreWebView2Controller*
	webview2    uintptr // ICoreWebView2*
	environment uintptr // ICoreWebView2Environment* (CreateWebResourceResponse)
	envH        *comHandler
	ctrlH       *comHandler
	msgH        *comHandler
	scriptH     *comHandler
	wrrH        *comHandler // WebResourceRequested handler (custom schemes)
	navH        *comHandler // NavigationCompleted handler (View.Ready)
	navStartH   *comHandler // NavigationStarting handler (the navigation policy)
	permH       *comHandler // PermissionRequested handler (View.Permissions)
	contentH    *comHandler // ContentLoading handler (a data: page's sender)
	newWinH     *comHandler // NewWindowRequested handler (the navigation policy)
	ready       bool
	scriptDone  bool
	lastScript  string

	// htmlServer is the loopback server of the test-only loadHTML, stopped
	// by the next loadHTML or by Destroy.
	htmlServer atomic.Pointer[loopbackServer]

	// The URIs of the navigations NavigationStarting let proceed and whose
	// documents have not committed, by navigation ID, and the URI of the one
	// whose document last committed: the document the view shows, read as a
	// message's sender when WebView2 names it about:blank. UI thread only.
	pendingNavs  map[uint64]string
	committedURI string

	// schemeAuthority remembers the scheme:// authority used on Navigate so
	// request-time URLs can be reconstructed (see rewriteSchemeURL /
	// canonicalSchemeURL).
	schemeAuthority string

	// Window size constraints from View.State (StateMin/StateMax); enforced in
	// wndProc's WM_GETMINMAXINFO handler.
	minWidth, minHeight int32
	maxWidth, maxHeight int32

	// frameless windows drop the OS frame; the page's -app-region
	// boxes then drive the WM_NCHITTEST-based move drag, and the edge bands
	// drive the matching WM_NCLBUTTONDOWN resize.
	frameless bool

	// fixed records that the window was created un-resizable (State ==
	// StateFixed): a frameless fixed window drops WS_THICKFRAME so neither the
	// OS nor the page edge bands can resize it.
	fixed bool

	// regions is the page's latest drag/no-drag box set (device px, client
	// coordinates), reported via the __tuohiAppRegions message. Written on
	// the UI thread by handleInternal; read on the UI thread by the hit-test path.
	regions appRegionSet

	// hostOrig is the original window proc of an embedded (caller-owned) host
	// HWND, saved before subclassing it for WM_APP dispatch / WM_SIZE routing
	// (zero for owned windows). Only touched on the UI thread.
	hostOrig uintptr

	// Persistent document-start scripts (bridge + Init + app-region tracker);
	// see userScriptSrcs / installedScriptIDs in the script section above.
	installedScriptIDs []string
	viewCore

	// uiThread is the thread that created the window (GetCurrentThreadId at
	// construction). The HWND and the WebView2 controller/environment belong
	// to it, so Destroy() marshals its teardown there when Close is called
	// from another goroutine (View.Close is documented safe from any
	// goroutine, and bindings run on their own goroutines). Captured at
	// creation rather than reusing uiThreadID(), which is only filled in
	// lazily by the App.Wait loop.
	uiThread uint32

	// Per-engine Dispatch queue: closures posted via WM_APP, keyed by an
	// integer id (no Go pointer crosses to C). Per-engine so Destroy can drop
	// any pending closures instead of leaking them in a shared global map.
	dispatchMu  sync.Mutex
	dispatchMap map[uintptr]func()
	dispatchSeq uintptr
}

var classNamePtr = utf16("tuohi_webview")

// newView creates a window and its web view on Windows. The App.Show method
// opens the app scope first and then calls this constructor with the
// committed App.FS; the meaning of opts (Debug, Window, ...) is
// documented there. The first successful call pins the calling goroutine to
// its OS thread.
func newView(v *View, serve serveFunc) (*webview, error) {
	err := ensureWinInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(func() {
		runtime.LockOSThread()
		uiThreadApp.Store(getCurrentThreadID())
		createDispatchWindow()
	})

	w := &webview{
		ownsWindow:  v.window == nil,
		frameless:   !v.Frame,
		fixed:       !v.Frame && v.State == StateFixed,
		dispatchMap: map[uintptr]func(){},
	}
	w.bindings = map[string]binding{}
	w.serve = serve
	w.id = registerEngine(w)
	w.hinst = getModuleHandleW(0)

	if w.ownsWindow {
		style := uint32(wsOverlappedWindow)
		if w.frameless {
			// A frameless window is a WS_POPUP (no OS caption/decorations).
			// WS_MINIMIZEBOX is kept even though nothing draws it: the taskbar
			// minimizes the foreground window with WM_SYSCOMMAND SC_MINIMIZE,
			// which DefWindowProc only honours when this style is set - without
			// it, clicking the taskbar button of a visible window does nothing
			// (only the restore half of the taskbar toggle works).
			// WS_THICKFRAME is still added when the window may be resized: it
			// is what makes DefWindowProc honour the WM_NCLBUTTONDOWN(HTCAPTION
			// / HT<edge>) that beginMoveDrag/beginResizeDrag send to start the
			// modal move/resize loop, and it does not paint a visible frame on
			// a borderless popup. StateFixed (or an explicit non-resizable
			// decode) drops it so the window cannot be resized.
			style = wsPopup | wsMinimizeBox
			if !w.fixed {
				style |= wsThickFrame
			}
		}
		wc := wndClassExW{
			lpfnWndProc:   wndProcCB,
			hInstance:     w.hinst,
			lpszClassName: classNamePtr,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		registerClassExW(&wc) // idempotent across instances (same class name)

		w.window = createWindowExW(
			wsExNoRedirectionBitmap, classNamePtr, utf16(""), style,
			cwUseDefault, cwUseDefault, defaultWidth, defaultHeight,
			0, 0, w.hinst, w.id, // lpCreateParams = engine id (integer)
		)
		if w.window == 0 {
			unregisterEngine(w.id)
			return nil, errNoWindow
		}
		atomic.AddInt32(&windowCount, 1)
	} else {
		w.window = uintptr(v.window)
		// Subclass the host window so WM_APP dispatch and WM_SIZE routing reach
		// this engine while the host keeps its own window proc for everything
		// else (the controller's bounds follow the host through WM_SIZE).
		w.hostOrig = setWindowLongPtrW(w.window, gwlWndProc, hostProcCB)
		setWindowLongPtrW(w.window, gwlpUserData, w.id)
	}

	// Remember the thread that owns the window (and the WebView2 controller /
	// environment created below): Destroy tears them down on it. See Destroy.
	w.uiThread = getCurrentThreadID()

	if err := w.embed(v); err != nil {
		w.Destroy()
		return nil, err
	}
	if w.frameless && w.ownsWindow {
		// Track the page's -app-region boxes: the native side hit-tests
		// with the region list (postRegions=true) and a mouse-down in a "drag"
		// box returns HTCAPTION from WM_NCHITTEST. Runs at document-start of
		// every navigation so the regions follow the content.
		w.pushUserScript(createAppRegionScript(v.State != StateFixed, true, "windows"))
	}
	if w.ownsWindow {
		// Apply the creation-time geometry (the View fields; the backend
		// default size when Width/Height are zero), which also shows the
		// window. Window geometry is fixed after creation - there is no
		// runtime move/resize API.
		w.applyGeometry(v)
	}
	// Per-view serving origin: start a window's loopback server under
	// App.HTTP - stopped by releaseLoopback when the window is destroyed
	// (see viewContentBase); otherwise the window's
	// content is served on the app scheme's https vhost
	// (serveSchemeWindows). A start failure tears the freshly created window
	// down.
	w.contentBase, w.transient, err = viewContentBase(v, false)
	if err != nil {
		w.Destroy()
		return nil, err
	}
	return w, nil
}

// wndProc is the class window proc for owned windows. It recovers the engine
// via the integer id stored in GWLP_USERDATA (seeded in WM_NCCREATE from
// lpCreateParams).
func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	var id uintptr
	if msg == wmNCCreate {
		// lp -> CREATESTRUCTW; lpCreateParams is the first field (offset 0).
		// Reinterpret the LPARAM bits as a pointer without a direct
		// uintptr->Pointer conversion (keeps go vet happy).
		cs := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
		id = *(*uintptr)(cs)
		setWindowLongPtrW(hwnd, gwlpUserData, id)
	} else {
		id = getWindowLongPtrW(hwnd, gwlpUserData)
	}
	w := lookupEngine(id)
	if w == nil {
		return defWindowProcW(hwnd, msg, wp, lp)
	}
	if res, handled := w.engineMsg(hwnd, msg, wp, lp); handled {
		return res
	}
	return defWindowProcW(hwnd, msg, wp, lp)
}

// hostProc is the subclass proc installed on an embedded (caller-owned) host
// HWND. It routes only the messages appkit must see (WM_APP dispatch and
// WM_SIZE -> controller bounds) and hands everything else to the host's own
// window proc.
func hostProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	id := getWindowLongPtrW(hwnd, gwlpUserData)
	w := lookupEngine(id)
	if w == nil {
		return defWindowProcW(hwnd, msg, wp, lp)
	}
	switch msg {
	case wmApp:
		w.runDispatch(lp)
		return 0
	case wmSize:
		w.resizeWebView()
	}
	if w.hostOrig != 0 {
		return callWindowProcW(w.hostOrig, hwnd, msg, wp, lp)
	}
	return defWindowProcW(hwnd, msg, wp, lp)
}

// engineMsg handles the messages an owned window's class proc must service
// and reports whether it did. Shared hit-test and close logic is kept here so
// hostProc could route the same messages if a future embed host needs them.
func (w *webview) engineMsg(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case wmApp:
		w.runDispatch(lp)
		return 0, true
	case wmSize:
		w.resizeWebView()
		return 0, true
	case wmNCCalcSize:
		// Frameless windows carry WS_THICKFRAME only so DefWindowProc will run
		// the modal resize (SC_SIZE) - the frame itself is not wanted. Return 0
		// so the whole window rect is the client area (no invisible resize
		// border / white inset the frame would otherwise keep), like the
		// reference does. When maximized we leave the system to size the frame
		// so the window still snaps inside the monitor work area.
		if w.frameless && w.ownsWindow && isZoomed(w.window) == 0 {
			return 0, true
		}
		return 0, false
	case wmSetFocus:
		// The window gained keyboard focus (launch, Alt-Tab back, a title-bar
		// click); forward it into the WebView2 content so the keyboard and a
		// screen reader's cursor land in the page. A no-op until the controller
		// exists (embed does the initial focus instead).
		if w.controller != 0 {
			asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
		}
		return 0, true
	case wmGetMinMaxInfo:
		// Enforce StateMin/StateMax, mirroring win32_edge.hh's WM_GETMINMAXINFO.
		mmi := (*minMaxInfo)(ptr(lp))
		if w.maxWidth > 0 && w.maxHeight > 0 {
			mmi.ptMaxSize = point{w.maxWidth, w.maxHeight}
			mmi.ptMaxTrackSize = point{w.maxWidth, w.maxHeight}
		}
		if w.minWidth > 0 && w.minHeight > 0 {
			mmi.ptMinTrackSize = point{w.minWidth, w.minHeight}
		}
		return 0, true
	case wmNCHitTest:
		if w.frameless && !w.regions.empty() {
			if pt, ok := w.clientPoint(hwnd, lp); ok && w.regions.isDrag(float64(pt.X), float64(pt.Y)) {
				return uintptr(int32(htCaption)), true
			}
		}
		return 0, false // not a drag box: let DefWindowProcW answer (HTCLIENT/edges)
	case wmClose:
		// WM_CLOSE is the user-initiated close (the X button / Alt+F4);
		// Destroy() calls destroyWindow directly and never routes through
		// here. destroyWindow runs WM_DESTROY synchronously (decrementing
		// windowCount), so once the last owned window is closed this way we
		// post WM_QUIT to end Run().
		destroyWindow(hwnd)
		if w.ownsWindow && atomic.LoadInt32(&windowCount) <= 0 {
			postQuitMessage(0)
		}
		return 0, true
	case wmDestroy:
		// Closed via the OS or by Destroy(): reclaim the registry entry so the
		// webview is not pinned when Destroy() is never called
		// (unregisterEngine is idempotent), and drop the owned-window count.
		unregisterEngine(w.id)
		if w.ownsWindow {
			atomic.AddInt32(&windowCount, -1)
			// Single per-window close event for the App scope (App.Wait).
			appWindowClosed()
		}
		w.window = 0
		setWindowLongPtrW(hwnd, gwlpUserData, 0)
		// Release the web view here, on the UI thread, while the window and
		// the loop that delivered this message are still alive. A user close
		// can end the last loop, and a later Close from another goroutine
		// would then post a teardown that nothing drains. When Destroy got
		// here through destroyOnUI, the controller is already closed and only
		// the environment is released early.
		w.closeController()
		w.releaseEnvironment()
		return 0, true
	}
	return 0, false
}

// clientPoint converts the screen-space point in a WM_NCHITTEST LPARAM to this
// window's client coordinates. It reports false when the point is not inside
// the window at all.
func (w *webview) clientPoint(hwnd uintptr, lp uintptr) (point, bool) {
	pt := lParamPoint(lp)
	if screenToClient(hwnd, &pt) == 0 {
		return point{}, false
	}
	return pt, true
}

// The UI thread's dispatch window is a message-only window (HWND_MESSAGE)
// created with the first view, on the thread that owns every view. It carries
// the dispatcher's posts (postUI) and outlives every view's own window. A
// window, unlike a thread message (PostThreadMessageW), still receives its
// messages while a modal loop such as a window drag or a dialog pumps
// instead of Run.
var (
	dispatchHWND  atomic.Uintptr
	uiDispatchMu  sync.Mutex
	uiDispatchMap = map[uintptr]func(){}
	uiDispatchSeq uintptr

	dispatchClassName = utf16("tuohi_dispatch")
)

// hwndMessage is HWND_MESSAGE, ((HWND)-3): the parent that makes a window
// message-only.
const hwndMessage = ^uintptr(2)

// createDispatchWindow creates the dispatch window on the calling thread,
// which must be the UI thread. On failure postUI refuses, and callers fall
// back as they did before the dispatch window existed.
func createDispatchWindow() {
	hinst := getModuleHandleW(0)
	wc := wndClassExW{
		lpfnWndProc:   dispatchProcCB,
		hInstance:     hinst,
		lpszClassName: dispatchClassName,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	registerClassExW(&wc)
	hwnd := createWindowExW(0, dispatchClassName, utf16(""), 0, 0, 0, 0, 0,
		hwndMessage, 0, hinst, 0)
	if hwnd == 0 {
		log.Printf("tuohi: dispatch window: CreateWindowExW failed")
		return
	}
	dispatchHWND.Store(hwnd)
}

// dispatchProc is the dispatch window's proc: WM_APP carries the id of a
// closure postUI queued.
func dispatchProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	if msg != wmApp {
		return defWindowProcW(hwnd, msg, wp, lp)
	}
	uiDispatchMu.Lock()
	f := uiDispatchMap[lp]
	delete(uiDispatchMap, lp)
	uiDispatchMu.Unlock()
	if f != nil {
		f()
	}
	return 0
}

// postUI is the dispatcher's post hook (see uiDispatcher). It refuses before
// the first view has created the dispatch window, or when the post fails.
func postUI(f func()) bool {
	hwnd := dispatchHWND.Load()
	if hwnd == 0 {
		return false
	}
	uiDispatchMu.Lock()
	uiDispatchSeq++
	id := uiDispatchSeq
	uiDispatchMap[id] = f
	uiDispatchMu.Unlock()
	if postMessageW(hwnd, wmApp, 0, id) == 0 {
		uiDispatchMu.Lock()
		delete(uiDispatchMap, id)
		uiDispatchMu.Unlock()
		return false
	}
	return true
}

// onUIThread is the dispatcher's onUI hook. It is true before any view
// exists, when there is no other thread to defer to.
func onUIThread() bool {
	t := uiThreadApp.Load()
	return t == 0 || getCurrentThreadID() == t
}

// uiLoopExternal is the dispatcher's external hook. The UI thread's messages
// are pumped by App.Wait and Run; a caller-owned host window's loop is not
// counted, since tuohi cannot tell whether it runs.
func uiLoopExternal() bool { return false }

func (w *webview) runDispatch(lp uintptr) {
	w.dispatchMu.Lock()
	f := w.dispatchMap[lp]
	delete(w.dispatchMap, lp)
	w.dispatchMu.Unlock()
	if f != nil {
		f()
	}
}

// --- View lifecycle --------------------------------------------------------

func (w *webview) Run() {
	ui.enterLoop()
	defer ui.exitLoop()
	var m msgStruct
	for getMessageW(&m, 0, 0, 0) > 0 {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
}

func (w *webview) Terminate() {
	// PostQuitMessage posts WM_QUIT to the CALLING thread's queue. Bindings run
	// on goroutines (off the UI thread), so route it to the UI thread via the
	// dispatch queue.
	w.Dispatch(func() { postQuitMessage(0) })
}
func (w *webview) Dispatch(f func()) {
	if w.window == 0 {
		// The window is gone, and PostMessageW with a NULL HWND would post to
		// the caller's own thread, where nothing runs the closure. The UI
		// thread's dispatch window still takes it.
		postUI(f)
		return
	}
	w.dispatchMu.Lock()
	w.dispatchSeq++
	id := w.dispatchSeq
	w.dispatchMap[id] = f
	w.dispatchMu.Unlock()
	if postMessageW(w.window, wmApp, 0, id) == 0 {
		// The window is already gone: reclaim the entry rather than leak it.
		w.dispatchMu.Lock()
		delete(w.dispatchMap, id)
		w.dispatchMu.Unlock()
	}
}

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// applyGeometry applies the creation-time geometry from the View to an owned
// window: Width/Height (backend default when zero) with State, and - when
// given - Left/Top afterwards. Window geometry is
// fixed after creation - there is no runtime move/resize API.
func (w *webview) applyGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	w.applySize(width, height, v.State)
	if v.Left != 0 || v.Top != 0 {
		const swpNoSize = 0x0001 // SWP_NOSIZE: keep the size, only move
		setWindowPos(w.window, 0, int32(v.Left), int32(v.Top), 0, 0,
			swpNoZOrder|swpNoActivate|swpNoSize)
	}
}

// applySize sizes (or constrains, for StateMin/StateMax) the window and shows
// it; the creation-time counterpart of the old runtime SetSize.
func (w *webview) applySize(width, height int, state State) {
	// StateMin/StateMax only record constraints (enforced via WM_GETMINMAXINFO);
	// they do not resize the window, matching win32_edge.hh.
	switch state {
	case StateMin:
		w.minWidth, w.minHeight = int32(width), int32(height)
		return
	case StateMax:
		w.maxWidth, w.maxHeight = int32(width), int32(height)
		return
	}
	if !w.frameless {
		// Frame windows toggle the resize frame and maximize box. Frameless
		// windows resize from the page's edge bands instead (see
		// beginResizeDrag), so their style is left alone here.
		style := getWindowLongPtrW(w.window, gwlStyle)
		if state == StateFixed {
			style &^= uintptr(wsThickFrame | wsMaximizeBox)
		} else {
			style |= uintptr(wsThickFrame | wsMaximizeBox)
		}
		setWindowLongPtrW(w.window, gwlStyle, style)
	}
	setWindowPos(w.window, 0, 0, 0, int32(width), int32(height),
		swpNoZOrder|swpNoActivate|swpNoMove)
	if w.ownsWindow {
		showWindow(w.window, swShow)
		updateWindow(w.window)
	}
}

// Destroy tears the window and web view down. The HWND and the WebView2
// controller/environment belong to the thread that created them, so when
// Destroy runs on another goroutine (View.Close is documented safe from any
// goroutine, and bindings run on their own goroutines) the UI-thread-only part
// is marshalled onto the UI thread: a cross-thread DestroyWindow silently
// fails, so WM_DESTROY never fires, the owned-window count is never dropped
// and a later WM_CLOSE stops posting WM_QUIT - Run() would then block forever.
func (w *webview) Destroy() {
	// The window's loopback server, if it has one, lives until here.
	w.releaseLoopback()
	stopLoopback(w.htmlServer.Swap(nil))
	// Off the UI thread the teardown is always marshalled, never run in
	// place: the controller and environment belong to the UI thread. Dispatch
	// posts to this view's window, or to the UI thread's dispatch window once
	// the view's window is gone. When neither can take it, the teardown is
	// dropped rather than run on the wrong thread. A window the user closed
	// has little left to tear down by then: WM_DESTROY already released the
	// web view on the UI thread (see engineMsg), which matters when that
	// close also ended the last loop and nothing would drain the post.
	if w.uiThread != 0 && getCurrentThreadID() != w.uiThread {
		w.Dispatch(w.destroyOnUI)
		return
	}
	w.destroyOnUI()
}

// destroyOnUI performs the UI-thread-only part of Destroy. See Destroy.
func (w *webview) destroyOnUI() {
	w.closeController()
	if w.window != 0 && w.ownsWindow {
		destroyWindow(w.window)
		w.window = 0
	}
	w.releaseEnvironment()
	// Drop any Dispatch closures that were queued but never delivered (their
	// WM_APP messages die with the window), instead of leaking them.
	w.dispatchMu.Lock()
	w.dispatchMap = map[uintptr]func(){}
	w.dispatchMu.Unlock()
	unregisterEngine(w.id)
}

// closeController closes the WebView2 controller and releases the web view
// and controller references, on the UI thread. It is a no-op once done.
func (w *webview) closeController() {
	if w.controller != 0 {
		// Close the controller, then release the references we took in
		// handlerInvoke (ICoreWebView2 was AddRef'd, the controller too),
		// matching win32_edge.hh's teardown order. Close() tears down the
		// WebView: it destroys the browser-side windows on the UI thread, so
		// it must run before the host HWND and before the run loop ends -
		// otherwise Chromium's Chrome_WidgetWin_0 child HWNDs are still open
		// when the process unwinds and their class cannot be unregistered
		// (ERROR_CLASS_HAS_WINDOWS, 1412).
		asController(w.controller).Close()
		if w.webview2 != 0 {
			asWebView2(w.webview2).Release()
			w.webview2 = 0
		}
		asController(w.controller).Release()
		w.controller = 0
	}
}

// releaseEnvironment drops the WebView2 environment reference, on the UI
// thread, after closeController. It is a no-op once done.
func (w *webview) releaseEnvironment() {
	// The environment reference taken in handlerInvoke (kindEnv) is never
	// released anywhere else. The environment is what pins the WebView2
	// browser process alive; dropping the reference is what lets it shut down
	// cleanly after its last WebView closes. Releasing it only at process exit
	// (or never) makes Chromium unwind its HWNDs abruptly instead of on a live
	// message loop, racing the Chrome_WidgetWin_0 class unregister (1412).
	if w.environment != 0 {
		asEnvironment(w.environment).Release()
		w.environment = 0
	}
}

// --- drag regions ----------------------------------------------------------

// setRegions stores the page's latest drag/no-drag boxes for the WM_NCHITTEST
// hit test. The message arrives on the UI thread (a WebMessageReceived
// callback), the same thread the hit test runs on.
func (w *webview) setRegions(rs appRegionSet) { w.regions = rs }

// beginMoveDrag starts a native window move for a mouse-down the page reported
// inside a drag box (the JS-backed path, mirroring macOS/Linux). It releases
// the mouse capture the WebView2 child input window holds from the mouse-down,
// then sends WM_NCLBUTTONDOWN with HTCAPTION so DefWindowProc runs the modal
// move loop. Releasing the capture first is what lets the OS engage the move:
// while the child still owns the capture, the non-client message would not
// start a move. This is the same robust path the reference (last-known-working
// webview_windows.go) uses; it does not rely on HTTRANSPARENT child-forwarding.
func (w *webview) beginMoveDrag(p dragRequestParams) {
	if w.window == 0 || !w.frameless || !w.ownsWindow {
		return
	}
	releaseCapture()
	sendMessageW(w.window, wmNCLButtonDown, uintptr(int32(htCaption)), lParamXY(p.ScreenX, p.ScreenY))
}

// htCodeFor maps the tracker's direction names onto the WM_NCHITTEST border
// hit codes used to start a native edge resize, or -1 for unknown input.
func htCodeFor(direction string) int {
	switch direction {
	case "nw":
		return htTopLeft
	case "n":
		return htTop
	case "ne":
		return htTopRight
	case "w":
		return htLeft
	case "e":
		return htRight
	case "sw":
		return htBottomLeft
	case "s":
		return htBottom
	case "se":
		return htBottomRight
	}
	return -1
}

// beginResizeDrag starts a native edge resize for a mouse-down the page
// reported on an edge band, via WM_NCLBUTTONDOWN with the matching hit code.
// Like beginMoveDrag it releases the capture first so the OS modal resize loop
// engages (the WebView2 child input window captures the mouse while pressed).
func (w *webview) beginResizeDrag(p dragRequestParams) {
	if w.window == 0 || !w.frameless || !w.ownsWindow {
		return
	}
	if getWindowLongPtrW(w.window, gwlStyle)&uintptr(wsThickFrame) == 0 {
		return // StateFixed: not resizable
	}
	code := htCodeFor(p.Direction)
	if code < 0 {
		return
	}
	releaseCapture()
	sendMessageW(w.window, wmNCLButtonDown, uintptr(int32(code)), lParamXY(p.ScreenX, p.ScreenY))
}

// platformBackend reports the web-engine backend in use. Windows has a single
// built-in backend (WebView2), so there is nothing to detect or override -
// TUOHI_BACKEND is Linux-only (see lib_unix.go).
func platformBackend() string { return "WebView2" }

// --- app-level run loop (App.Wait) -----------------------------------------

var (
	waitThreadOnce sync.Once
	uiThreadIDv    uint32
)

func uiThreadID() uint32 {
	waitThreadOnce.Do(func() {
		// Capture lazily: appUIWait runs on the UI thread.
		uiThreadIDv = getCurrentThreadID()
	})
	return uiThreadIDv
}

// appUIWait pumps Windows messages until WM_QUIT. App.Wait calls it
// repeatedly and re-checks the app-scope exit flag between calls.
// uiThreadErr is nil: Win32 has no main-thread rule, and each window belongs
// to the thread that created it (see ErrNotMainThread).
func uiThreadErr() error { return nil }

// startOnUI runs Wait's start step in place, on the goroutine that calls
// Wait, which is the UI thread when the program follows the package doc.
func startOnUI(f func()) error {
	f()
	return nil
}

func appUIWait() {
	uiThreadID()
	var m msgStruct
	for getMessageW(&m, 0, 0, 0) > 0 {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
}

// appUIWake posts WM_QUIT to the UI thread so a blocked appUIWait returns;
// App.Wait then sees the exit flag and stops.
func appUIWake() {
	postThreadMessageW(uiThreadID(), wmQuit, 0, 0)
}
