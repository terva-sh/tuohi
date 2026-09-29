//go:build linux || freebsd || netbsd

// Unix notification backend (Linux, FreeBSD, NetBSD): org.freedesktop.Notifications over the session
// D-Bus via github.com/godbus/dbus/v5 (pure Go, no cgo). The connection is
// opened lazily on the first call and then reused; a failed or closed
// connection is dropped and reopened on the next call. The Show name
// argument becomes the app_name shown by the desktop (falling back to the
// basename of os.Args[0] when empty).
//
// Options are carried on the wire per the notification spec: an icon path or
// themed icon name (Options.Icon) becomes the app_icon argument, PNG bytes
// (Options.IconData) are decoded and sent as the image-data hint with the
// (iiibiiay) signature, the urgency is sent as the urgency hint (byte 0 = low,
// 1 = normal, 2 = critical), and a critical notification also asks for the
// desktop's attention sound via the sound-name hint.
//
// When the session bus is unreachable or the Notify call fails, show falls
// back to the notify-send CLI and then to the kdialog CLI (both optional
// desktop tools, located on PATH). If every path fails the returned error
// wraps ErrUnavailable, so callers can tell "no notification service in this
// environment" apart from other failures with errors.Is.
//
// beep writes a tone start/stop event pair to the PC speaker device
// /dev/input/by-path/platform-pcspkr-event-spkr (the pcspkr module must be
// loaded and the user in the input group); when the device cannot be opened
// it falls back to writing the terminal bell character (0x07) to stdout, so
// Beep degrades instead of failing hard on speaker-less machines.

package notify

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/godbus/dbus/v5"
)

const (
	notifInterface = "org.freedesktop.Notifications"
	notifPath      = "/org/freedesktop/Notifications"

	// kdialogPopupSeconds is the passive-popup duration kdialog requires as
	// an explicit argument (in seconds). The notification backends otherwise
	// leave the duration to the server's default.
	kdialogPopupSeconds = 5
)

var (
	connMu sync.Mutex
	conn   *dbus.Conn
)

// sessionConn returns a live session-bus connection, reopening it if needed.
func sessionConn() (*dbus.Conn, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if conn != nil {
		// The godbus read loop stops when the bus goes away; drop and retry.
		if conn.Connected() {
			return conn, nil
		}
		_ = conn.Close()
		conn = nil
	}
	c, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("notify: connect to session bus: %w", err)
	}
	conn = c
	return conn, nil
}

// show posts a desktop notification. Safe from any goroutine. An empty name
// falls back to the running executable's base name. Delivery tries the
// session-bus Notify call first, then the notify-send and kdialog CLIs; when
// all three are unavailable the error wraps ErrUnavailable.
func show(name, title, message string, opts Options) error {
	if name == "" {
		name = filepath.Base(os.Args[0])
	}
	err := dbusNotify(name, title, message, opts)
	if err == nil {
		return nil
	}
	err1 := notifySend(name, title, message, opts)
	if err1 == nil {
		return nil
	}
	err2 := kdialogPopup(title, message, opts)
	if err2 == nil {
		return nil
	}
	return fmt.Errorf("%w: session bus: %v; notify-send: %v; kdialog: %v",
		ErrUnavailable, err, err1, err2)
}

// dbusNotify posts the notification over the org.freedesktop.Notifications
// session-bus interface with the options encoded as app_icon and hints.
func dbusNotify(name, title, message string, opts Options) error {
	c, err := sessionConn()
	if err != nil {
		return err
	}
	appIcon := opts.Icon
	if appIcon != "" {
		// A path that exists is made absolute; anything else is passed
		// through as a themed icon name for the desktop to resolve.
		if _, err := os.Stat(appIcon); err == nil {
			appIcon, _ = filepath.Abs(appIcon)
		}
	}
	level := opts.Urgency.level()
	hints := map[string]dbus.Variant{
		"urgency": dbus.MakeVariant(byte(level)), // 0 low / 1 normal / 2 critical
	}
	if level == 2 {
		hints["sound-name"] = dbus.MakeVariant("bell") // themed alert sound
	}
	if len(opts.IconData) > 0 {
		rgba, err := bytesToRGBA(opts.IconData)
		if err != nil {
			return fmt.Errorf("notify: decode Options.IconData: %w", err)
		}
		hints["image-data"] = dbus.MakeVariant(imageDataHint(rgba))
	}
	obj := c.Object(notifInterface, notifPath)
	call := obj.Call(notifInterface+".Notify", 0,
		name,       // app_name
		uint32(0),  // replaces_id
		appIcon,    // app_icon
		title,      // summary
		message,    // body
		[]string{}, // actions
		hints,      // hints
		int32(-1),  // expire_timeout: server default
	)
	if call.Err != nil {
		// The daemon may have restarted; drop the stale connection.
		connMu.Lock()
		if conn == c {
			_ = conn.Close()
			conn = nil
		}
		connMu.Unlock()
		return fmt.Errorf("notify: %w", call.Err)
	}
	return nil
}

// imageData is the payload of the org.freedesktop.Notifications image-data
// hint. Its exported fields marshal to the required (iiibiiay) signature:
// width, height, row stride, has-alpha flag, bits per sample, channels and
// the raw bottom-up RGBA scanlines. Marshaling an anonymous []any would
// produce (av), which notification servers reject, so the concrete struct is
// what godbus encodes.
type imageData struct {
	Width         int32
	Height        int32
	RowStride     int32
	HasAlpha      bool
	BitsPerSample int32
	Channels      int32
	Data          []byte
}

// imageDataHint converts an RGBA image into the image-data hint payload.
// RowStride is the actual stride of Pix (which may include padding), HasAlpha
// is always true because the buffer is RGBA, and the depth is 8 bits over 4
// channels.
func imageDataHint(img *image.RGBA) imageData {
	b := img.Bounds()
	height := b.Dy()
	stride := img.Stride
	data := img.Pix
	if stride*height < len(data) {
		data = data[:stride*height]
	}
	return imageData{
		Width:         int32(b.Dx()), // #nosec G115 -- PNG dimensions bounded by image/png
		Height:        int32(height), // #nosec G115 -- PNG dimensions bounded by image/png
		RowStride:     int32(stride), // #nosec G115 -- PNG dimensions bounded by image/png
		HasAlpha:      true,
		BitsPerSample: 8,
		Channels:      4,
		Data:          data,
	}
}

// bytesToRGBA decodes PNG data into an *image.RGBA. Formats that decode to
// something else (paletted, gray, ...) are drawn into a fresh RGBA buffer so
// the pixels always have the 4-bytes-per-pixel layout the image-data hint
// expects.
func bytesToRGBA(data []byte) (*image.RGBA, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if img, ok := src.(*image.RGBA); ok {
		return img, nil
	}
	b := src.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	return img, nil
}

// cliIcon resolves the icon argument for the notify-send and kdialog CLIs,
// which take a file path or icon name on the command line. PNG bytes cannot
// cross a command line, so they are written to a temporary file whose removal
// the returned cleanup performs.
func cliIcon(opts Options) (icon string, cleanup func(), err error) {
	cleanup = func() {}
	if len(opts.IconData) > 0 {
		f, err := os.CreateTemp("", "tuohi-notify-*.png")
		if err != nil {
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		if _, err := f.Write(opts.IconData); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
	}
	if opts.Icon != "" {
		if _, err := os.Stat(opts.Icon); err == nil {
			icon, _ = filepath.Abs(opts.Icon)
		} else {
			icon = opts.Icon // themed icon name, resolved by the desktop
		}
	}
	return icon, cleanup, nil
}

// urgencyName is the notify-send spelling of a normalized urgency level.
func urgencyName(level int) string {
	switch level {
	case 0:
		return "low"
	case 2:
		return "critical"
	default:
		return "normal"
	}
}

// notifySend posts the notification through the notify-send CLI. The expire
// timeout is left to the server default so the fallback matches the D-Bus
// path's expire_timeout of -1.
func notifySend(name, title, message string, opts Options) error {
	bin, err := exec.LookPath("notify-send")
	if err != nil {
		return err
	}
	icon, cleanup, err := cliIcon(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{title, message, "-a", name}
	if icon != "" {
		args = append(args, "-i", icon)
	}
	// -t -1 is libnotify's NOTIFY_EXPIRES_DEFAULT: the server picks the
	// expiry, matching the D-Bus path's expire_timeout of -1.
	args = append(args, "-t", "-1", "-u", urgencyName(opts.Urgency.level()))
	return exec.Command(bin, args...).Run()
}

// kdialogPopup posts the notification through the kdialog passive popup. The
// popup duration is explicit (kdialog has no server default) and the icon is
// passed through as a file path or icon name.
func kdialogPopup(title, message string, opts Options) error {
	bin, err := exec.LookPath("kdialog")
	if err != nil {
		return err
	}
	icon, cleanup, err := cliIcon(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{"--title", title, "--passivepopup", message, strconv.Itoa(kdialogPopupSeconds)}
	if icon != "" {
		args = append(args, "--icon", icon)
	}
	return exec.Command(bin, args...).Run()
}

const (
	// linux/input-event-codes.h
	evSnd   = 0x12 // EV_SND: sound events
	sndTone = 0x02 // SND_TONE: tone on/off
)

// inputEvent mirrors struct input_event from linux/input.h - a timeval plus
// type/code/value - written to the pcspkr event device to start and stop a
// tone. The struct must match the kernel ABI byte for byte.
type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

// beep sounds the PC speaker. It needs the pcspkr module loaded and write
// access to /dev/input/by-path/platform-pcspkr-event-spkr (the input group);
// when the device cannot be opened it falls back to the terminal bell
// character, so a speaker-less machine still gets a (quieter) alert.
func beep(freq float64, duration int) error {
	if freq == 0 {
		freq = DefaultFreq
	} else if freq > 20000 {
		freq = 20000
	} else if freq < 0 {
		freq = DefaultFreq
	}
	if duration == 0 {
		duration = DefaultDuration
	}

	f, err := os.OpenFile("/dev/input/by-path/platform-pcspkr-event-spkr", os.O_WRONLY, 0644)
	if err != nil {
		// No PC speaker or no permission: output the only beep we can.
		if _, err := os.Stdout.Write([]byte{7}); err != nil {
			return fmt.Errorf("notify: beep: write bell to stdout: %w", err)
		}
		return nil
	}
	defer func() { _ = f.Close() }()

	ev := inputEvent{Type: evSnd, Code: sndTone, Value: int32(freq)} // #nosec G115 -- freq clamped to <= 20000 above
	raw := *(*[unsafe.Sizeof(ev)]byte)(unsafe.Pointer(&ev))

	// Start the tone.
	if _, err := f.Write(raw[:]); err != nil {
		return fmt.Errorf("notify: beep: write tone start to pcspkr: %w", err)
	}

	time.Sleep(time.Duration(duration) * time.Millisecond)

	// Stop the tone.
	ev.Value = 0
	raw = *(*[unsafe.Sizeof(ev)]byte)(unsafe.Pointer(&ev))
	if _, err := f.Write(raw[:]); err != nil {
		return fmt.Errorf("notify: beep: write tone stop to pcspkr: %w", err)
	}
	return nil
}

// alertSound backs Alert: a critical notification on Linux also beeps.
func alertSound() error {
	return beep(DefaultFreq, DefaultDuration)
}
