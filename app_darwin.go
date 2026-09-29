// macOS backends for the app-scope services: the application icon (an
// NSImage handed to AppKit, which the Dock draws) and Open/Reveal
// (NSWorkspace) - all via purego's Objective-C runtime (no cgo).
package tuohi

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
func setAppIcon(png []byte, _ string) error {
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
	_ = setAppIcon(lastIcon, "")
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

// --- Autostart -------------------------------------

// The darwin autostart backend. Two mechanisms, chosen by what the running
// binary is:
//
//   - SMAppService (macOS 13+): used when the executable lives inside a .app
//     bundle that carries a bundle identifier. Registration goes through the
//     OS (no artefact file of our own) and works for sandboxed and Mac-App-
//     Store apps without an automation prompt.
//   - LaunchAgent: the fallback for unbundled binaries and older macOS. A
//     plist is written to ~/Library/LaunchAgents/<label>.plist and loaded
//     into the current GUI session, best effort.
//
// A registration points at the running executable and takes effect on the
// next login.

// darwinAutostart implements autostartBackend on macOS.
type darwinAutostart struct{}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_darwin*.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newAutostartBackend returns the darwin autostart backend.
func newAutostartBackend(cfg appConfig) autostartBackend {
	return &darwinAutostart{}
}

// strategy picks SMAppService when running from a bundled .app on macOS 13+,
// otherwise the LaunchAgent path.
func (a *darwinAutostart) strategy() string {
	if !runningFromAppBundle() || bundleID() == "" {
		return autostartBackendLaunchAgent
	}
	major, err := darwinMajorVersion()
	if err != nil || major < 13 {
		return autostartBackendLaunchAgent
	}
	return autostartBackendSMAppService
}

func (a *darwinAutostart) enable(id string, args []string) error {
	switch a.strategy() {
	case autostartBackendSMAppService:
		if err := smAppServiceRegister(); err == nil {
			return nil
		} else if !errors.Is(err, errSMAppServiceUnavailable) {
			return fmt.Errorf("appkit: autostart: SMAppService register: %w", err)
		}
		// Unavailable (or the class missing): fall through to the plist.
		fallthrough
	default:
		return a.enableLaunchAgent(id, args)
	}
}

func (a *darwinAutostart) disable() error {
	// Try both paths and merge errors: an earlier version of the app may
	// have used the other mechanism.
	var errs []error
	if a.strategy() == autostartBackendSMAppService {
		if err := smAppServiceUnregister(); err != nil &&
			!errors.Is(err, errSMAppServiceUnavailable) &&
			!errors.Is(err, errSMAppServiceNotRegistered) {
			errs = append(errs, fmt.Errorf("appkit: autostart: SMAppService unregister: %w", err))
		}
	}
	if err := a.disableLaunchAgent(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *darwinAutostart) status() (bool, string, string) {
	if a.strategy() == autostartBackendSMAppService {
		enabled, err := smAppServiceIsEnabled()
		// RequiresApproval means the user switched the login item off in
		// System Settings: semantically "not enabled". Like Unavailable, it
		// lets the LaunchAgent fallback look for a legacy plist.
		if err == nil && enabled {
			return true, bundleID(), autostartBackendSMAppService
		}
	}
	// Also checked when SMAppService answered "no", so a previously written
	// LaunchAgent does not disappear after the app was bundled.
	if path, ok := a.findLaunchAgent(); ok {
		return true, path, autostartBackendLaunchAgent
	}
	return false, "", ""
}

func (a *darwinAutostart) launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("appkit: autostart: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func (a *darwinAutostart) enableLaunchAgent(id string, args []string) error {
	exe, err := resolvedExecutable()
	if err != nil {
		return err
	}
	dir, err := a.launchAgentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("appkit: autostart: create LaunchAgents dir: %w", err)
	}
	path := filepath.Join(dir, id+".plist")
	body, err := launchAgentPlist(id, exe, args)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, body, 0o644); err != nil {
		return fmt.Errorf("appkit: autostart: write plist: %w", err)
	}
	// Best effort: activate immediately for the current GUI session. The
	// plist is picked up at next login regardless.
	_ = launchctlBootstrap(path)
	return nil
}

func (a *darwinAutostart) disableLaunchAgent() error {
	path, ok := a.findLaunchAgent()
	if !ok {
		return nil
	}
	_ = launchctlBootout(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("appkit: autostart: remove plist: %w", err)
	}
	return nil
}

// findLaunchAgent looks for a plist in ~/Library/LaunchAgents whose
// ProgramArguments' first element equals the current executable.
func (a *darwinAutostart) findLaunchAgent() (string, bool) {
	dir, err := a.launchAgentsDir()
	if err != nil {
		return "", false
	}
	exe, err := resolvedExecutable()
	if err != nil {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if plistFirstProgramArg(data) == exe {
			return full, true
		}
	}
	return "", false
}

// runningFromAppBundle reports whether the executable lives inside a .app
// bundle (its path ends with .app/Contents/MacOS/<name>).
func runningFromAppBundle() bool {
	exe, err := resolvedExecutable()
	if err != nil {
		return false
	}
	macOSDir := filepath.Dir(exe)
	contentsDir := filepath.Dir(macOSDir)
	appDir := filepath.Dir(contentsDir)
	return filepath.Base(macOSDir) == "MacOS" &&
		filepath.Base(contentsDir) == "Contents" &&
		strings.HasSuffix(appDir, ".app")
}

// bundleID reads the CFBundleIdentifier of the .app bundle containing the
// running executable ("" when there is none or it cannot be read).
func bundleID() string {
	exe, err := resolvedExecutable()
	if err != nil {
		return ""
	}
	macOSDir := filepath.Dir(exe)
	appDir := filepath.Dir(filepath.Dir(macOSDir))
	if filepath.Base(macOSDir) != "MacOS" || !strings.HasSuffix(appDir, ".app") {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	return plistStringForKey(data, "CFBundleIdentifier")
}

// plistStringForKey returns the <string> value of the given key in an XML
// plist ("" when the key is absent or the file is not parseable).
func plistStringForKey(data []byte, want string) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	var inDict, captureKey bool
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					captureKey = true
				}
			case "string":
				if lastKey == want {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						return s
					}
					return ""
				}
			}
		case xml.CharData:
			if captureKey {
				lastKey = string(t)
				captureKey = false
			}
		}
	}
}

// darwinMajorVersion returns the running macOS major version (e.g. 14 for
// 14.5), via sw_vers.
func darwinMajorVersion() (int, error) {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return 0, err
	}
	ver := strings.TrimSpace(string(out))
	if i := strings.IndexByte(ver, '.'); i > 0 {
		ver = ver[:i]
	}
	return strconv.Atoi(ver)
}

// launchctlBootstrap loads a plist into the current GUI session. Best effort;
// errors are ignored (the plist is still picked up at next login).
//
// Indirected through a package-level variable so tests can replace it with a
// no-op: a test plist with RunAtLoad=true that bootstraps successfully would
// respawn the test binary recursively.
var launchctlBootstrap = func(plistPath string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	return exec.Command("launchctl", "bootstrap", target, plistPath).Run()
}

// launchctlBootout unloads a plist from the current GUI session; see
// launchctlBootstrap.
var launchctlBootout = func(plistPath string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	return exec.Command("launchctl", "bootout", target, plistPath).Run()
}

// --- plist marshalling ------------------------------------------------------

// launchAgentPlist renders a LaunchAgent plist that runs exe with args at
// login (RunAtLoad, no KeepAlive).
func launchAgentPlist(label, exe string, args []string) ([]byte, error) {
	progArgs := append([]string{exe}, args...)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	sb.WriteString(`<plist version="1.0">` + "\n")
	sb.WriteString("  <dict>\n")
	sb.WriteString("    <key>Label</key>\n")
	fmt.Fprintf(&sb, "    <string>%s</string>\n", xmlEscape(label))
	sb.WriteString("    <key>ProgramArguments</key>\n")
	sb.WriteString("    <array>\n")
	for _, a := range progArgs {
		fmt.Fprintf(&sb, "      <string>%s</string>\n", xmlEscape(a))
	}
	sb.WriteString("    </array>\n")
	sb.WriteString("    <key>RunAtLoad</key>\n")
	sb.WriteString("    <true/>\n")
	sb.WriteString("    <key>KeepAlive</key>\n")
	sb.WriteString("    <false/>\n")
	sb.WriteString("  </dict>\n")
	sb.WriteString("</plist>\n")
	return []byte(sb.String()), nil
}

// xmlEscape escapes s for use inside an XML text element.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plistFirstProgramArg returns the first <string> under the ProgramArguments
// array of a LaunchAgent plist ("" on any parse failure - a malformed file is
// treated as "not ours").
func plistFirstProgramArg(data []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	var inDict, inArray, captureKey bool
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					captureKey = true
				}
			case "array":
				if lastKey == "ProgramArguments" {
					inArray = true
				}
			case "string":
				if inArray {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						return s
					}
					return ""
				}
			}
		case xml.CharData:
			if captureKey {
				lastKey = string(t)
				captureKey = false
			}
		case xml.EndElement:
			if t.Name.Local == "array" && inArray {
				return ""
			}
		}
	}
}

// SMAppService (macOS 13+) access without CGO: the class is driven through
// the Objective-C runtime with purego, exactly like the rest of the darwin
// backend (lib_darwin.go). SMAppService.mainAppService registers the app that
// CONTAINS the running binary - i.e. it only makes sense for a bundled .app,
// whose own bundle identifier is the registration key.

// SMAppServiceStatus values (the class's NSInteger property): not registered,
// enabled, requires user approval, not found.
const (
	smAppServiceStatusNotRegistered    = 0
	smAppServiceStatusEnabled          = 1
	smAppServiceStatusRequiresApproval = 2
	smAppServiceStatusNotFound         = 3
)

var (
	errSMAppServiceUnavailable      = errors.New("appkit: SMAppService unavailable on this macOS")
	errSMAppServiceNotRegistered    = errors.New("appkit: SMAppService not registered")
	errSMAppServiceRequiresApproval = errors.New("appkit: SMAppService requires user approval in System Settings")
)

// smAppService returns the shared mainAppService instance, or 0 when the
// class does not exist (macOS < 13).
func smAppService() objc.ID {
	if class("SMAppService") == 0 {
		return 0
	}
	return class("SMAppService").Send(sel("mainAppService"))
}

// smAppServiceRegister registers the containing app as a login item.
func smAppServiceRegister() error {
	svc := smAppService()
	if svc == 0 {
		return errSMAppServiceUnavailable
	}
	var errID objc.ID
	if svc.Send(sel("registerAndReturnError:"), unsafe.Pointer(&errID)) != 0 {
		return nil
	}
	return smAppServiceFailure(errID, "register")
}

// smAppServiceUnregister removes the login item; errSMAppServiceNotRegistered
// when nothing was registered.
func smAppServiceUnregister() error {
	svc := smAppService()
	if svc == 0 {
		return errSMAppServiceUnavailable
	}
	switch int(svc.Send(sel("status"))) {
	case smAppServiceStatusNotRegistered, smAppServiceStatusNotFound:
		return errSMAppServiceNotRegistered
	}
	var errID objc.ID
	if svc.Send(sel("unregisterAndReturnError:"), unsafe.Pointer(&errID)) != 0 {
		return nil
	}
	return smAppServiceFailure(errID, "unregister")
}

// smAppServiceIsEnabled reports whether the containing app is currently a
// registered login item. errSMAppServiceRequiresApproval means the user
// disabled it in System Settings - semantically "not enabled", signalled
// separately so the caller can fall back to looking for a LaunchAgent.
func smAppServiceIsEnabled() (bool, error) {
	svc := smAppService()
	if svc == 0 {
		return false, errSMAppServiceUnavailable
	}
	switch int(svc.Send(sel("status"))) {
	case smAppServiceStatusEnabled:
		return true, nil
	case smAppServiceStatusRequiresApproval:
		return false, errSMAppServiceRequiresApproval
	default:
		return false, nil
	}
}

// smAppServiceFailure turns the NSError** a failed call filled in into a Go
// error (a generic message when the call left it nil).
func smAppServiceFailure(errID objc.ID, what string) error {
	if errID != 0 {
		if desc := errID.Send(sel("localizedDescription")); desc != 0 {
			if s := cstr(desc.Send(sel("UTF8String"))); s != "" {
				return errors.New("appkit: SMAppService " + what + ": " + s)
			}
		}
	}
	return errors.New("appkit: SMAppService " + what + " failed")
}
