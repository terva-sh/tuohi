//go:build linux || freebsd || netbsd

package tuohi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/terva-sh/tuohi/clipboard"
)

var resClipboardImage atomic.Value // string

// clipboardOwnerEnv makes the test binary a clipboard owner instead of a test
// run: TestMain shows a window, puts the offer it names on the clipboard,
// prints "ready", and serves the clipboard until its stdin closes.
const clipboardOwnerEnv = "TUOHI_TEST_CLIPBOARD_OWNER"

// clipboardTestImage is 400×300 of seeded noise with a varying alpha, so
// neither PNG nor BMP compresses it small: its BMP, 360 kB, crosses X11's
// INCR threshold and takes several of GTK 4's stream reads.
func clipboardTestImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32())
		img.Pix[i+3] = uint8(128 + i/4%128)
	}
	return img
}

// opaqueCopy returns img with every alpha at 255, as a 24-bit BMP holds it.
func opaqueCopy(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 0xff
	}
	return out
}

// bmpFile encodes img as a 24-bit bottom-up BMP, the layout a Windows CF_DIB
// usually has and the one WSLg offers as image/bmp.
func bmpFile(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	stride := (w*3 + 3) &^ 3
	out := make([]byte, 54+stride*h)
	le := binary.LittleEndian
	out[0], out[1] = 'B', 'M'
	le.PutUint32(out[2:], uint32(len(out)))
	le.PutUint32(out[10:], 54)
	le.PutUint32(out[14:], 40)
	le.PutUint32(out[18:], uint32(w))
	le.PutUint32(out[22:], uint32(h))
	le.PutUint16(out[26:], 1)
	le.PutUint16(out[28:], 24)
	for y := range h {
		row := out[54+(h-1-y)*stride:]
		for x := range w {
			c := img.NRGBAAt(x, y)
			row[3*x], row[3*x+1], row[3*x+2] = c.B, c.G, c.R
		}
	}
	return out
}

func pngFile(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// clipOffer is one format a clipboard owner offers.
type clipOffer struct {
	mime string
	data []byte
}

// clipboardOffers are the offers a clipboard owner process can make.
func clipboardOffers(name string) []clipOffer {
	img := clipboardTestImage()
	switch name {
	case "png":
		return []clipOffer{{"image/png", pngFile(img)}}
	case "wslg":
		// What WSLg offers for an image copied in a Windows browser.
		return []clipOffer{
			{"text/html", []byte(`<img src="https://example.invalid/a.png">`)},
			{"image/bmp", bmpFile(img)},
		}
	}
	return nil
}

// clipboardOwner is the body of a clipboard owner process: name is "text"
// for text only, or an offer of clipboardOffers.
func clipboardOwner(name string) int {
	w := &View{Width: 200, Height: 100}
	if err := testApp().Show(w); err != nil {
		fmt.Fprintln(os.Stderr, "clipboard owner:", err)
		return 1
	}
	if name == "text" {
		if err := clipboard.Copy("tuohi clipboard owner"); err != nil {
			fmt.Fprintln(os.Stderr, "clipboard owner:", err)
			return 1
		}
	} else if err := offerClipboard(clipboardOffers(name)); err != nil {
		fmt.Fprintln(os.Stderr, "clipboard owner:", err)
		return 1
	}
	fmt.Println("ready")
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		w.Close()
	}()
	w.w.Run()
	return 0
}

var (
	gtk3Offers []clipOffer // what the GTK 3 get function serves
	gtk3GetFn  uintptr
)

// offerClipboard puts offers on the clipboard, each format with its bytes,
// the way an application that copies an image does. It runs on the UI
// thread.
func offerClipboard(offers []clipOffer) error {
	if gtk4 {
		var gBytesNew func(data *byte, size uintptr) uintptr
		var gBytesUnref func(uintptr)
		var newForBytes func(mime string, bytes uintptr) uintptr
		var newUnion func(providers *uintptr, n uintptr) uintptr
		var getDefault func() uintptr
		var getClipboard func(uintptr) uintptr
		var setContent func(clipboard, provider uintptr) bool
		var unref func(uintptr)
		purego.RegisterLibFunc(&gBytesNew, glibLib, "g_bytes_new")
		purego.RegisterLibFunc(&gBytesUnref, glibLib, "g_bytes_unref")
		purego.RegisterLibFunc(&newForBytes, gtkLib, "gdk_content_provider_new_for_bytes")
		purego.RegisterLibFunc(&newUnion, gtkLib, "gdk_content_provider_new_union")
		purego.RegisterLibFunc(&getDefault, gtkLib, "gdk_display_get_default")
		purego.RegisterLibFunc(&getClipboard, gtkLib, "gdk_display_get_clipboard")
		purego.RegisterLibFunc(&setContent, gtkLib, "gdk_clipboard_set_content")
		purego.RegisterLibFunc(&unref, gtkLib, "g_object_unref")
		providers := make([]uintptr, len(offers))
		for i, o := range offers {
			b := gBytesNew(&o.data[0], uintptr(len(o.data))) // copies
			providers[i] = newForBytes(o.mime, b)
			gBytesUnref(b)
		}
		union := newUnion(&providers[0], uintptr(len(providers))) // takes the providers
		defer unref(union)
		if !setContent(getClipboard(getDefault()), union) {
			return errors.New("gdk_clipboard_set_content refused the offer")
		}
		return nil
	}

	var clipboardGet func(selection uintptr) uintptr
	var setWithData func(clipboard uintptr, targets unsafe.Pointer, n uint32, get, clear, data uintptr) bool
	var selectionSet func(sel, typ uintptr, format int32, data *byte, length int32)
	var selectionTarget func(sel uintptr) uintptr
	purego.RegisterLibFunc(&clipboardGet, gtkLib, "gtk_clipboard_get")
	purego.RegisterLibFunc(&setWithData, gtkLib, "gtk_clipboard_set_with_data")
	purego.RegisterLibFunc(&selectionSet, gtkLib, "gtk_selection_data_set")
	purego.RegisterLibFunc(&selectionTarget, gtkLib, "gtk_selection_data_get_target")
	if gtk3GetFn == 0 {
		// void (*GtkClipboardGetFunc)(GtkClipboard *, GtkSelectionData *,
		// guint info, gpointer).
		gtk3GetFn = purego.NewCallback(func(_, sel, info, _ uintptr) uintptr {
			if int(info) < len(gtk3Offers) {
				d := gtk3Offers[info].data
				selectionSet(sel, selectionTarget(sel), 8, &d[0], int32(len(d)))
			}
			return 0
		})
	}
	// GtkTargetEntry; GTK interns the names during the call.
	type targetEntry struct {
		target uintptr
		flags  uint32
		info   uint32
	}
	names := make([][]byte, len(offers))
	entries := make([]targetEntry, len(offers))
	for i, o := range offers {
		names[i] = append([]byte(o.mime), 0)
		entries[i] = targetEntry{target: uintptr(unsafe.Pointer(&names[i][0])), info: uint32(i)}
	}
	gtk3Offers = offers
	ok := setWithData(clipboardGet(69 /* GDK_SELECTION_CLIPBOARD */), unsafe.Pointer(&entries[0]), uint32(len(entries)), gtk3GetFn, 0, 0)
	runtime.KeepAlive(names)
	if !ok {
		return errors.New("gtk_clipboard_set_with_data refused the offer")
	}
	return nil
}

// ownerProcess is a clipboard owner the scenario started.
type ownerProcess struct {
	cmd   *exec.Cmd
	stdin io.Closer
}

// startOwner starts a clipboard owner process making offer and waits until
// it has set the clipboard.
func startOwner(offer string) (*ownerProcess, error) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), clipboardOwnerEnv+"="+offer)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &ownerProcess{cmd: cmd, stdin: stdin}
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- s
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case s := <-line:
		if strings.TrimSpace(s) == "ready" {
			return p, nil
		}
		p.stop()
		return nil, fmt.Errorf("owner %s said %q", offer, s)
	case <-time.After(30 * time.Second):
		p.stop()
		return nil, fmt.Errorf("owner %s not ready in 30s", offer)
	}
}

// stop closes the owner's stdin, which ends it, and waits for it to exit.
func (p *ownerProcess) stop() {
	_ = p.stdin.Close()
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
	}
}

// pasteUntil calls PasteImage until it returns something other than
// ErrNoImage or the deadline passes. GTK 4 learns the formats of a new
// clipboard owner asynchronously, after the owner says it is ready.
func pasteUntil(limits clipboard.ImageLimits) ([]byte, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := clipboard.PasteImage(limits)
		if !errors.Is(err, clipboard.ErrNoImage) || time.Now().After(deadline) {
			return data, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// checkPNG reports how data differs from a PNG of want, "ok" when it does
// not.
func checkPNG(data []byte, err error, want *image.NRGBA) string {
	if err != nil {
		return "error " + err.Error()
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "not a PNG: " + err.Error()
	}
	if img.Bounds() != want.Bounds() {
		return fmt.Sprintf("bounds %v", img.Bounds())
	}
	for y := range want.Bounds().Dy() {
		for x := range want.Bounds().Dx() {
			if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got != want.NRGBAAt(x, y) {
				return fmt.Sprintf("pixel (%d,%d) = %v, want %v", x, y, got, want.NRGBAAt(x, y))
			}
		}
	}
	return "ok"
}

// clipboardImageScenario puts images on the clipboard from other processes,
// the way another application does, and reads them with PasteImage from a
// goroutine while the loop runs. It covers a PNG offer, WSLg's text/html and
// image/bmp offer, an offer of text only, both limits, and an image this
// process put on the clipboard itself.
//
// On Wayland the owners are left out: a compositor gives the clipboard only
// to a client with recent input, which a window that just opened has not
// had. GTK connects to the default wayland-0 socket even without
// WAYLAND_DISPLAY, so under WSLg the scenario runs this way unless
// GDK_BACKEND=x11 sends it to Xvfb; CI has no compositor and runs it all.
func clipboardImageScenario() string {
	w := &View{Width: 300, Height: 200}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	img := clipboardTestImage()
	wayland := waylandDisplay()
	run := func() string {
		var out []string
		report := func(name, result string) { out = append(out, name+"="+result) }
		withOwner := func(offer string, f func()) {
			p, err := startOwner(offer)
			if err != nil {
				report(offer, err.Error())
				return
			}
			defer p.stop()
			f()
		}

		if wayland {
			report("owners", "skipped on Wayland")
		} else {
			ownerCases(withOwner, report, img)
		}

		// This process as the owner, offering a BMP only.
		set := make(chan error, 1)
		w.w.Dispatch(func() { set <- offerClipboard([]clipOffer{{"image/bmp", bmpFile(img)}}) })
		if err := <-set; err != nil {
			report("local", err.Error())
		} else {
			data, err := pasteUntil(clipboard.ImageLimits{})
			report("local", checkPNG(data, err, opaqueCopy(img)))
		}
		return strings.Join(out, "|")
	}

	result := make(chan string, 1)
	watchdog := time.AfterFunc(120*time.Second, func() { w.Close() })
	defer watchdog.Stop()
	w.w.Dispatch(func() {
		go func() {
			defer w.Close()
			result <- run()
		}()
	})
	w.w.Run()
	select {
	case r := <-result:
		return r
	default:
		return "no report"
	}
}

// ownerCases reads what owner processes offer: text only, a PNG, and
// WSLg's text/html with image/bmp, at the default limits and over each.
func ownerCases(withOwner func(string, func()), report func(name, result string), img *image.NRGBA) {
	withOwner("text", func() {
		// Wait until the owner's text is what this process sees, so
		// the image read below is of the owner's offer.
		seen := false
		for deadline := time.Now().Add(10 * time.Second); !seen && time.Now().Before(deadline); {
			text, _ := clipboard.Paste()
			seen = text == "tuohi clipboard owner"
			time.Sleep(50 * time.Millisecond)
		}
		_, err := clipboard.PasteImage(clipboard.ImageLimits{})
		report("text", fmt.Sprintf("seen %v ErrNoImage %v", seen, errors.Is(err, clipboard.ErrNoImage)))
	})
	withOwner("png", func() {
		data, err := pasteUntil(clipboard.ImageLimits{})
		report("png", checkPNG(data, err, img))
	})
	withOwner("wslg", func() {
		data, err := pasteUntil(clipboard.ImageLimits{})
		report("wslg", checkPNG(data, err, opaqueCopy(img)))
		_, err = clipboard.PasteImage(clipboard.ImageLimits{MaxBytes: 100_000})
		report("maxbytes", fmt.Sprintf("ErrImageTooLarge %v", errors.Is(err, clipboard.ErrImageTooLarge)))
		_, err = clipboard.PasteImage(clipboard.ImageLimits{MaxPixels: 400*300 - 1})
		report("maxpixels", fmt.Sprintf("ErrImageTooLarge %v", errors.Is(err, clipboard.ErrImageTooLarge)))
	})
}

// TestClipboardPasteImage checks that PasteImage reads images other
// processes put on a real clipboard, as PNG, BMP-only, and WSLg's text/html
// with image/bmp offers, refuses text and images over its limits, and reads
// an image this process offers.
func TestClipboardPasteImage(t *testing.T) {
	got, _ := resClipboardImage.Load().(string)
	requireGUI(t, got)
	want := "text=seen true ErrNoImage true|png=ok|wslg=ok|maxbytes=ErrImageTooLarge true|maxpixels=ErrImageTooLarge true|local=ok"
	if strings.HasPrefix(got, "owners=") {
		t.Log("on Wayland only this process's own offer is read; GDK_BACKEND=x11 runs the owner processes too")
		want = "owners=skipped on Wayland|local=ok"
	}
	if got != want {
		t.Fatalf("clipboard image:\n got %s\nwant %s", got, want)
	}
}
