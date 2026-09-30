// Command notify demonstrates OS-level notifications through the
// notify subpackage.
//
// Notifications are standalone - no tray icon and no window are needed - so
// this demo fires a few notifications at the platform's notification
// subsystem (Win32 balloon tips, macOS Notification Center, Linux
// org.freedesktop.Notifications via D-Bus) and then plays a Beep, exiting
// after short, bounded pauses so the desktop has time to present them.
//
// It showcases the extended API: a plain Show, a ShowOpts with a custom icon
// (an in-memory PNG on Linux/macOS, a stock icon name on Windows, which does
// not decode PNG bytes), an Alert (critical urgency + the platform's
// attention sound) and a Beep.
//
// Every step is optional, so the demo also exits cleanly when the environment
// has no notification service - a headless box (Linux without a session bus
// or notify-send/kdialog) or an unbundled macOS binary. Those cases report
// the skipped step through ErrUnavailable/ErrUnsupported and the demo still
// exits 0; only genuine failures fail the run.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"time"

	"github.com/terva-sh/tuohi/notify"
)

const source = "tuohi notify demo"

func main() {
	icon, err := makeIconPNG()
	if err != nil {
		fmt.Println("icon generation error:", err)
		os.Exit(1)
	}

	ok := true
	report := func(step string, err error) {
		switch {
		case err == nil:
			fmt.Printf("[%s] %s\n", timestamp(), step)
		case errors.Is(err, notify.ErrUnsupported):
			fmt.Printf("[%s] %s - skipped: %v\n", timestamp(), step, err)
		case errors.Is(err, notify.ErrUnavailable):
			fmt.Printf("[%s] %s - skipped: no notification service in this environment (%v)\n", timestamp(), step, err)
		default:
			fmt.Printf("[%s] %s - FAILED: %v\n", timestamp(), step, err)
			ok = false
		}
	}

	report("plain notification", notify.Show(source, "Information", "This is an informational message from tuohi."))
	time.Sleep(1500 * time.Millisecond)

	// Custom icon: in-memory PNG bytes on Linux/macOS; on Windows use a stock
	// icon name, because Windows only loads .ico/.bmp files from disk.
	opts := notify.Options{}
	if runtime.GOOS == "windows" {
		opts.Icon = "warning"
	} else {
		opts.IconData = icon
	}
	report("notification with a custom icon", notify.ShowOpts(source, "Custom icon", "The icon beside this message was generated in memory.", opts))
	time.Sleep(1500 * time.Millisecond)

	report("alert (critical + attention sound)", notify.Alert(source, "Alert", "This notification demands attention.", notify.Options{}))
	time.Sleep(500 * time.Millisecond)

	// On Linux the tone needs a PC speaker (pcspkr); without one Beep falls
	// back to the terminal bell character, which is why the demo stays
	// usable on speaker-less machines.
	report("beep", notify.Beep(notify.DefaultFreq, 150))

	// Stay alive briefly so the desktop can present the notifications (and,
	// on macOS, so a non-bundled binary keeps its notification session).
	time.Sleep(1500 * time.Millisecond)
	fmt.Println("Done.")
	if !ok {
		os.Exit(1)
	}
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}

// makeIconPNG renders a 48x48 filled circle as a PNG in memory, so the demo
// carries no binary assets and works from any working directory.
func makeIconPNG() ([]byte, error) {
	const size = 48
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := size/2 - 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, color.RGBA{R: 0xE8, G: 0x7D, B: 0x1E, A: 0xFF}) // the demo icon's orange
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
