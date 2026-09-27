//go:build linux || freebsd || netbsd

package notify

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestBytesToRGBA checks that PNG bytes decode into an *image.RGBA whose
// pixels match the source.
func TestBytesToRGBA(t *testing.T) {
	const w, h = 8, 5
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	src.SetRGBA(2, 3, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	src.SetRGBA(7, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := bytesToRGBA(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	if got := img.RGBAAt(2, 3); got != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Errorf("pixel (2,3) = %v, want opaque 10,20,30", got)
	}
	if got := img.RGBAAt(7, 0); got != (color.RGBA{R: 200, G: 100, B: 50, A: 255}) {
		t.Errorf("pixel (7,0) = %v, want opaque 200,100,50", got)
	}
}

// TestBytesToRGBAConvertsPaletted checks that PNGs that decode to a non-RGBA
// format (paletted) are still delivered as a plain *image.RGBA.
func TestBytesToRGBAConvertsPaletted(t *testing.T) {
	pal := color.Palette{color.RGBA{R: 0, G: 0, B: 0, A: 255}, color.RGBA{R: 255, G: 0, B: 0, A: 255}}
	src := image.NewPaletted(image.Rect(0, 0, 4, 2), pal)
	src.SetColorIndex(0, 0, 1)
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := bytesToRGBA(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := any(img).(*image.RGBA); !ok {
		t.Fatalf("decoded type = %T, want *image.RGBA", img)
	}
	if got := img.RGBAAt(0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Errorf("palette index 1 pixel = %v, want red", got)
	}
}

// TestBytesToRGBARejectsGarbage checks that non-image data is reported as an
// error instead of panicking.
func TestBytesToRGBARejectsGarbage(t *testing.T) {
	if _, err := bytesToRGBA([]byte("this is not a png")); err == nil {
		t.Fatal("bytesToRGBA on garbage must return an error")
	}
}

// TestImageDataHintSignature pins the D-Bus signature of the image-data hint
// payload to exactly (iiibiiay), which notification servers require.
func TestImageDataHintSignature(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	v := dbus.MakeVariant(imageDataHint(img))
	if got := v.Signature().String(); got != "(iiibiiay)" {
		t.Fatalf("image-data hint signature = %q, want (iiibiiay)", got)
	}
}
