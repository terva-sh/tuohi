// Package notify displays OS-level desktop notifications without cgo or
// bundled libraries. It is fully standalone: no tray icon and no window are
// required, only a running desktop session.
//
//	err := notify.Show("backup", "done", "Snapshot finished at 09:41")
//
// Each platform uses the notification service it already ships:
// NSUserNotificationCenter on macOS, a Shell_NotifyIconW balloon on Windows,
// and org.freedesktop.Notifications on Linux.
//
// macOS caveat: the deprecated NSUserNotificationCenter only hands a usable
// center to a process with an app identity - a bundled .app the user granted
// Notification permission in System Settings. An unbundled binary (e.g. a
// `go run ./demo` not inside a .app) gets no center; Show then returns
// ErrUnavailable (check with errors.Is) rather than crashing, because the
// modern UNUserNotificationCenter aborts without an app bundle.
//
// The name argument identifies the source application shown by the desktop
// (the app_name of org.freedesktop.Notifications on Linux; macOS and Windows
// derive the identity from the OS and ignore it). Title and message are plain
// UTF-8 text.
//
// Show sends a plain notification. ShowOpts additionally attaches an icon -
// Options.Icon (an image file path, or a themed icon name on Linux) or
// Options.IconData (raw PNG bytes; set at most one) - and an Urgency level.
// Alert posts the notification at UrgencyCritical and follows up with the
// platform's attention sound; Beep sounds a tone directly. Icon support
// differs per platform: Linux takes an image path or themed icon name and can
// embed raw PNG bytes in the D-Bus message; macOS loads most image formats
// from a file path and PNG bytes; Windows loads .ico/.bmp files or the stock
// names "info", "information", "warning" and "error" and does not decode PNG
// bytes (ErrUnsupported). See the per-OS backend comments and notify/README.md.
//
// Threading: every function is safe to call from any goroutine. macOS
// delivers through the shared NSUserNotificationCenter and Windows through a
// lazily created hidden notification icon, so there is nothing to set up
// first.
//
// On platforms without a notification backend, Show, ShowOpts and Alert
// return ErrUnsupported.
package notify

import "errors"

// ErrUnsupported is returned by Show when the platform has no notification
// backend, and by the icon/beep features when the platform cannot honor them.
var ErrUnsupported = errors.New("notify: not supported on this platform")

// ErrUnavailable is returned by Show when the platform nominally has a
// notification backend but it cannot be used for this process/environment. On
// macOS this is the case for anything but a bundled .app the user granted
// Notification permission: the legacy NSUserNotificationCenter returns nil for
// an unbundled process, and the modern UNUserNotificationCenter crashes
// without an app bundle. On Linux it is the case when no notification service
// is reachable at all (no session bus, notify-send or kdialog). Consumers
// that want to degrade gracefully can test errors.Is(err, ErrUnavailable).
var ErrUnavailable = errors.New("notify: notifications unavailable in this process/environment (macOS: needs a bundled .app the user granted Notification permission; Linux: no session bus or notify-send/kdialog)")

// DefaultFreq is the tone frequency in hertz that Beep and Alert use when
// called with a zero frequency: 440 Hz, the A above middle C.
var DefaultFreq = 440.0

// DefaultDuration is the tone duration in milliseconds that Beep and Alert
// use when called with a zero duration.
var DefaultDuration = 200

// Urgency expresses how strongly a notification should draw the user's
// attention. The zero value of Urgency - and therefore of Options - means
// normal urgency, so a plain Options{} behaves exactly like Show.
type Urgency uint8

const (
	// UrgencyLow marks background information that may be ignored until the
	// user looks at it.
	UrgencyLow Urgency = iota + 1
	// UrgencyNormal is the standard urgency; it is what a zero Options value
	// and Show use.
	UrgencyNormal
	// UrgencyCritical asks the desktop for immediate attention: on macOS it
	// plays the notification sound, on Linux it requests the desktop's alert
	// sound, and on Windows it shows the error-styled balloon.
	UrgencyCritical
)

// level normalizes an Urgency to 0 (low), 1 (normal) or 2 (critical). The
// zero value and any unknown value behave as UrgencyNormal so that an
// Options{} always means a normal notification.
func (u Urgency) level() int {
	switch u {
	case UrgencyLow:
		return 0
	case UrgencyCritical:
		return 2
	default:
		return 1
	}
}

// Options configures a notification beyond the plain Show basics. The zero
// value posts the same normal-urgency, icon-less notification as Show.
type Options struct {
	// Icon is an image shown with the notification.
	//
	// Linux: a path to an image file or a themed icon name resolved by the
	// desktop (e.g. "dialog-warning").
	// macOS: a path to an image file in any format NSImage reads.
	// Windows: a path to an .ico or .bmp file, or one of the stock names
	// "info", "information", "warning" or "error" (case-insensitive), which
	// select the matching system icon. Windows does not load PNG files.
	Icon string

	// IconData holds the raw bytes of a PNG image shown with the
	// notification. It is an alternative to Icon - set at most one. Linux
	// embeds the decoded pixels in the D-Bus message; macOS hands the bytes
	// to NSImage; Windows does not decode PNGs and returns ErrUnsupported
	// (use Icon with an .ico or .bmp file there).
	IconData []byte

	// Urgency selects how strongly the desktop should draw attention.
	// UrgencyCritical is what Alert uses and makes the desktop play a sound
	// or present the notification more prominently.
	Urgency Urgency
}

// validate rejects option combinations no platform could honor. It runs
// before the per-OS backends so every GOOS agrees on the rules.
func (o Options) validate() error {
	if o.Icon != "" && len(o.IconData) > 0 {
		return errors.New("notify: set at most one of Options.Icon and Options.IconData")
	}
	if o.IconData != nil && len(o.IconData) == 0 {
		return errors.New("notify: Options.IconData must not be empty")
	}
	return nil
}

// Show posts a desktop notification from the named application, with the
// given title and message. An empty name falls back to the name of the
// running executable. It is equivalent to ShowOpts with a zero Options.
func Show(name, title, message string) error {
	return ShowOpts(name, title, message, Options{})
}

// ShowOpts posts a desktop notification like Show, configured by opts: an
// icon (Options.Icon or Options.IconData, not both) and an Urgency. The name,
// title and message behave exactly as in Show.
func ShowOpts(name, title, message string, opts Options) error {
	if err := opts.validate(); err != nil {
		return err
	}
	return show(name, title, message, opts)
}

// Alert posts a notification that demands attention: it shows the message at
// UrgencyCritical urgency (overriding opts.Urgency), then plays the
// platform's alert sound - the notification sound attached to the critical
// notification on macOS, a MessageBeep on Windows, and a Beep with
// DefaultFreq/DefaultDuration on Linux. Icon options are honored like
// ShowOpts.
func Alert(name, title, message string, opts Options) error {
	if err := opts.validate(); err != nil {
		return err
	}
	opts.Urgency = UrgencyCritical
	if err := show(name, title, message, opts); err != nil {
		return err
	}
	return alertSound()
}

// Beep plays an audible tone of the given frequency (hertz) and duration
// (milliseconds). A zero frequency or duration selects DefaultFreq (440 Hz)
// and DefaultDuration (200 ms).
//
// The mechanism is platform-specific: Linux writes a tone event to the PC
// speaker (/dev/input/by-path/platform-pcspkr-event-spkr) and falls back to
// the terminal bell character when the speaker is unavailable; Windows calls
// the kernel Beep function (clamping the frequency to the 37–32767 Hz range
// the driver accepts); macOS plays the system beep through osascript and
// falls back to the terminal bell - macOS ignores frequency and duration.
// Beep returns ErrUnsupported on platforms without any of these.
func Beep(freq float64, duration int) error {
	return beep(freq, duration)
}
