package tuohi

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

var resWinBackground atomic.Value // string

// TestDefaultBackgroundColorPacking checks the COREWEBVIEW2_COLOR values:
// the struct is {A, R, G, B}, so A is the low byte of the uint32 it travels
// as. 0x000000FF, which tuohi once used for white, is opaque black.
func TestDefaultBackgroundColorPacking(t *testing.T) {
	unpack := func(c uint32) [4]byte { return [4]byte{byte(c), byte(c >> 8), byte(c >> 16), byte(c >> 24)} }
	if got := unpack(defaultBackgroundColor(false)); got != [4]byte{0xFF, 0xFF, 0xFF, 0xFF} {
		t.Errorf("framed: A,R,G,B = %v, want opaque white", got)
	}
	if got := unpack(defaultBackgroundColor(true)); got[0] != 0 {
		t.Errorf("frameless: A = %d, want 0 (transparent)", got[0])
	}
}

// winBackgroundScenario shows a framed and a frameless view and reads back,
// through ICoreWebView2Controller2, the default background each got. The
// controller must answer the interface: a wrong IID makes it answer
// E_NOINTERFACE, and the background then silently stays WebView2's default.
func winBackgroundScenario() string {
	var out []string
	for _, frame := range []bool{true, false} {
		w := &View{Width: 200, Height: 150, Frame: frame}
		if err := testApp().Show(w); err != nil {
			return "new error: " + err.Error()
		}
		var ctrl2 uintptr
		hr := asController(native(w).controller).QueryInterface(&iidController2, &ctrl2)
		if ctrl2 == 0 {
			out = append(out, fmt.Sprintf("frame=%v: QueryInterface(Controller2) hr=0x%08x", frame, uint32(hr)))
			w.Close()
			continue
		}
		var color uint32
		hr = asController2(ctrl2).GetDefaultBackgroundColor(&color)
		asController2(ctrl2).Release()
		out = append(out, fmt.Sprintf("frame=%v: hr=0x%08x alpha=%d white=%v", frame, uint32(hr), byte(color), color == backgroundOpaqueWhite))
		w.Close()
	}
	return strings.Join(out, "|")
}

// TestDefaultBackgroundColor checks that a framed window gets an opaque
// white background and a frameless one a transparent background.
func TestDefaultBackgroundColor(t *testing.T) {
	got, _ := resWinBackground.Load().(string)
	requireGUI(t, got)
	want := "frame=true: hr=0x00000000 alpha=255 white=true|frame=false: hr=0x00000000 alpha=0 white=false"
	if got != want {
		t.Fatalf("default background:\n got %s\nwant %s", got, want)
	}
}
