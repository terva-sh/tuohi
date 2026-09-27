package tuohi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// samplePNG returns a tiny valid PNG (the smallest thing AppKit will accept
// as an image).
func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0x55, B: 0x55, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the sample PNG: %v", err)
	}
	return buf.Bytes()
}

// The icon has to actually reach NSApplication - reading it back is the only
// way to know, since nothing about the call fails when AppKit quietly refuses
// the bytes.
func TestAppIconReachesTheApplication(t *testing.T) {
	if err := setAppIcon(samplePNG(t), ""); err != nil {
		t.Fatalf("setAppIcon: %v", err)
	}
	app := class("NSApplication").Send(sel("sharedApplication"))
	got := app.Send(sel("applicationIconImage"))
	if got == 0 {
		t.Fatal("the application has no icon after one was set")
	}
	// An NSImage built from bytes AppKit could not decode still exists as an
	// object but is not valid, which is exactly the failure a nil check misses.
	if got.Send(sel("isValid")) == 0 {
		t.Fatal("the icon AppKit holds is not a valid image")
	}
}

// An icon that is not an image must be reported, not silently ignored: a
// caller passing the wrong bytes deserves to hear about it once.
func TestAppIconRejectsWhatIsNotAnImage(t *testing.T) {
	if err := setAppIcon(nil, ""); err == nil {
		t.Error("an empty icon was accepted")
	}
	if err := setAppIcon([]byte("this is not a png"), ""); err == nil {
		t.Error("a string was accepted as an image")
	}
}

func TestKitDarwinPlatform(t *testing.T) {}

// Smoke test for the macOS backend that exercises the Objective-C marshaling
// (framework load, class lookup, NSString/NSURL/NSArray construction) without
// calling openURL:/activateFileViewerSelectingURLs:, which would actually launch
// the browser/Finder. Catches a broken framework path or selector name on CI.
func TestDarwinObjcMarshaling(t *testing.T) {
	err := openEnsureInit()
	if err != nil {
		t.Fatalf("openEnsureInit: %v", err)
	}
	_, err = checkedClass("NSWorkspace")
	if err != nil {
		t.Fatalf("class NSWorkspace: %v", err)
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		t.Fatalf("class NSURL: %v", err)
	}
	arrCls, err := checkedClass("NSArray")
	if err != nil {
		t.Fatalf("class NSArray: %v", err)
	}
	autorelease(func() {
		u := urlCls.Send(sel("URLWithString:"), nsstr("https://example.com"))
		if u == 0 {
			t.Error("URLWithString: returned nil")
		}
		fileURL := urlCls.Send(sel("fileURLWithPath:"), nsstr("/tmp"))
		if fileURL == 0 {
			t.Error("fileURLWithPath: returned nil")
		}
		arr := arrCls.Send(sel("arrayWithObject:"), fileURL)
		if arr == 0 {
			t.Error("arrayWithObject: returned nil")
		}
	})
}
