package autostart

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
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

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

// darwinAutostart implements backend on macOS.
type darwinAutostart struct{}

// The autostart support below is derived from Wails v3
// pkg/application/autostart_darwin*.go (MIT, Copyright (c) 2018-Present Lea Anthony).
// See NOTICE.

// newBackend returns the darwin autostart backend.
func newBackend() backend {
	return &darwinAutostart{}
}

// strategy picks SMAppService when running from a bundled .app on macOS 13+,
// otherwise the LaunchAgent path.
func (a *darwinAutostart) strategy() string {
	if !runningFromAppBundle() || bundleID() == "" {
		return backendLaunchAgent
	}
	major, err := darwinMajorVersion()
	if err != nil || major < 13 {
		return backendLaunchAgent
	}
	return backendSMAppService
}

func (a *darwinAutostart) enable(id string, args []string) error {
	switch a.strategy() {
	case backendSMAppService:
		if err := smAppServiceRegister(); err == nil {
			return nil
		} else if !errors.Is(err, errSMAppServiceUnavailable) {
			return fmt.Errorf("autostart: SMAppService register: %w", err)
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
	if a.strategy() == backendSMAppService {
		if err := smAppServiceUnregister(); err != nil &&
			!errors.Is(err, errSMAppServiceUnavailable) &&
			!errors.Is(err, errSMAppServiceNotRegistered) {
			errs = append(errs, fmt.Errorf("autostart: SMAppService unregister: %w", err))
		}
	}
	if err := a.disableLaunchAgent(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *darwinAutostart) status() (bool, string, string) {
	if a.strategy() == backendSMAppService {
		enabled, err := smAppServiceIsEnabled()
		// RequiresApproval means the user switched the login item off in
		// System Settings: semantically "not enabled". Like Unavailable, it
		// lets the LaunchAgent fallback look for a legacy plist.
		if err == nil && enabled {
			return true, bundleID(), backendSMAppService
		}
	}
	// Also checked when SMAppService answered "no", so a previously written
	// LaunchAgent does not disappear after the app was bundled.
	if path, ok := a.findLaunchAgent(); ok {
		return true, path, backendLaunchAgent
	}
	return false, "", ""
}

func (a *darwinAutostart) launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("autostart: %w", err)
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
		return fmt.Errorf("autostart: create LaunchAgents dir: %w", err)
	}
	path := filepath.Join(dir, id+".plist")
	body, err := launchAgentPlist(id, exe, args)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, body, 0o644); err != nil {
		return fmt.Errorf("autostart: write plist: %w", err)
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
		return fmt.Errorf("autostart: remove plist: %w", err)
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
// the Objective-C runtime with purego, exactly like tuohi's darwin engine
// (lib_darwin.go). SMAppService.mainAppService registers the app that
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
	errSMAppServiceUnavailable      = errors.New("autostart: SMAppService unavailable on this macOS")
	errSMAppServiceNotRegistered    = errors.New("autostart: SMAppService not registered")
	errSMAppServiceRequiresApproval = errors.New("autostart: SMAppService requires user approval in System Settings")
)

// serviceManagementPath is the framework that vends SMAppService. Nothing else
// in the process may have loaded it, so smAppService loads it itself.
const serviceManagementPath = "/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement"

var smLoadOnce sync.Once

// smAppService returns the shared mainAppService instance, or 0 when the
// class does not exist (macOS < 13, or the framework could not be loaded).
func smAppService() objc.ID {
	smLoadOnce.Do(func() {
		// A failed load leaves the class missing, which reads as unavailable
		// and sends Enable to the LaunchAgent fallback.
		_, _ = purego.Dlopen(serviceManagementPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	})
	cls := objc.ID(objc.GetClass("SMAppService"))
	if cls == 0 {
		return 0
	}
	return cls.Send(sel("mainAppService"))
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
				return errors.New("autostart: SMAppService " + what + ": " + s)
			}
		}
	}
	return errors.New("autostart: SMAppService " + what + " failed")
}

// sel returns the Objective-C selector with the given name.
func sel(name string) objc.SEL { return objc.RegisterName(name) }

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
