// macOS View backend in pure Go via purego's Objective-C runtime.
//
// This backend drives AppKit and WebKit directly, so appkit needs no cgo and
// no bundled native library on macOS.

package tuohi

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/terva-sh/tuohi/dialog"
)

const (
	nsWindowStyleMaskTitled         = 1 << 0
	nsWindowStyleMaskClosable       = 1 << 1
	nsWindowStyleMaskMiniaturizable = 1 << 2
	nsWindowStyleMaskResizable      = 1 << 3

	nsBackingStoreBuffered = 2

	nsApplicationActivationPolicyRegular = 0

	nsEventTypeKeyDown            = 10
	nsEventTypeApplicationDefined = 15
	nsEventMaskAny                = ^uint(0)

	nsViewWidthSizable  = 1 << 1
	nsViewHeightSizable = 1 << 4

	nsModalResponseOK = 1

	// wkPermissionDecisionGrant and wkPermissionDecisionDeny are
	// WKUIDelegate's WKPermissionDecision values (Prompt=0, Grant=1,
	// Deny=2), passed to the media-capture decision handler.
	wkPermissionDecisionGrant = 1
	wkPermissionDecisionDeny  = 2

	// wkMediaCaptureType values: what a media-capture request asks for.
	wkMediaCaptureTypeCamera              = 0
	wkMediaCaptureTypeMicrophone          = 1
	wkMediaCaptureTypeCameraAndMicrophone = 2

	// wkNavigationActionPolicyCancel and wkNavigationActionPolicyAllow are
	// WKNavigationDelegate's WKNavigationActionPolicy values, passed to the
	// navigation decision handler (see decidePolicyForNavigationAction).
	wkNavigationActionPolicyCancel = 0
	wkNavigationActionPolicyAllow  = 1

	// wkNavigationResponsePolicyCancel and wkNavigationResponsePolicyAllow
	// are WKNavigationResponsePolicy values (see
	// decidePolicyForNavigationResponse).
	wkNavigationResponsePolicyCancel = 0
	wkNavigationResponsePolicyAllow  = 1

	// nsURLErrorFileDoesNotExist is Foundation's NSURLErrorFileDoesNotExist,
	// reported to a URL-scheme task when its handler has no resource to serve.
	nsURLErrorFileDoesNotExist = -1100

	wkInjectionTimeAtDocumentStart = 0

	defaultWidth  = 640
	defaultHeight = 480
)

// CGFloat is float64 on 64-bit; these mirror Cocoa geometry structs passed by
// value through objc_msgSend.
type cgPoint struct{ X, Y float64 }
type cgSize struct{ Width, Height float64 }
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

// --- objc helpers ----------------------------------------------------------

var selCache sync.Map // string -> objc.SEL

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
		panic(fmt.Sprintf("appkit: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// cstr reads a NUL-terminated C string returned as an objc.ID (e.g. -UTF8String).
func cstr(id objc.ID) string {
	if id == 0 {
		return ""
	}
	// Reinterpret the objc.ID's bits without a uintptr->Pointer cast (keeps go
	// vet's unsafeptr check quiet); the pointer is C string memory, not a Go
	// pointer.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n)) // #nosec G103 -- slice over the C string buffer
}

// autorelease wraps f in an NSAutoreleasePool and drains the pool afterward.
//
// LockOSThread pins the goroutine for the pool's lifetime: an
// NSAutoreleasePool is thread-local, so if the goroutine migrated between
// creating the pool and the deferred drain, the pool would be drained on the
// wrong thread and corrupt the autorelease stack - an intermittent SIGSEGV.
// The defers run LIFO, so drain happens before UnlockOSThread, i.e. while
// still on the creating thread.
func autorelease(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	f()
}

// --- one-time runtime initialization ---------------------------------------

var (
	initOnce sync.Once
	initErr  error

	dispatchAsyncF func(queue, context, work uintptr)
	mainQueue      uintptr
	dispatchWork   uintptr

	appDelegateClass, scriptHandlerClass, windowDelegateClass, uiDelegateClass objc.Class
	schemeHandlerClass, firstMouseViewClass, borderlessWindowClass             objc.Class
)

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Cocoa.framework/Cocoa",
			"/System/Library/Frameworks/WebKit.framework/WebKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY)
			if err != nil {
				initErr = fmt.Errorf("webview: dlopen %s: %w", fw, err)
				return
			}
		}
		q, err := purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil {
			initErr = fmt.Errorf("webview: resolve _dispatch_main_q: %w", err)
			return
		}
		mainQueue = q
		purego.RegisterLibFunc(&dispatchAsyncF, purego.RTLD_DEFAULT, "dispatch_async_f")
		dispatchWork = purego.NewCallback(func(ctx uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[ctx]
			delete(dispatchMap, ctx)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return 0
		})
		initErr = registerClasses()
	})
	return initErr
}

func registerClasses() error {
	var err error
	appDelegateClass, err = objc.RegisterClass(
		"AppkitAppDelegate", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("NSTouchBarProvider")}, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("applicationShouldTerminateAfterLastWindowClosed:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) bool { return false },
			},
			{
				// Clicking the app's Dock icon while no window is on screen is a
				// "reopen" event. Without this handler a window hidden by the
				// frameless Minimize (orderOut) - or miniaturized into the Dock
				// by the framed path - can never be brought back: AppKit's
				// default reopen handling only creates an untitled document for
				// document-style apps and otherwise does nothing. Restore the
				// appkit window (un-minimize + re-show + make key) so a normal
				// menu-bar/Dock app behaves the way macOS users expect.
				Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"),
				Fn: func(self objc.ID, _cmd objc.SEL, sender objc.ID, hasVisible bool) bool {
					if w := lookupEngine(self); w != nil {
						w.restoreOnReopen()
						return false // we restored the window; stop AppKit's default
					}
					return true // no appkit window to restore; keep the default
				},
			},
			{
				Cmd: sel("applicationDidFinishLaunching:"),
				Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
					w := lookupEngine(self)
					if w != nil {
						w.onApplicationDidFinishLaunching(notification.Send(sel("object")))
					}
				},
			},
		})
	if err != nil {
		return fmt.Errorf("webview: app delegate class: %w", err)
	}

	scriptHandlerClass, err = objc.RegisterClass(
		"AppkitScriptMessageHandler", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("WKScriptMessageHandler")}, nil,
		[]objc.MethodDef{{
			Cmd: sel("userContentController:didReceiveScriptMessage:"),
			Fn: func(self objc.ID, _cmd objc.SEL, ucc objc.ID, message objc.ID) {
				w := lookupEngine(self)
				if w == nil {
					return
				}
				// A page can post any JavaScript value. Only a string is a
				// bridge message, and sending UTF8String to anything else
				// raises an Objective-C exception, which aborts.
				body := message.Send(sel("body"))
				if body == 0 || !objc.Send[bool](body, sel("isKindOfClass:"), class("NSString")) {
					return
				}
				// The bridge is for the top-level page only. A frame, even
				// one on a trusted origin, is refused.
				frame := message.Send(sel("frameInfo"))
				if frame == 0 || !objc.Send[bool](frame, sel("isMainFrame")) {
					return
				}
				// The sender is the URL of the document that posted, from the
				// message's own frame. The web view's current URL would be
				// wrong for a message still queued when the view navigated
				// on to another page. A nil URL names no origin, and the gate
				// refuses it.
				sender := ""
				if req := frame.Send(sel("request")); req != 0 {
					if u := req.Send(sel("URL")); u != 0 {
						sender = cstr(u.Send(sel("absoluteString")).Send(sel("UTF8String")))
					}
				}
				w.onMessage(cstr(body.Send(sel("UTF8String"))), sender, true)
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: script handler class: %w", err)
	}

	windowDelegateClass, err = objc.RegisterClass(
		"AppkitWindowDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("NSWindowDelegate")}, nil,
		[]objc.MethodDef{{
			Cmd: sel("windowWillClose:"),
			Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
				w := lookupEngine(self)
				if w != nil {
					w.onWindowWillClose()
				}
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: window delegate class: %w", err)
	}

	uiDelegateClass, err = objc.RegisterClass(
		"AppkitUIDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKUIDelegate")}, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
				Fn:  runOpenPanel,
			},
			{
				Cmd: sel("webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:"),
				Fn:  requestMediaCapturePermission,
			},
			{
				Cmd: sel("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:"),
				Fn:  createWebView,
			},
			{
				// The same delegate object also serves as the WKNavigationDelegate,
				// which applies the navigation policy.
				Cmd: sel("webView:decidePolicyForNavigationAction:decisionHandler:"),
				Fn:  decidePolicyForNavigationAction,
			},
			{
				Cmd: sel("webView:decidePolicyForNavigationResponse:decisionHandler:"),
				Fn:  decidePolicyForNavigationResponse,
			},
			{
				// webView:didFinishNavigation: is the "page fully loaded" moment
				// View.Ready waits for.
				Cmd: sel("webView:didFinishNavigation:"),
				Fn: func(self objc.ID, _cmd objc.SEL, _webView, _navigation objc.ID) {
					if w := lookupEngine(self); w != nil {
						w.fireReady()
					}
				},
			},
		})
	if err != nil {
		return fmt.Errorf("webview: ui delegate class: %w", err)
	}

	schemeHandlerClass, err = objc.RegisterClass(
		"AppkitURLSchemeHandler", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKURLSchemeHandler")}, nil,
		[]objc.MethodDef{
			{Cmd: sel("webView:startURLSchemeTask:"), Fn: startURLSchemeTask},
			{Cmd: sel("webView:stopURLSchemeTask:"), Fn: stopURLSchemeTask},
		})
	if err != nil {
		return fmt.Errorf("webview: url scheme handler class: %w", err)
	}

	// A borderless NSWindow subclass that answers YES to canBecomeKeyWindow /
	// canBecomeMainWindow. AppKit's NSWindow default returns NO for a window
	// with no title bar (NSWindowStyleMaskBorderless), so a plain borderless
	// window can NEVER become key - makeKeyAndOrderFront: / Raise would order
	// it to the front but it stays non-key, and keyboard input never reaches
	// the web view. Frameless appkit windows (View.Frame false, the
	// default) therefore allocate from this subclass; framed windows keep the
	// plain NSWindow, whose titled style already allows keyness. Subclassing
	// is the only mechanism - there is no window-level or runtime switch for
	// it (same shape as firstMouseViewClass below).
	borderlessWindowClass, err = objc.RegisterClass(
		"AppkitBorderlessWindow", objc.GetClass("NSWindow"), nil, nil,
		[]objc.MethodDef{
			{
				Cmd: sel("canBecomeKeyWindow"),
				Fn:  func(self objc.ID, _cmd objc.SEL) bool { return true },
			},
			{
				Cmd: sel("canBecomeMainWindow"),
				Fn:  func(self objc.ID, _cmd objc.SEL) bool { return true },
			},
		})
	if err != nil {
		return fmt.Errorf("webview: borderless window class: %w", err)
	}

	// A WKWebView that answers YES to acceptsFirstMouse:, used only when
	// View.FirstMouse asks for it. NSView's default is NO, so a click
	// on an inactive window is spent activating it and never reaches the page -
	// the "I had to click twice" complaint. Subclassing is the whole mechanism:
	// AppKit asks the VIEW under the cursor, and there is no window-level or
	// runtime switch for it.
	firstMouseViewClass, err = objc.RegisterClass(
		"AppkitFirstMouseWebView", objc.GetClass("WKWebView"), nil, nil,
		[]objc.MethodDef{{
			Cmd: sel("acceptsFirstMouse:"),
			Fn:  func(self objc.ID, _cmd objc.SEL, event objc.ID) bool { return true },
		}})
	if err != nil {
		return fmt.Errorf("webview: first-mouse web view class: %w", err)
	}
	return nil
}

// startURLSchemeTask implements -webView:startURLSchemeTask:. It resolves the
// owning webview via the scheme-handler object's registry entry, invokes the
// registered app-scope resolver, and feeds the bytes back through the task.
func startURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {
	w := lookupEngine(self)
	if w == nil {
		return
	}
	req := task.Send(sel("request"))
	nsurl := req.Send(sel("URL"))
	urlStr := cstr(nsurl.Send(sel("absoluteString")).Send(sel("UTF8String")))

	resp := callServe(w.serve, &request{URL: urlStr})
	autorelease(func() {
		if resp == nil {
			// WebKit requires a non-nil NSError here; passing nil can raise. A nil
			// response means "not found", so report NSURLErrorFileDoesNotExist.
			nsErr := class("NSError").Send(sel("errorWithDomain:code:userInfo:"),
				nsstr("NSURLErrorDomain"), nsURLErrorFileDoesNotExist, objc.ID(0))
			task.Send(sel("didFailWithError:"), nsErr)
			return
		}
		body := resp.Body
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = unsafe.Pointer(&body[0]) // #nosec G103 -- dataWithBytes:length: copies the buffer
		}
		data := class("NSData").Send(sel("dataWithBytes:length:"), dataPtr, len(body))
		// A 200 NSHTTPURLResponse with a Content-Type header - an http-style
		// response is what makes WebKit treat the custom origin as secure.
		headers := class("NSMutableDictionary").Send(sel("dictionary"))
		headers.Send(sel("setObject:forKey:"), nsstr(schemeMIME(resp)), nsstr("Content-Type"))
		urlResp := class("NSHTTPURLResponse").Send(sel("alloc")).Send(
			sel("initWithURL:statusCode:HTTPVersion:headerFields:"),
			nsurl, 200, nsstr("HTTP/1.1"), headers)
		// alloc/init returns a +1 object; autorelease it so it does not leak once
		// per request (didReceiveResponse: retains what it needs).
		urlResp.Send(sel("autorelease"))
		task.Send(sel("didReceiveResponse:"), urlResp)
		task.Send(sel("didReceiveData:"), data)
		task.Send(sel("didFinish"))
	})
}

// stopURLSchemeTask implements -webView:stopURLSchemeTask: - we complete
// synchronously, so there is nothing to cancel.
func stopURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {}

// runOpenPanel implements WKUIDelegate's file chooser via NSOpenPanel, invoking
// the completion handler block (driven through NSInvocation) with the URLs.
func runOpenPanel(self objc.ID, _cmd objc.SEL, webView, parameters, frame, completionHandler objc.ID) {
	autorelease(func() {
		allowsMultiple := parameters.Send(sel("allowsMultipleSelection")) != 0
		allowsDirs := parameters.Send(sel("allowsDirectories")) != 0

		panel := class("NSOpenPanel").Send(sel("openPanel"))
		configureOpenPanel(panel, true, allowsDirs, allowsMultiple, dialog.Options{})

		var urls objc.ID
		if int(panel.Send(sel("runModal"))) == nsModalResponseOK { // #nosec G115 -- NSModalResponse is a small int
			urls = panel.Send(sel("URLs"))
		}
		invokeOpenPanelCompletion(completionHandler, urls)
	})
}

// configureOpenPanel applies the open-panel settings shared by the WKUIDelegate
// <input type=file> chooser (the View Dialog method delegates to the dialog
// package instead). opts.Title becomes the panel's message, opts.Directory the
// initial folder, and the flattened Filters / Extensions the allowed file
// types.
func configureOpenPanel(panel objc.ID, canFiles, canDirs, multiple bool, opts dialog.Options) {
	panel.Send(sel("setCanChooseFiles:"), canFiles)
	panel.Send(sel("setCanChooseDirectories:"), canDirs)
	panel.Send(sel("setAllowsMultipleSelection:"), multiple)
	if opts.Title != "" {
		// On modern macOS the panel has no title-bar text; setMessage shows the
		// label prominently above the file list, which is the visible spot.
		panel.Send(sel("setMessage:"), nsstr(opts.Title))
	}
	if opts.Directory != "" {
		url := class("NSURL").Send(sel("fileURLWithPath:"), nsstr(opts.Directory))
		panel.Send(sel("setDirectoryURL:"), url)
	}
	types := openPanelTypes(opts)
	if types != 0 {
		panel.Send(sel("setAllowedFileTypes:"), types)
	}
}

// openPanelTypes flattens an Config' Filters and Extensions into an
// NSArray<NSString*> of bare extensions for setAllowedFileTypes:, or 0 (no
// restriction) when there are none or any entry is a wildcard.
func openPanelTypes(opts dialog.Options) objc.ID {
	var exts []string
	for _, f := range opts.Filters {
		exts = append(exts, f.Extensions...)
	}
	exts = append(exts, opts.Extensions...)
	var clean []string
	for _, e := range exts {
		e = strings.TrimPrefix(e, ".")
		if e == "" || e == "*" {
			return 0
		}
		clean = append(clean, e)
	}
	if len(clean) == 0 {
		return 0
	}
	arr := class("NSMutableArray").Send(sel("array"))
	for _, e := range clean {
		arr.Send(sel("addObject:"), nsstr(e))
	}
	return arr
}

// invokeOpenPanelCompletion calls the WKWebView open-panel completion block with
// the selected URLs (or nil when cancelled). The handler is an opaque block, so
// it is driven through NSInvocation with the signature "v@?@": index 0 is the
// block itself, index 1 the NSArray<NSURL*>* argument.
func invokeOpenPanelCompletion(completionHandler, urls objc.ID) {
	sig := class("NSMethodSignature").Send(sel("signatureWithObjCTypes:"), "v@?@")
	inv := class("NSInvocation").Send(sel("invocationWithMethodSignature:"), sig)
	inv.Send(sel("setTarget:"), completionHandler)
	inv.Send(sel("setArgument:atIndex:"), unsafe.Pointer(&urls), 1) // #nosec G103 -- pass the arg's address to NSInvocation
	inv.Send(sel("invoke"))
}

// requestMediaCapturePermission implements WKUIDelegate's
// requestMediaCapturePermission implements WKUIDelegate's
// webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:
// decisionHandler: (macOS 12+). WKMediaCaptureType only ever reports camera,
// microphone, or camera-and-microphone requests; display capture is not
// surfaced through this delegate. Each is decided by the view's policy
// (viewCore.permits) for origin, the security origin of the frame asking,
// and a type this code does not know is denied. A program that grants the
// camera or the microphone still needs NSCameraUsageDescription or
// NSMicrophoneUsageDescription in its Info.plist.
func requestMediaCapturePermission(self objc.ID, _cmd objc.SEL, webView, origin, frame objc.ID, captureType int, decisionHandler objc.ID) {
	autorelease(func() {
		decision := wkPermissionDecisionDeny
		var perms []Permission
		switch captureType {
		case wkMediaCaptureTypeCamera:
			perms = []Permission{PermissionCamera}
		case wkMediaCaptureTypeMicrophone:
			perms = []Permission{PermissionMicrophone}
		case wkMediaCaptureTypeCameraAndMicrophone:
			perms = []Permission{PermissionCamera, PermissionMicrophone}
		}
		if w := lookupEngine(self); w != nil && w.permits(securityOriginURL(origin), perms...) {
			decision = wkPermissionDecisionGrant
		}
		invokeDecisionHandler(decisionHandler, decision)
	})
}

// securityOriginURL writes a WKSecurityOrigin as a URL whose origin is that
// one (see originURL). A nil origin gives "".
func securityOriginURL(origin objc.ID) string {
	if origin == 0 {
		return ""
	}
	return originURL(cstr(origin.Send(sel("protocol")).Send(sel("UTF8String"))),
		cstr(origin.Send(sel("host")).Send(sel("UTF8String"))), int(origin.Send(sel("port"))))
}

// decidePolicyForNavigationAction implements WKNavigationDelegate's
// webView:decidePolicyForNavigationAction:decisionHandler:, which WKWebView
// asks before every navigation, and calls decisionHandler exactly once:
//   - a navigation with no target frame asks for a new window, which is never
//     opened: see handleNewWindow;
//   - a frame's navigation is allowed, because the policy leaves frames alone;
//   - a main-frame navigation is allowed only when the navigation policy lets
//     it proceed, and is otherwise cancelled and refused.
//
// Unlike WebKitGTK, WKWebView names the target frame, so the decision is made
// before any request is sent.
func decidePolicyForNavigationAction(self objc.ID, _cmd objc.SEL, _webView, action, decisionHandler objc.ID) {
	autorelease(func() {
		w := lookupEngine(self)
		if w == nil {
			invokeDecisionHandler(decisionHandler, wkNavigationActionPolicyAllow)
			return
		}
		uri := navigationActionURL(action)
		frame := action.Send(sel("targetFrame"))
		switch {
		case frame == 0:
			invokeDecisionHandler(decisionHandler, wkNavigationActionPolicyCancel)
			w.handleNewWindow(uri)
		case !objc.Send[bool](frame, sel("isMainFrame")):
			invokeDecisionHandler(decisionHandler, wkNavigationActionPolicyAllow)
		default:
			if a := w.navigationPolicy(uri); a != navProceed {
				invokeDecisionHandler(decisionHandler, wkNavigationActionPolicyCancel)
				refuseNavigation(uri, a)
				return
			}
			invokeDecisionHandler(decisionHandler, wkNavigationActionPolicyAllow)
		}
	})
}

// decidePolicyForNavigationResponse implements WKNavigationDelegate's
// webView:decidePolicyForNavigationResponse:decisionHandler:, the backstop for
// a server redirect: it applies the navigation policy again to the final URL
// of the main frame's response. A navigation the action check already
// cancelled has no response, so nothing is judged twice. Whether WKWebView
// asks decidePolicyForNavigationAction before following a redirect is not
// something this code relies on.
func decidePolicyForNavigationResponse(self objc.ID, _cmd objc.SEL, _webView, response, decisionHandler objc.ID) {
	autorelease(func() {
		w := lookupEngine(self)
		if w == nil || !objc.Send[bool](response, sel("isForMainFrame")) {
			invokeDecisionHandler(decisionHandler, wkNavigationResponsePolicyAllow)
			return
		}
		uri := ""
		if r := response.Send(sel("response")); r != 0 {
			if u := r.Send(sel("URL")); u != 0 {
				uri = cstr(u.Send(sel("absoluteString")).Send(sel("UTF8String")))
			}
		}
		// A response with no URL has nothing to judge; the action check
		// already saw its request.
		if uri == "" {
			invokeDecisionHandler(decisionHandler, wkNavigationResponsePolicyAllow)
			return
		}
		if a := w.navigationPolicy(uri); a != navProceed {
			invokeDecisionHandler(decisionHandler, wkNavigationResponsePolicyCancel)
			refuseNavigation(uri, a)
			return
		}
		invokeDecisionHandler(decisionHandler, wkNavigationResponsePolicyAllow)
	})
}

// createWebView implements WKUIDelegate's
// webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:,
// which WKWebView calls when a page asks for a new window. It returns nil, so
// no window opens, and handleNewWindow decides what loads instead.
func createWebView(self objc.ID, _cmd objc.SEL, _webView, _config, action, _features objc.ID) objc.ID {
	autorelease(func() {
		if w := lookupEngine(self); w != nil {
			w.handleNewWindow(navigationActionURL(action))
		}
	})
	return 0
}

// navigationActionURL returns the absolute URL a WKNavigationAction's request
// is for, or "" when it has none.
func navigationActionURL(action objc.ID) string {
	req := action.Send(sel("request"))
	if req == 0 {
		return ""
	}
	u := req.Send(sel("URL"))
	if u == 0 {
		return ""
	}
	return cstr(u.Send(sel("absoluteString")).Send(sel("UTF8String")))
}

// invokeDecisionHandler calls a WebKit decision block that takes one NSInteger
// enum: the media-capture handler's WKPermissionDecision, or the navigation
// handler's WKNavigationActionPolicy. The handler is an opaque block, so it is
// driven through NSInvocation with the signature "v@?q": index 0 is the block
// itself, index 1 the NSInteger decision argument.
func invokeDecisionHandler(decisionHandler objc.ID, decision int) {
	sig := class("NSMethodSignature").Send(sel("signatureWithObjCTypes:"), "v@?q")
	inv := class("NSInvocation").Send(sel("invocationWithMethodSignature:"), sig)
	inv.Send(sel("setTarget:"), decisionHandler)
	inv.Send(sel("setArgument:atIndex:"), unsafe.Pointer(&decision), 1) // #nosec G103 -- pass the arg's address to NSInvocation
	inv.Send(sel("invoke"))
}

// --- instance registry (replaces objc associated objects) ------------------

var (
	regMu    sync.Mutex
	registry = map[objc.ID]*webview{}
)

func registerEngine(id objc.ID, w *webview) {
	regMu.Lock()
	registry[id] = w
	regMu.Unlock()
}

func unregisterEngine(id objc.ID) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id objc.ID) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

// --- libdispatch -----------------------------------------------------------

var (
	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	dispatchAsyncF(mainQueue, id, dispatchWork)
}

// onMainThread reports whether the caller runs on the process main thread -
// the only thread AppKit accepts UI work from.
func onMainThread() bool {
	return class("NSThread").Send(sel("isMainThread")) != 0
}

// uiIsMain records, at first webview creation, whether the UI runs on the
// process main thread. True in both supported shapes: creation on the main
// thread (the normal contract), and creation marshaled to the main thread
// because an external owner's run loop (e.g. the tray package's) is already
// there. False only when the whole UI lifecycle is pinned to a secondary
// thread whose main dispatch queue is never drained - marshaling to the main
// thread would hang, so every call runs inline instead (see onUIThread).
var (
	uiIsMainOnce sync.Once
	uiIsMain     atomic.Bool
)

// onUIThread is the dispatcher's onUI hook (see uiDispatcher). The UI thread
// is the main thread, except when uiIsMain is false: then there is no other
// thread to defer to, which includes the time before the first webview.
//
// A first webview created off the main thread with no loop running leaves
// uiIsMain false for good, and every goroutine then counts as the UI thread:
// nothing drains a queue on the thread that created the view, so there is
// nowhere to marshal to. That shape runs AppKit off the main thread whatever
// tuohi does, and TKT-01M3J59M5V12QW1WRBEJPJ5H38 (Make the macOS main-thread
// rule explicit and enforced) refuses it.
func onUIThread() bool {
	return onMainThread() || !uiIsMain.Load()
}

// postUI is the dispatcher's post hook. The main dispatch queue always takes
// work, so it never refuses.
func postUI(f func()) bool {
	dispatchMain(f)
	return true
}

// uiLoopExternal is the dispatcher's external hook: NSApp runs, but not
// through tuohi (appkitRunsLoop), so the tray package or an embedding host
// owns the loop that drains the main queue.
func uiLoopExternal() bool {
	app := class("NSApplication").Send(sel("sharedApplication"))
	return app.Send(sel("isRunning")) != 0 && !appkitRunsLoop.Load()
}

// performOnMain runs f on the UI thread and waits for it, running inline when
// the caller is already there. Off the UI thread it queues onto the main
// dispatch queue and waits until a run loop drains it. When no loop is
// running, or the loop stops before f starts, f is dropped and performOnMain
// returns rather than wait for a loop that may never come (see
// uiDispatcher.call).
func performOnMain(f func()) {
	_ = ui.call(f)
}

// --- process-wide lifecycle bookkeeping ------------------------------------

var (
	firstMu      sync.Mutex
	notFirst     bool
	windowCount  int32
	uiThreadOnce sync.Once

	// appkitRunsLoop is true while OUR Run() drives [NSApp run]. When the
	// loop belongs to someone else (e.g. the tray package's Run started it
	// before the first webview existed), it stays false: closing the last
	// appkit window must not stop a loop we do not own, and Terminate must
	// not stop it either.
	appkitRunsLoop atomic.Bool
)

func claimFirstInstance() bool {
	firstMu.Lock()
	defer firstMu.Unlock()
	if notFirst {
		return false
	}
	notFirst = true
	return true
}

func incWindowCount()       { atomic.AddInt32(&windowCount, 1) }
func decWindowCount() int32 { return atomic.AddInt32(&windowCount, -1) }

// --- webview ---------------------------------------------------------------

// webview is the macOS implementation behind the View struct.
type webview struct {
	app            objc.ID
	appDelegate    objc.ID
	windowDelegate objc.ID
	uiDelegate     objc.ID
	window         objc.ID
	widget         objc.ID
	webView        objc.ID
	manager        objc.ID
	scriptHandler  objc.ID

	ownsWindow bool
	// firstMouse makes a click on an INACTIVE window reach the page instead of
	// only bringing the window forward. See Config.FirstMouse.
	firstMouse bool
	// frameless windows have no title bar/frame and a fully transparent
	// background (View.Frame is false); the page's -app-region
	// boxes decide which parts move the window (via -[NSWindow
	// performWindowDragWithEvent:]), and the resizable style mask keeps the
	// native edge/corner resizing with its resize cursors.
	frameless bool
	// lastWidth/lastHeight remember the content size so a synthesized drag
	// event can be placed in window coordinates (the legacy path avoided
	// struct-returning frame reads; see applyGeometry and objc.Send[cgRect]
	// for the window-frame reads used by manual zoom).
	lastWidth, lastHeight int
	// Frameless windows can't use AppKit's built-in zoom/miniaturize, so the
	// engine tracks the manual zoom/hide state itself (see Maximize/Unmaximize
	// and Minimize/Unminimize). savedFrame holds the pre-maximize window frame
	// so Unmaximize can restore it exactly; frameless Minimize is implemented
	// as a hide (orderOut) because a borderless NSWindow has no Dock-miniature.
	savedFrame cgRect
	maximized  bool
	minimized  bool

	isSizeSet bool

	// closed is closed when this window goes away (user close or Destroy);
	// Run() waits on it instead of re-running NSApp when the run loop already
	// belongs to someone else. closeOnce makes the two close paths safe.
	closed    chan struct{}
	closeOnce sync.Once

	viewCore

	// schemeHandlerObjs are the WKURLSchemeHandler delegate object (one per
	// view), kept so Destroy can drop its instance-registry entry - the
	// engine would otherwise stay pinned in the registry after Destroy.
	schemeHandlerObjs []objc.ID
}

func newWebView(v *View, serve serveFunc, app objc.ID, loopRunning bool) *webview {
	w := &webview{
		ownsWindow: true,
		firstMouse: v.FirstMouse,
		frameless:  !v.Frame,
		bindings:   map[string]binding{},
		serve:      serve,
		closed:     make(chan struct{}),
		lastWidth:  defaultWidth,
		lastHeight: defaultHeight,
	}
	w.app = app
	w.windowInit(objc.ID(uintptr(v.window)))
	// Window settings: create the WKWebViewConfiguration, tune its
	// preferences and build the web view, all before the WKWebView is
	// initialised (WKWebView copies its configuration at init, so nothing
	// can change afterwards). The pushed values mirror WKWebView's native
	// WKPreferences defaults - javaScriptEnabled YES, fullScreenEnabled NO
	// (appkit's one tuned divergence: it enables fullscreen so the demo's
	// <video> can go fullscreen), deprecated javaEnabled/plugInsEnabled NO -
	// except developerExtrasEnabled, which tracks the view's resolved Debug
	// flag (View.Debug OR App.Debug / APPKIT_DEBUG). Every write goes
	// through KVC guarded by respondsToSelector: on the property's setter,
	// so a preference the running macOS does not know (newer or removed
	// properties, e.g. javaEnabled after 10.15) is skipped instead of
	// raising NSUnknownKeyException.
	autorelease(func() {
		rect := cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}}

		config := class("WKWebViewConfiguration").Send(sel("new"))
		config.Send(sel("autorelease"))
		w.manager = config.Send(sel("userContentController"))

		prefs := config.Send(sel("preferences"))
		devTools := v.Debug
		num := func(b bool) objc.ID {
			return class("NSNumber").Send(sel("numberWithBool:"), b)
		}
		numF := func(f float64) objc.ID {
			return class("NSNumber").Send(sel("numberWithDouble:"), f)
		}
		push := func(key, setter string, v objc.ID) {
			if prefs.Send(sel("respondsToSelector:"), sel(setter)) != 0 {
				prefs.Send(sel("setValue:forKey:"), v, nsstr(key))
			}
		}
		push("javaScriptEnabled", "setJavaScript:", num(true))
		push("fullScreenEnabled", "setFullScreenEnabled:", num(true)) // appkit's tuned default (native NO)
		push("developerExtrasEnabled", "setDeveloperExtrasEnabled:", num(devTools))
		push("javaScriptCanOpenWindowsAutomatically", "setJavaScriptCanOpenWindowsAutomatically:", num(true))
		push("minimumFontSize", "setMinimumFontSize:", numF(0))
		push("tabFocusesLinks", "setTabFocusesLinks:", num(false))
		push("textInteractionEnabled", "setTextInteractionEnabled:", num(true))
		push("siteSpecificQuirksModeEnabled", "setSiteSpecificQuirksModeEnabled:", num(true))
		push("elementFullscreenEnabled", "setElementFullscreenEnabled:", num(false))
		push("fraudulentWebsiteWarningEnabled", "setFraudulentWebsiteWarningEnabled:", num(true))
		push("shouldPrintBackgrounds", "setShouldPrintBackgrounds:", num(false))
		push("javaEnabled", "setJavaEnabled:", num(false))
		push("plugInsEnabled", "setPlugInsEnabled:", num(false))

		// Register the app scheme handler on the configuration BEFORE the
		// WKWebView is created - WKWebView copies its configuration at init, so
		// this cannot be done afterward. The single handler object is mapped
		// back to this webview via the instance registry.
		if w.serve != nil {
			sh := objc.ID(schemeHandlerClass).Send(sel("new"))
			// Autorelease the +1 from -new; the configuration retains it (matching
			// scriptHandler). Track it so Destroy can drop its registry entry.
			sh.Send(sel("autorelease"))
			registerEngine(sh, w)
			w.schemeHandlerObjs = append(w.schemeHandlerObjs, sh)
			config.Send(sel("setURLSchemeHandler:forURLScheme:"), sh, nsstr(appSchemeName))
		}

		// The first-mouse variant is a WKWebView subclass, so everything below
		// treats it as one; only the class allocated differs.
		viewClass := objc.Class(class("WKWebView"))
		if w.firstMouse {
			viewClass = firstMouseViewClass
		}
		wv := objc.ID(viewClass).Send(sel("alloc"))
		wv = wv.Send(sel("initWithFrame:configuration:"), rect, config)
		w.webView = wv.Send(sel("retain"))
		w.webView.Send(sel("setAutoresizingMask:"), uint(nsViewWidthSizable|nsViewHeightSizable))
		// setInspectable: exists from macOS 13.3. Sent to an older WKWebView
		// it raises an unrecognized-selector exception, which aborts.
		if devTools && objc.Send[bool](w.webView, sel("respondsToSelector:"), sel("setInspectable:")) {
			w.webView.Send(sel("setInspectable:"), true)
		}

		// Frameless windows are fully transparent by default: mark the window
		// non-opaque with a clear background and stop the web view from
		// drawing its own, so transparent page areas reveal the desktop behind
		// the window (the page's html/body can set its own background). Frame
		// windows keep the OS's opaque window.
		if w.frameless {
			w.window.Send(sel("setOpaque:"), false)
			clear := class("NSColor").Send(sel("clearColor"))
			w.window.Send(sel("setBackgroundColor:"), clear)
			w.webView.Send(sel("setValue:forKey:"),
				class("NSNumber").Send(sel("numberWithBool:"), false), nsstr("drawsBackground"))
		}

		// UIDelegate is a weak reference; keep our own strong ref in w.uiDelegate.
		w.uiDelegate = objc.ID(uiDelegateClass).Send(sel("new"))
		registerEngine(w.uiDelegate, w) // for webView:didFinishNavigation: (Ready)
		w.webView.Send(sel("setUIDelegate:"), w.uiDelegate)
		w.webView.Send(sel("setNavigationDelegate:"), w.uiDelegate)

		handler := objc.ID(scriptHandlerClass).Send(sel("new"))
		registerEngine(handler, w)
		handler.Send(sel("autorelease"))
		w.scriptHandler = handler // kept so Destroy can drop its registry entry
		w.manager.Send(sel("addScriptMessageHandler:name:"), handler, nsstr("__webview__"))

		w.mu.Lock()
		w.rebuildScriptsLocked() // installs the bridge
		w.mu.Unlock()

		widget := class("NSView").Send(sel("alloc")).Send(sel("initWithFrame:"), rect)
		w.widget = widget.Send(sel("retain"))
		w.widget.Send(sel("setAutoresizesSubviews:"), true)
		w.widget.Send(sel("addSubview:"), w.webView)

		w.window.Send(sel("setContentView:"), w.widget)
		if w.ownsWindow {
			w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
			// The content view is a plain NSView container, and a plain NSView
			// REFUSES first-responder status - so when the window becomes key,
			// AppKit's offer stops at the window itself and every keystroke is
			// an unhandled key: the system beep. A click fixed it only because
			// hit-testing hands the WKWebView the responder role. Hand it over
			// at birth instead, so a freshly opened window types.
			w.window.Send(sel("makeFirstResponder:"), w.webView)
		}
	})
	if w.frameless && w.ownsWindow {
		// Track the page's -app-region boxes; a mouse-down inside a
		// "drag" box (outside any "no-drag" box) requests a native window
		// move. The tracker runs at document-start of every navigation so the
		// regions follow the content.
		w.pushUserScript(createAppRegionScript(v.State != StateFixed, false, "darwin"))
	}
	if loopRunning && w.ownsWindow {
		// The loop's owner picked the activation policy (a tray app runs as
		// Accessory, no Dock icon - leave that alone); activate so the new
		// window actually fronts instead of opening behind the current app.
		w.Raise()
	}
	if w.ownsWindow {
		// Apply the creation-time geometry (the View fields; the backend
		// default size when Width/Height are zero). Window geometry is fixed
		// after creation - there is no runtime move/resize API.
		w.applyGeometry(v)
	}
	// Per-view serving origin (macOS: app content always loads over the
	// loopback http://localhost origin - WKWebView cannot make a custom
	// scheme a secure context - see viewContentBase): start the window's
	// temporary loopback server when App.FS is set, else serve app://
	// through the custom scheme handler. A server that fails to start
	// degrades to scheme serving.
	if base, tr, err := viewContentBase(v, true); err == nil {
		w.contentBase, w.transient = base, tr
	} else if tr != nil {
		stopLoopback(tr)
	}
	return w
}

func (w *webview) windowInit(window objc.ID) {
	autorelease(func() {
		if window != 0 {
			w.window = window
			w.ownsWindow = false
			return
		}
		// The bootstrap below exists to finish launching the app: it installs
		// an app delegate and spins a temporary [NSApp run] that the delegate
		// stops from applicationDidFinishLaunching. If the run loop is already
		// running (an external owner such as the tray package started it), the
		// app finished launching long ago - that notification will never fire
		// again and the temporary run would block forever. Skip straight to
		// window creation.
		if w.app.Send(sel("isRunning")) != 0 || !claimFirstInstance() {
			w.windowInitProceed()
			return
		}
		w.appDelegate = objc.ID(appDelegateClass).Send(sel("new"))
		registerEngine(w.appDelegate, w)
		w.app.Send(sel("setDelegate:"), w.appDelegate)
		// Temporary run loop: returns once applicationDidFinishLaunching stops it.
		w.app.Send(sel("run"))
	})
}

func (w *webview) onApplicationDidFinishLaunching(app objc.ID) {
	if w.ownsWindow {
		w.stopRunLoop()
	}
	if !isAppBundled() {
		app.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
		app.Send(sel("activateIgnoringOtherApps:"), true)
	}
	// The Dock builds its process tile when the app finishes launching; the
	// App.Icon applied earlier in App.start (before the run loop) is ignored,
	// so re-apply it now that the Dock connection is live.
	reapplyAppIcon()
	w.windowInitProceed()
}

func (w *webview) windowInitProceed() {
	autorelease(func() {
		// Frameless windows must allocate from borderlessWindowClass: a plain
		// NSWindow with no title bar cannot become key (see registerClasses),
		// and every window needs keyness - Raise, Show and the initial
		// makeKeyAndOrderFront below all depend on it.
		winClass := objc.Class(class("NSWindow"))
		if w.frameless {
			winClass = borderlessWindowClass
		}
		win := objc.ID(winClass).Send(sel("alloc"))
		style := uint(nsWindowStyleMaskTitled)
		if w.frameless {
			// NSWindowStyleMaskBorderless == 0: no title bar or system buttons.
			// Resizability (and the resize geometry) is applied later in
			// applySize. (Maximize/minimize for a borderless window are driven
			// manually in the engine - AppKit only wires built-in zoom and
			// Dock-miniaturization for titled windows, so initiating the style
			// bits here is not what enables them.)
			style = 0
		}
		win = win.Send(sel("initWithContentRect:styleMask:backing:defer:"),
			cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}},
			style, nsBackingStoreBuffered, false)
		w.window = win.Send(sel("retain"))
		w.windowDelegate = objc.ID(windowDelegateClass).Send(sel("new"))
		registerEngine(w.windowDelegate, w)
		w.window.Send(sel("setDelegate:"), w.windowDelegate)
		incWindowCount()
	})
}

func (w *webview) stopRunLoop() {
	autorelease(func() {
		w.app.Send(sel("stop:"), objc.ID(0))
		postWakeEvent(w.app)
	})
}

// postWakeEvent posts a no-op application-defined event so a thread blocked in
// nextEventMatchingMask wakes up (stop: alone only takes effect after an event).
func postWakeEvent(app objc.ID) {
	event := class("NSEvent").Send(
		sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(sel("postEvent:atStart:"), event, true)
}

func (w *webview) onWindowWillClose() {
	if w.ownsWindow {
		// Single per-window close event for the App scope (App.Wait).
		appWindowClosed()
	}
	w.widget = 0
	w.webView = 0
	w.window = 0
	w.closeOnce.Do(func() { close(w.closed) })
	dispatchMain(func() { w.onWindowDestroyed(false) })
}

func (w *webview) onWindowDestroyed(skipTermination bool) {
	if !skipTermination && w.windowDelegate != 0 {
		// Closed via the OS, not Destroy(): drop the delegate->engine mapping so
		// the webview is not pinned in the registry when Destroy() is never
		// called. The objc object is still released by a later Destroy() if any
		// (the map delete is idempotent); a stray delegate callback resolves to
		// nil and no-ops.
		unregisterEngine(w.windowDelegate)
	}
	// Last owned window gone: stop the loop - but only when Run() drives it.
	// An external owner's loop (for example the tray package's) outlives every
	// appkit window.
	if decWindowCount() <= 0 && !skipTermination && appkitRunsLoop.Load() {
		w.Terminate()
	}
}

func isAppBundled() bool {
	bundle := class("NSBundle").Send(sel("mainBundle"))
	if bundle == 0 {
		return false
	}
	path := bundle.Send(sel("bundlePath"))
	return path.Send(sel("hasSuffix:"), nsstr(".app")) != 0
}

// --- public API (the View struct methods) ----------------------------------------

func (w *webview) Run() {
	if w.app.Send(sel("isRunning")) != 0 {
		// A run loop is already active - an external owner's (for example the
		// tray package's) or our own driving another window. Re-running NSApp
		// would fight it, so
		// Run means "until THIS window closes". Off the UI thread, waiting on
		// the channel is enough; on it (a Run inside a tray OnClick or another
		// run-loop callout), block-waiting would starve the loop that must
		// deliver the close, so pump events until the window goes away.
		if onMainThread() {
			w.pumpUntilClosed()
			return
		}
		<-w.closed
		return
	}
	ui.enterLoop()
	defer ui.exitLoop()
	appkitRunsLoop.Store(true)
	w.app.Send(sel("run"))
	appkitRunsLoop.Store(false)
}

// pumpUntilClosed services the event queue on the UI thread until this window
// closes - a nested, modal-style loop for a Run() issued from inside a run-loop
// callout (e.g. a tray menu handler).
func (w *webview) pumpUntilClosed() {
	for {
		select {
		case <-w.closed:
			return
		default:
		}
		autorelease(func() {
			// A short wait instead of distantFuture: the close can arrive from
			// a plain goroutine (Terminate), and a nested pump cannot count on
			// the main dispatch queue for a wake-up - when the pump itself runs
			// inside a main-queue callout, libdispatch will not drain that
			// queue again until the callout returns. Polling the channel every
			// 50ms is boring and deterministic.
			deadline := class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.05)
			ev := w.app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, deadline, nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

// Terminate stops the run loop. Per the View contract it is safe to call from
// a background thread, so the AppKit calls in stopRunLoop are routed to the main
// thread (bindings run on goroutines), matching the Linux/Windows backends.
//
// When the run loop belongs to someone else (for example the tray package's),
// stopping it would kill the owner's app; Terminate then only ends this
// webview's Run wait, and the caller's Destroy closes the window.
func (w *webview) Terminate() {
	if w.app.Send(sel("isRunning")) != 0 && !appkitRunsLoop.Load() {
		// Closing the channel is enough for both Run shapes: the channel wait
		// returns at once, and the pump polls it (see pumpUntilClosed for why
		// a queued wake-up could not be trusted here).
		w.closeOnce.Do(func() { close(w.closed) })
		return
	}
	dispatchMain(w.stopRunLoop)
}

func (w *webview) Dispatch(f func()) { dispatchMain(f) }
func (w *webview) Window() unsafe.Pointer {
	id := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&id)) // #nosec G103 -- reinterpret the objc.ID's bits as the window pointer
}

func (w *webview) Focus() {
	if w.window == 0 || w.webView == 0 {
		return
	}
	// Largely redundant: an NSWindow makes its content view the first responder
	// when it becomes key, and restores it on re-activation. Kept as the explicit,
	// on-demand path and to mirror the other backends.
	performOnMain(func() {
		autorelease(func() { w.window.Send(sel("makeFirstResponder:"), w.webView) })
	})
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			// Both halves are needed and neither substitutes for the other:
			// activateIgnoringOtherApps brings the APPLICATION forward (without it
			// the window rises inside an app that is still in the background, and
			// the click that follows is still spent activating), and
			// makeKeyAndOrderFront brings THIS window forward within the app.
			w.app.Send(sel("activateIgnoringOtherApps:"), true)
			w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		})
	})
}

// Show makes a hidden or miniaturized window visible again and brings it to
// the front (see the View doc). Must run on the main thread.
func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.window.Send(sel("isMiniaturized")) != 0 {
				w.window.Send(sel("deminiaturize:"), objc.ID(0))
			}
			if w.frameless {
				// Frameless Minimize hides via orderOut and tracks minimized in
				// the engine; Show is what brings it back and clears that state.
				w.minimized = false
			}
			w.app.Send(sel("activateIgnoringOtherApps:"), true)
			w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		})
	})
}

// Hide removes the window from the screen (orderOut: detaches it from the
// window list, unlike a miniaturize which would keep a Dock tile).
func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() { w.window.Send(sel("orderOut:"), objc.ID(0)) })
	})
}

// Maximize enlarges the window to fill the screen's visible area.
//
// Frame windows use the native zoom (performZoom:, which toggles). A
// BORDERLESS (frameless) window cannot use AppKit's performZoom: - the zoom
// machinery is only wired for titled windows - so maximization there is done
// manually: the current window frame is remembered and the window is resized
// to the visible frame of the screen it sat on. Toggling (already maximized)
// restores that saved frame.
func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.frameless {
				if w.maximized {
					w.doUnmaximizeFrameless()
					return
				}
				w.doMaximizeFrameless()
				return
			}
			w.window.Send(sel("performZoom:"), objc.ID(0))
		})
	})
}

// doMaximizeFrameless resizes a borderless window to its screen's visible
// frame, remembering the frame it is zooming out of so Unmaximize can restore
// it.
func (w *webview) doMaximizeFrameless() {
	if w.window == 0 {
		return
	}
	if w.minimized {
		w.doUnminimizeFrameless()
	}
	// Frame (NSRect) is read as a struct via objc_return (objc_msgSend_stret).
	w.savedFrame = objc.Send[cgRect](w.window, sel("frame"))
	target := w.visibleFrameFor(w.savedFrame)
	w.window.Send(sel("setFrame:display:"), target, true)
	w.maximized = true
}

// visibleFrameFor returns the NSScreen.visibleFrame of the screen that
// currently holds the window's midpoint (frame), falling back to the main
// screen when nothing is found. A full maximized borderless window uses this
// so the Dock and menu bar stay reachable.
func (w *webview) visibleFrameFor(frame cgRect) cgRect {
	screensSel := sel("screens")
	scr := class("NSScreen").Send(sel("mainScreen"))
	if screens := class("NSScreen").Send(screensSel); screens != 0 {
		n := int(screens.Send(sel("count")))
		midX := frame.Origin.X + frame.Size.Width/2
		midY := frame.Origin.Y + frame.Size.Height/2
		for i := 0; i < n; i++ {
			s := screens.Send(sel("objectAtIndex:"), i)
			f := objc.Send[cgRect](s, sel("frame"))
			if midX >= f.Origin.X && midX < f.Origin.X+f.Size.Width &&
				midY >= f.Origin.Y && midY < f.Origin.Y+f.Size.Height {
				scr = s
				break
			}
		}
	}
	if scr == 0 {
		return frame
	}
	return objc.Send[cgRect](scr, sel("visibleFrame"))
}

// Unmaximize restores a maximized window to its normal size. Frameless windows
// undo the manual zoom (see Maximize); framed windows are a no-op unless the
// native zoom is active (performZoom: toggles back, we only call it when
// isZoomed - there is no unzoom: selector).
func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.frameless {
				w.doUnmaximizeFrameless()
				return
			}
			if w.window.Send(sel("isZoomed")) != 0 {
				w.window.Send(sel("performZoom:"), objc.ID(0))
			}
		})
	})
}

// setTitle sets the window's title (see applyTitle), on the main thread. A
// borderless window draws no title bar, but the title still names it in the
// Window menu, Mission Control and accessibility tools.
func (w *webview) setTitle(title string) {
	if w.window == 0 {
		return
	}
	autorelease(func() {
		w.window.Send(sel("setTitle:"), nsstr(title))
	})
}

// doUnmaximizeFrameless restores a borderless window to the frame it had
// before Maximize put it into the visible frame.
func (w *webview) doUnmaximizeFrameless() {
	if w.window == 0 {
		return
	}
	if !w.maximized {
		return
	}
	w.window.Send(sel("setFrame:display:"), w.savedFrame, true)
	w.maximized = false
}

// Minimize shrinks the window away. A framed window is miniaturized into the
// Dock. A BORDERLESS (frameless) window cannot Dock-miniaturize (that too is
// a titled-window feature), so minimize there is implemented as hiding the
// window (orderOut:) - restore with Unminimize or Show (see View.Show).
func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.frameless {
				if w.maximized {
					w.doUnmaximizeFrameless()
				}
				w.window.Send(sel("orderOut:"), objc.ID(0))
				w.minimized = true
				return
			}
			w.window.Send(sel("performMiniaturize:"), objc.ID(0))
		})
	})
}

// restoreOnReopen brings this window back when the user clicks the app's Dock
// icon while nothing of it is on screen (the "reopen" event, handled by the
// AppkitAppDelegate's applicationShouldHandleReopen:hasVisibleWindows:). A
// frameless Minimize hides the window via orderOut with no Dock-miniature, and
// a framed Minimize docks it as a miniature, so this performs the same
// recovery a tray's Show menu item would: un-minimize if needed, then re-show
// and make the window key. It runs on the main thread (the AppKit delegate
// calls it from there).
func (w *webview) restoreOnReopen() {
	if w.window == 0 {
		return
	}
	autorelease(func() {
		w.Show() // Show already un-minimizes (frameless + framed) and makes key
	})
}

// Unminimize restores a minimized window. Frame windows deminiaturize; a
// frameless window that Minimize hid is made visible and key again.
func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.frameless {
				w.doUnminimizeFrameless()
				return
			}
			w.window.Send(sel("deminiaturize:"), objc.ID(0))
		})
	})
}

// doUnminimizeFrameless shows a borderless window that Minimize hid (orderOut)
// and brings it to the front. No-op when the window is not in that state.
func (w *webview) doUnminimizeFrameless() {
	if w.window == 0 {
		return
	}
	if !w.minimized {
		return
	}
	w.window.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	w.minimized = false
}

// applyGeometry applies the creation-time geometry from the View to an owned
// window: Width/Height (backend default when zero) with State, and, when
// given - Left/Top before sizing. AppKit coordinates
// measure y from the screen's bottom edge; see the View's geometry fields.
func (w *webview) applyGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	positioned := v.Left != 0 || v.Top != 0
	performOnMain(func() {
		autorelease(func() {
			if positioned {
				w.window.Send(sel("setFrameOrigin:"), cgPoint{float64(v.Left), float64(v.Top)})
			}
			w.applySize(width, height, v.State)
			if !positioned {
				w.window.Send(sel("center"))
			}
		})
	})
	w.isSizeSet = true
}

// applySize applies the creation-time size and State to an owned,
// already-created window, mirroring the Linux applySize. On macOS the window
// was created borderless/framed (windowInitProceed); applySize is responsible
// for the pieces that depend on the final View fields: the style mask
// (frameless keeps no title bar, and StateFixed removes the resizable bit that
// would otherwise expose AppKit's native edge/corner resize), and the content
// size or the matching min/max constraint for the state. It runs on the UI
// thread.
func (w *webview) applySize(width, height int, state State) {
	// Frameless (borderless) windows draw no title bar or OS buttons, but must
	// stay natively resizable at their edge (the page chrome reports edges and
	// corners). Maximize/minimize are handled MANUALLY by the engine (see
	// Maximize/Minimize) because AppKit's performZoom:/miniaturize: are wired
	// only for Titled windows. Frame windows get the ordinary decorated set,
	// whose Miniaturizable bit is what lets a framed window Dock-miniaturize.
	style := uint(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable)
	if state != StateFixed {
		style |= nsWindowStyleMaskResizable
	}
	if w.frameless {
		// Borderless: no title bar or traffic-light buttons. Only Resizable
		// (when not StateFixed) is wanted so native edge resize works.
		// Miniaturizable/Closable are not needed because minimize and zoom are
		// done in Go.
		style &^= nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable
		if state == StateFixed {
			style &^= nsWindowStyleMaskResizable
		}
	}
	w.window.Send(sel("setStyleMask:"), style)
	size := cgSize{float64(width), float64(height)}
	switch state {
	case StateMin:
		w.window.Send(sel("setContentMinSize:"), size)
	case StateMax:
		w.window.Send(sel("setContentMaxSize:"), size)
	default:
		// setContentSize keeps the top-left corner fixed, avoiding a
		// struct-return read of the current frame.
		w.window.Send(sel("setContentSize:"), size)
		w.lastWidth, w.lastHeight = width, height
	}
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	// The uniform content origin is "app://" (see App.FS). While this window
	// is served over its temporary loopback server (darwin app content always
	// is - WKWebView cannot make a custom scheme a secure context), an app://
	// URL is rewritten to that server's http://localhost base: same path,
	// query and fragment, served from the app's filesystem. Once the server's
	// idle timeout has closed it, the dead base is dropped here and later
	// app:// navigations go to the web view's custom scheme handler
	// unchanged.
	if w.contentBase != "" {
		if w.transient != nil && w.transient.isClosed() {
			w.transient = nil
			w.contentBase = ""
		} else {
			url = rewriteAppURL(w.contentBase, url)
		}
	}
	url = canonicalNavigateURL(url)
	w.trust(url)
	performOnMain(func() {
		autorelease(func() {
			nsurl := class("NSURL").Send(sel("URLWithString:"), nsstr(url))
			if nsurl == 0 {
				// NSURL refuses a string it cannot parse, and the view would
				// silently load nothing.
				log.Printf("tuohi: navigate: NSURL refused %q", url)
				return
			}
			req := class("NSURLRequest").Send(sel("requestWithURL:"), nsurl)
			w.webView.Send(sel("loadRequest:"), req)
		})
	})
}

// trust rebuilds the scripts on the main thread, where WKUserContentController
// must be used, and returns once they are in place.
func (w *webview) trust(urls ...string) {
	if !w.trustURLs(urls) {
		return
	}
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.rebuildScriptsLocked()
	})
}

func (w *webview) loadHTML(html string) {
	w.trust(loadHTMLBase)
	performOnMain(func() {
		autorelease(func() {
			base := class("NSURL").Send(sel("URLWithString:"), nsstr(loadHTMLBase))
			w.webView.Send(sel("loadHTMLString:baseURL:"), nsstr(html), base)
		})
	})
}

func (w *webview) Init(js string) {
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.pushUserScript(js)
	})
}

func (w *webview) Eval(js string) {
	if w.webView == 0 {
		return // web view destroyed (e.g. a late reply dispatched after Destroy).
	}
	// Unlike the Linux backend, there is no "URL is nil" guard here: SetHtml uses
	// loadHTMLString with a nil baseURL, which leaves WKWebView.URL nil, so such a
	// guard would block every Eval on SetHtml pages. Evaluating before load is
	// harmless on WKWebView (the completion handler, which we ignore, just errors).
	performOnMain(func() {
		autorelease(func() {
			w.webView.Send(sel("evaluateJavaScript:completionHandler:"), nsstr(js), objc.ID(0))
		})
	})
}

// updateBindings changes the binding table and rebuilds the user scripts on
// the UI thread. The rebuild touches the WKUserContentController, and taking
// mu inside the marshalled closure keeps every mu and AppKit section on one
// thread, with no lock held across a thread hop (see engine.updateBindings).
func (w *webview) updateBindings(mutate func(bindings map[string]binding) error) error {
	var err error
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if err = mutate(w.bindings); err != nil {
			return
		}
		w.rebuildScriptsLocked()
	})
	return err
}

// Destroy releases the web view and closes the native window. The AppKit
// objects must be released in dependency order (see destroyOnUI).
func (w *webview) Destroy() {
	// A window closed before its first load finished (blank window, early
	// close) still owns a temporary loopback server: stop it here - the
	// load-finished path (fireReady) never ran.
	w.releaseLoopback()
	performOnMain(func() { w.destroyOnUI() })
}

func (w *webview) destroyOnUI() {
	autorelease(func() {
		if w.window != 0 {
			if w.webView != 0 {
				if w.uiDelegate != 0 {
					w.webView.Send(sel("setUIDelegate:"), objc.ID(0))
					w.webView.Send(sel("setNavigationDelegate:"), objc.ID(0))
					unregisterEngine(w.uiDelegate)
					w.uiDelegate.Send(sel("release"))
					w.uiDelegate = 0
				}
				w.webView.Send(sel("release"))
				w.webView = 0
			}
			if w.widget != 0 {
				if w.widget == w.window.Send(sel("contentView")) {
					w.window.Send(sel("setContentView:"), objc.ID(0))
				}
				w.widget.Send(sel("release"))
				w.widget = 0
			}
			if w.ownsWindow {
				w.window.Send(sel("setDelegate:"), objc.ID(0))
				w.window.Send(sel("close"))
				w.onWindowDestroyed(true)
			}
			w.window = 0
		}
		if w.windowDelegate != 0 {
			unregisterEngine(w.windowDelegate)
			w.windowDelegate.Send(sel("release"))
			w.windowDelegate = 0
		}
		if w.appDelegate != 0 {
			w.app.Send(sel("setDelegate:"), objc.ID(0))
			unregisterEngine(w.appDelegate)
			w.appDelegate.Send(sel("release"))
			w.appDelegate = 0
		}
		if w.scriptHandler != 0 {
			// The handler object is owned by the (now-released) content manager;
			// only its registry entry needs reclaiming (a map delete).
			unregisterEngine(w.scriptHandler)
			w.scriptHandler = 0
		}
		// Scheme-handler delegates are owned by the (now-released) configuration;
		// like scriptHandler, only their registry entries need reclaiming.
		for _, sh := range w.schemeHandlerObjs {
			unregisterEngine(sh)
		}
		w.schemeHandlerObjs = nil
	})
	// Unblock a Run() waiting on this window (the pump polls the channel).
	w.closeOnce.Do(func() { close(w.closed) })
	if w.ownsWindow && !appkitRunsLoop.Load() && w.app.Send(sel("isRunning")) == 0 {
		// No run loop is active (the normal teardown, after Run returned):
		// flush the events queued during destruction ourselves. When a loop IS
		// running - ours or an external owner's - it drains them, and pumping
		// nested from inside one of its callouts is exactly the kind of
		// re-entrancy to avoid.
		w.depleteRunLoopEventQueue()
	}
}

// runEventLoopWhile pumps queued AppKit events while cond holds, bounded so it
// can never hang even when the application run loop is not active.
func (w *webview) runEventLoopWhile(cond func() bool) {
	for i := 0; i < 10000 && cond(); i++ {
		autorelease(func() {
			ev := w.app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, objc.ID(0), nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

// depleteRunLoopEventQueue runs the event loop until the currently queued
// events have been processed.
func (w *webview) depleteRunLoopEventQueue() {
	var done atomic.Bool
	dispatchMain(func() { done.Store(true) })
	w.runEventLoopWhile(func() bool { return !done.Load() })
}

// --- user scripts + message routing ----------------------------------------

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

// rebuildScriptsLocked re-injects the bridge, Init() scripts and the current
// bind script in order. Assumes w.mu is held (or single-threaded setup).
func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	autorelease(func() {
		w.manager.Send(sel("removeAllUserScripts"))
		addWKUserScript(w.manager, w.bridgeScriptLocked(bridgePostFn))
		for _, src := range w.userScriptSrcs {
			addWKUserScript(w.manager, src)
		}
		addWKUserScript(w.manager, createBindScript(w.bindingEntriesLocked()))
	})
}
func addWKUserScript(manager objc.ID, src string) {
	s := class("WKUserScript").Send(sel("alloc"))
	s = s.Send(sel("initWithSource:injectionTime:forMainFrameOnly:"),
		nsstr(src), wkInjectionTimeAtDocumentStart, true)
	manager.Send(sel("addUserScript:"), s)
	s.Send(sel("release"))
}

// interceptOutsideLinks is false: this engine decides a top-level
// navigation before its request is sent, so the bridge need not (see
// initOutsideLinks).
const interceptOutsideLinks = false

func (w *webview) handleInternal(method string, params json.RawMessage) bool {
	switch method {
	case internalWindowDrag:
		// The app-region tracker saw a mouse-down inside a "drag" box. The
		// mouse-down event is still being processed, so the current event is
		// the one AppKit needs for performWindowDragWithEvent:. If the script
		// message arrives after that dispatch finished, beginMoveDrag
		// synthesizes a left-mouse-down at the reported position instead.
		w.beginMoveDrag(parseDragRequest(params))
	case internalWindowToggleMaximize:
		// The tracker saw a double-click inside a "drag" box. Maximize is
		// already a toggle on macOS: the native performZoom: for framed
		// windows, the saved-frame path for borderless ones. The tracker
		// exists only on frameless windows, so a framed window ignores the
		// message.
		if w.frameless {
			w.Maximize()
		}
	case internalWindowCursor:
		w.setEdgeCursor(parseCursorRequest(params).Edge)
	default:
		return false
	}
	return true
}

// beginMoveDrag runs -[NSWindow performWindowDragWithEvent:]. Prefer the
// event currently being dispatched (the script-message path is synchronous
// with the mouse-down); when it is already gone, synthesize a left-mouse-down
// event at the reported point so the drag still anchors correctly.
func (w *webview) beginMoveDrag(p dragRequestParams) {
	if w.window == 0 || !w.frameless {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			ev := w.app.Send(sel("currentEvent"))
			if ev == 0 {
				// NSEventTypeLeftMouseDown = 1. The web view fills the content
				// view, so the client point maps to window coordinates by
				// flipping Y against the remembered content height.
				winNum := w.window.Send(sel("windowNumber"))
				loc := cgPoint{X: p.ClientX, Y: float64(w.lastHeight) - p.ClientY}
				ev = class("NSEvent").Send(
					sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
					1, loc, uint(0), float64(0), winNum, objc.ID(0), 0, 1, float32(1))
			}
			if ev != 0 {
				w.window.Send(sel("performWindowDragWithEvent:"), ev)
			}
		})
	})
}

// setEdgeCursor pushes AppKit's resize cursor when the pointer hovers a
// frameless window's edge. WKWebView runs in its own process and can revert a
// page-wide CSS cursor to the arrow over live content, so the engine forces
// the native NSCursor for the straight edges (which NSCursor exposes directly)
// as an override on the UI thread. Corner/diagonal edges have no single
// public NSCursor, so they keep the tracker's CSS cursor (see setCursor in
// view.go); leaving an edge band ("") is left to the DOM again so normal
// content cursors (I-beam, drag hand, …) still apply. Inert when the call
// arrives after the window went away.
func (w *webview) setEdgeCursor(edge string) {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			var cursorClassSelector string
			switch edge {
			case "n", "s":
				cursorClassSelector = "resizeUpDownCursor"
			case "e", "w":
				cursorClassSelector = "resizeLeftRightCursor"
			default:
				return // corners + leaving the band stay with the page's CSS cursor
			}
			cursor := class("NSCursor").Send(sel(cursorClassSelector))
			if cursor != 0 {
				cursor.Send(sel("set"))
			}
		})
	})
}

// bridgePostFn for the WebKit backends (macOS WKWebView, Linux WebKitGTK): the
// script message handler registered under the name "__webview__".
const bridgePostFn = `function(message) {
  return window.webkit.messageHandlers.__webview__.postMessage(message);
}`

// newView creates a window and its web view on macOS. The App.Show method
// opens the app scope first and then calls this constructor with the
// committed App.FS; the meaning of opts (Debug, Window, ...) is
// documented there. Like the Windows and Linux backends, the darwin engine
// registers the "app" scheme (serving App.FS) on the web view's
// configuration and applies the window settings at creation (see newWebView).
// The first successful call pins the
// calling goroutine to its OS thread; keep all direct UI calls on that
// goroutine and re-enter through Dispatch from background goroutines.
// Exception: when the application run loop is already running (started by a
// tray loop or another owner), newView may be called from any goroutine -
// creation and the UI-touching methods marshal themselves to the main thread.
func newView(v *View, serve serveFunc) (*webview, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}

	app := class("NSApplication").Send(sel("sharedApplication"))
	loopRunning := app.Send(sel("isRunning")) != 0
	uiIsMainOnce.Do(func() { uiIsMain.Store(onMainThread() || loopRunning) })

	if !onMainThread() && loopRunning {
		// Someone else's run loop is draining the main queue: build the whole
		// webview over there. Doing it here would run AppKit off the main
		// thread, and the old bootstrap path would hang in a second [NSApp
		// run] waiting for an applicationDidFinishLaunching that already fired.
		var w *webview
		performOnMain(func() { w = newWebView(v, serve, app, loopRunning) })
		return w, nil
	}

	uiThreadOnce.Do(runtime.LockOSThread)
	return newWebView(v, serve, app, loopRunning), nil
}

// newWebView builds the webview on the UI thread.

// platformBackend reports the web-engine backend in use. macOS has a single
// built-in backend (WKWebView), so there is nothing to detect or override -
// APPKIT_BACKEND is Linux-only (see lib_unix.go).
func platformBackend() string { return "WKWebView" }

// --- app-level run loop (App.Wait) -----------------------------------------

// appUIWait runs the NSApplication loop - or, when an external owner (such as
// the tray package) already runs it, services the queue - until the app scope
// asks to exit. App.Wait calls it repeatedly.
func appUIWait() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	if app.Send(sel("isRunning")) == 0 {
		appkitRunsLoop.Store(true)
		app.Send(sel("run"))
		appkitRunsLoop.Store(false)
		return
	}
	for !appExitRequested() {
		autorelease(func() {
			deadline := class("NSDate").Send(sel("dateWithTimeIntervalSinceNow:"), 0.05)
			ev := app.Send(sel("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, deadline, nsstr("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				app.Send(sel("sendEvent:"), ev)
			}
		})
	}
}

// appUIWake makes a running NSApplication loop return (App.Exit).
func appUIWake() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	performOnMain(func() {
		app.Send(sel("stop:"), objc.ID(0))
		postWakeEvent(app)
	})
}
