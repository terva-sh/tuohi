package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const managedDelegateClass = "TUOHIManagedNotificationDelegate"

type macBackend struct {
	center   objc.ID
	delegate objc.ID
	event    func(managedEvent)
}

var (
	managedMacMu         sync.Mutex
	managedMacClients    = make(map[objc.ID]*macBackend)
	managedMacOnce       sync.Once
	managedMacErr        error
	managedDefaultAction objc.ID
)

func notificationPool(fn func()) {
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	fn()
}

func notificationString(value objc.ID) string {
	if value == 0 {
		return ""
	}
	n := objc.Send[uint64](value, sel("lengthOfBytesUsingEncoding:"), uint64(4)) // NSUTF8StringEncoding
	p := value.Send(sel("UTF8String"))
	if p == 0 || n == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	return string(unsafe.Slice((*byte)(ptr), int(n)))
}

// Do not call currentNotificationCenter until the main bundle is a real app.
// UNUserNotificationCenter raises an Objective-C exception for bare executables;
// exceptions cannot be recovered by Go's panic/recover.
func notificationBundleAvailable() bool {
	bundle := class("NSBundle").Send(sel("mainBundle"))
	if bundle == 0 {
		return false
	}
	root := notificationString(bundle.Send(sel("bundlePath")))
	identifier := notificationString(bundle.Send(sel("bundleIdentifier")))
	kind := notificationString(bundle.Send(sel("objectForInfoDictionaryKey:"), nsstr("CFBundlePackageType")))
	executable := notificationString(bundle.Send(sel("executablePath")))
	actual, err := os.Executable()
	if err != nil || !validNotificationBundle(root, identifier, kind, executable) {
		return false
	}
	actual, err = filepath.EvalSymlinks(actual)
	if err != nil {
		return false
	}
	executable, err = filepath.EvalSymlinks(executable)
	return err == nil && actual == executable
}

func validNotificationBundle(root, identifier, kind, executable string) bool {
	if !filepath.IsAbs(root) || filepath.Ext(root) != ".app" || identifier == "" || kind != "APPL" {
		return false
	}
	rel, err := filepath.Rel(filepath.Join(root, "Contents", "MacOS"), executable)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func initManagedMac() error {
	managedMacOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			managedMacErr = fmt.Errorf("%w: UserNotifications framework unavailable", ErrUnavailable)
			return
		}
		if objc.GetClass("UNUserNotificationCenter") == 0 {
			managedMacErr = ErrUnsupported
			return
		}
		address, err := purego.Dlsym(lib, "UNNotificationDefaultActionIdentifier")
		if err != nil {
			managedMacErr = ErrUnsupported
			return
		}
		pointer := *(*unsafe.Pointer)(unsafe.Pointer(&address))
		managedDefaultAction = *(*objc.ID)(pointer)
		protocol := objc.GetProtocol("UNUserNotificationCenterDelegate")
		if protocol == nil {
			managedMacErr = ErrUnsupported
			return
		}
		_, err = objc.RegisterClass(managedDelegateClass, objc.GetClass("NSObject"), []*objc.Protocol{protocol}, nil, []objc.MethodDef{
			{Cmd: sel("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, response, completion objc.ID) {
				notificationPool(func() {
					defer invokeNotificationCompletion(completion, nil)
					managedMacMu.Lock()
					b := managedMacClients[self]
					managedMacMu.Unlock()
					if b == nil {
						return
					}
					request := response.Send(sel("notification")).Send(sel("request"))
					id := notificationString(request.Send(sel("identifier")))
					action := response.Send(sel("actionIdentifier"))
					if objc.Send[bool](action, sel("isEqualToString:"), managedDefaultAction) {
						// Never wait for the client's operation lock on a native
						// callback queue; a posting completion may need that queue.
						go b.event(managedEvent{native: id, activated: true, closed: true})
					}
				})
			}},
			{Cmd: sel("userNotificationCenter:willPresentNotification:withCompletionHandler:"), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, completion objc.ID) {
				notificationPool(func() {
					options := uint64(1<<2 | 1<<1) // UNNotificationPresentationOptionAlert | Sound
					invokeNotificationCompletion(completion, &options)
				})
			}},
		})
		if err != nil {
			managedMacErr = errors.New("notify: cannot register notification delegate")
		}
	})
	return managedMacErr
}

// Native completion blocks are not Go-created objc.Block values. Drive them
// using their NSInvocation signature, including the hidden block argument.
func invokeNotificationCompletion(block objc.ID, options *uint64) {
	if block == 0 {
		return
	}
	types := "v@?"
	if options != nil {
		types = "v@?Q"
	}
	signature := class("NSMethodSignature").Send(sel("signatureWithObjCTypes:"), types)
	invocation := class("NSInvocation").Send(sel("invocationWithMethodSignature:"), signature)
	invocation.Send(sel("setTarget:"), block)
	if options != nil {
		invocation.Send(sel("setArgument:atIndex:"), unsafe.Pointer(options), 1)
	}
	invocation.Send(sel("invoke"))
}

func openManagedBackend(ctx context.Context, _ Config, event func(managedEvent)) (managedBackend, error) {
	if err := ensureInit(); err != nil {
		return nil, err
	}
	var b *macBackend
	var err error
	notificationPool(func() {
		if !notificationBundleAvailable() {
			err = fmt.Errorf("%w: managed notifications require a bundled APPL .app with a bundle identifier", ErrUnavailable)
			return
		}
		if err = initManagedMac(); err != nil {
			return
		}
		if err = ctx.Err(); err != nil {
			return
		}
		managedMacMu.Lock()
		defer managedMacMu.Unlock()
		center := class("UNUserNotificationCenter").Send(sel("currentNotificationCenter"))
		if center == 0 {
			err = ErrUnavailable
			return
		}
		if center.Send(sel("delegate")) != 0 {
			err = fmt.Errorf("%w: another notification center delegate is installed", ErrUnavailable)
			return
		}
		delegate := class(managedDelegateClass).Send(sel("alloc")).Send(sel("init"))
		if delegate == 0 {
			err = ErrUnavailable
			return
		}
		center.Send(sel("retain"))
		b = &macBackend{center: center, delegate: delegate, event: event}
		managedMacClients[delegate] = b
		center.Send(sel("setDelegate:"), delegate) // delegate is weak; b owns the retain
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (b *macBackend) capabilities() Capabilities {
	return Capabilities{Activation: true, Replacement: true, Dismissal: true, Permission: true}
}

func (b *macBackend) permission(ctx context.Context, request bool) (PermissionStatus, error) {
	if err := ctx.Err(); err != nil {
		return PermissionUnknown, err
	}
	if request {
		type result struct {
			granted bool
			failed  bool
		}
		results := make(chan result, 1)
		notificationPool(func() {
			block := objc.NewBlock(func(_ objc.Block, granted bool, nativeErr objc.ID) { results <- result{granted, nativeErr != 0} })
			defer block.Release()
			b.center.Send(sel("requestAuthorizationWithOptions:completionHandler:"), uint64(1<<2|1<<1), block) // Alert | Sound
		})
		select {
		case <-ctx.Done():
			return PermissionUnknown, ctx.Err()
		case r := <-results:
			if r.failed {
				return PermissionUnknown, ErrUnavailable
			}
			if !r.granted {
				return PermissionDenied, nil
			}
		}
	}
	result := make(chan PermissionStatus, 1)
	notificationPool(func() {
		block := objc.NewBlock(func(_ objc.Block, settings objc.ID) {
			status := PermissionUnknown
			switch objc.Send[int64](settings, sel("authorizationStatus")) {
			case 0:
				status = PermissionNotDetermined
			case 1:
				status = PermissionDenied
			case 2:
				status = PermissionAuthorized
			case 3:
				status = PermissionProvisional
			}
			result <- status
		})
		defer block.Release()
		b.center.Send(sel("getNotificationSettingsWithCompletionHandler:"), block)
	})
	select {
	case <-ctx.Done():
		return PermissionUnknown, ctx.Err()
	case status := <-result:
		return status, nil
	}
}

func notificationIDs(id string) objc.ID {
	return class("NSArray").Send(sel("arrayWithObject:"), nsstr(id))
}

func removeManagedMacNotification(center objc.ID, id string) {
	ids := notificationIDs(id)
	center.Send(sel("removePendingNotificationRequestsWithIdentifiers:"), ids)
	center.Send(sel("removeDeliveredNotificationsWithIdentifiers:"), ids)
}

func (b *macBackend) post(ctx context.Context, id, _ string, n Notification) (string, error) {
	if n.Options.Icon != "" || n.Options.IconData != nil {
		return "", fmt.Errorf("%w: managed macOS icon attachments are not supported", ErrUnsupported)
	}
	status, err := b.permission(ctx, false)
	if err != nil {
		return "", err
	}
	if status != PermissionAuthorized && status != PermissionProvisional {
		return "", ErrPermissionDenied
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	result := make(chan bool, 1)
	notificationPool(func() {
		content := class("UNMutableNotificationContent").Send(sel("alloc")).Send(sel("init"))
		defer content.Send(sel("release"))
		content.Send(sel("setTitle:"), nsstr(n.Title))
		content.Send(sel("setBody:"), nsstr(n.Message))
		if n.Options.Urgency.level() == 2 {
			content.Send(sel("setSound:"), class("UNNotificationSound").Send(sel("defaultSound")))
		}
		request := class("UNNotificationRequest").Send(sel("requestWithIdentifier:content:trigger:"), nsstr(id), content, objc.ID(0))
		// The completion may outlive a cancelled Post and Client.Close.
		// Keep the center alive for late best-effort cancellation cleanup.
		center := b.center.Send(sel("retain"))
		block := objc.NewBlock(func(_ objc.Block, nativeErr objc.ID) {
			notificationPool(func() {
				defer center.Send(sel("release"))
				if ctx.Err() != nil {
					removeManagedMacNotification(center, id)
				}
				result <- nativeErr == 0
			})
		})
		defer block.Release()
		b.center.Send(sel("addNotificationRequest:withCompletionHandler:"), request, block)
	})
	select {
	case <-ctx.Done():
		notificationPool(func() { removeManagedMacNotification(b.center, id) })
		return "", ctx.Err()
	case ok := <-result:
		if !ok {
			return "", fmt.Errorf("%w: notification center rejected the request", ErrUnavailable)
		}
		return id, nil
	}
}

func (b *macBackend) dismiss(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	notificationPool(func() { removeManagedMacNotification(b.center, id) })
	return nil // native removal APIs acknowledge submission, not actual visibility
}

func (b *macBackend) close() error {
	notificationPool(func() {
		managedMacMu.Lock()
		delete(managedMacClients, b.delegate)
		if b.center.Send(sel("delegate")) == b.delegate {
			b.center.Send(sel("setDelegate:"), objc.ID(0))
		}
		managedMacMu.Unlock()
		b.delegate.Send(sel("release"))
		b.center.Send(sel("release"))
	})
	return nil
}
