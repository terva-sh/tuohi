package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// testImage is a 5×3 image, an odd width so BMP rows carry padding, with a
// distinct colour in every pixel and a varying alpha.
func testImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for y := range 3 {
		for x := range 5 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(40 * x), G: uint8(80 * y), B: uint8(10*x + 20*y + 5), A: uint8(255 - 30*x - 10*y)})
		}
	}
	return img
}

// bmpSpec says how makeDIB lays a bitmap out.
type bmpSpec struct {
	header      int  // 12, 40, 56, 108 or 124
	bpp         int  // 1, 8, 16, 24 or 32
	compression int  // biRGB or biBitfields
	topDown     bool // rows top to bottom, a negative height
	zeroAlpha   bool // 32 bits: leave the fourth byte zero
	palette     [][3]uint8
}

// makeDIB writes img as a device-independent bitmap without a file header.
// A paletted bitmap writes each pixel as the index of its exact colour in
// spec.palette.
func makeDIB(t *testing.T, img *image.NRGBA, spec bmpSpec) []byte {
	t.Helper()
	le := binary.LittleEndian
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var buf bytes.Buffer
	hdr := make([]byte, spec.header)
	le.PutUint32(hdr, uint32(spec.header))
	if spec.header == 12 {
		le.PutUint16(hdr[4:], uint16(w))
		le.PutUint16(hdr[6:], uint16(h))
		le.PutUint16(hdr[8:], 1)
		le.PutUint16(hdr[10:], uint16(spec.bpp))
	} else {
		le.PutUint32(hdr[4:], uint32(w))
		height := int32(h)
		if spec.topDown {
			height = -height
		}
		le.PutUint32(hdr[8:], uint32(height))
		le.PutUint16(hdr[12:], 1)
		le.PutUint16(hdr[14:], uint16(spec.bpp))
		le.PutUint32(hdr[16:], uint32(spec.compression))
		le.PutUint32(hdr[32:], uint32(len(spec.palette)))
	}
	masks := []uint32{0x00ff0000, 0x0000ff00, 0x000000ff, 0xff000000}
	if spec.compression == biBitfields && spec.bpp == 16 {
		masks = []uint32{0xf800, 0x07e0, 0x001f, 0} // 5-6-5
	}
	if spec.compression == biBitfields && spec.header >= 56 {
		for i, m := range masks {
			le.PutUint32(hdr[40+4*i:], m)
		}
	}
	buf.Write(hdr)
	if spec.compression == biBitfields && spec.header == 40 {
		for _, m := range masks[:3] {
			_ = binary.Write(&buf, le, m)
		}
	}
	for _, c := range spec.palette {
		if spec.header == 12 {
			buf.Write([]byte{c[2], c[1], c[0]})
		} else {
			buf.Write([]byte{c[2], c[1], c[0], 0})
		}
	}
	stride := (w*spec.bpp + 31) / 32 * 4
	for i := range h {
		y := h - 1 - i
		if spec.topDown {
			y = i
		}
		row := make([]byte, stride)
		for x := range w {
			c := img.NRGBAAt(x, y)
			switch spec.bpp {
			case 1, 8:
				idx := -1
				for j, p := range spec.palette {
					if p == [3]uint8{c.R, c.G, c.B} {
						idx = j
					}
				}
				if idx < 0 {
					t.Fatalf("colour %v is not in the palette", c)
				}
				bit := x * spec.bpp
				row[bit/8] |= byte(idx << (8 - spec.bpp - bit%8))
			case 16:
				var v uint16
				if spec.compression == biBitfields {
					v = uint16(c.R>>3)<<11 | uint16(c.G>>2)<<5 | uint16(c.B>>3)
				} else {
					v = uint16(c.R>>3)<<10 | uint16(c.G>>3)<<5 | uint16(c.B>>3)
				}
				le.PutUint16(row[2*x:], v)
			case 24:
				copy(row[3*x:], []byte{c.B, c.G, c.R})
			case 32:
				a := c.A
				if spec.zeroAlpha {
					a = 0
				}
				copy(row[4*x:], []byte{c.B, c.G, c.R, a})
			}
		}
		buf.Write(row)
	}
	return buf.Bytes()
}

// withFileHeader prefixes a DIB with the BMP file header.
func withFileHeader(dib []byte, pixOff int) []byte {
	out := make([]byte, 14, 14+len(dib))
	out[0], out[1] = 'B', 'M'
	binary.LittleEndian.PutUint32(out[2:], uint32(14+len(dib)))
	binary.LittleEndian.PutUint32(out[10:], uint32(14+pixOff))
	return append(out, dib...)
}

// opaque returns img with every alpha at 255.
func opaque(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 0xff
	}
	return out
}

// quantize returns img with each channel cut to its top bits, as 16-bit
// pixels carry it, and scaled back to eight bits as decodeDIB scales it.
func quantize(img *image.NRGBA, rBits, gBits, bBits int) *image.NRGBA {
	out := opaque(img)
	scale := func(v uint8, n int) uint8 { return uint8(int(v>>(8-n)) * 255 / (1<<n - 1)) }
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i] = scale(out.Pix[i], rBits)
		out.Pix[i+1] = scale(out.Pix[i+1], gBits)
		out.Pix[i+2] = scale(out.Pix[i+2], bBits)
	}
	return out
}

// sameImage fails unless got has want's size and NRGBA pixels.
func sameImage(t *testing.T, got image.Image, want *image.NRGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds %v, want %v", got.Bounds(), want.Bounds())
	}
	for y := range want.Bounds().Dy() {
		for x := range want.Bounds().Dx() {
			g := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			if w := want.NRGBAAt(x, y); g != w {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, g, w)
			}
		}
	}
}

var limits = ImageLimits{}.withDefaults()

func TestDecodeDIB(t *testing.T) {
	src := testImage()
	// A palette of every colour in the opaque image, and a two-colour image
	// for the one-bit palette.
	var palette [][3]uint8
	for i := 0; i < len(src.Pix); i += 4 {
		palette = append(palette, [3]uint8{src.Pix[i], src.Pix[i+1], src.Pix[i+2]})
	}
	mono := image.NewNRGBA(src.Bounds())
	for y := range 3 {
		for x := range 5 {
			if (x+y)%2 == 0 {
				mono.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			} else {
				mono.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
			}
		}
	}
	cases := []struct {
		name string
		img  *image.NRGBA
		spec bmpSpec
		want *image.NRGBA
	}{
		{"24-bit", src, bmpSpec{header: 40, bpp: 24}, opaque(src)},
		{"24-bit top-down", src, bmpSpec{header: 40, bpp: 24, topDown: true}, opaque(src)},
		{"24-bit core header", src, bmpSpec{header: 12, bpp: 24}, opaque(src)},
		{"32-bit with alpha", src, bmpSpec{header: 40, bpp: 32}, src},
		{"32-bit, alpha all zero", src, bmpSpec{header: 40, bpp: 32, zeroAlpha: true}, opaque(src)},
		{"32-bit bitfields after a V1 header", src, bmpSpec{header: 40, bpp: 32, compression: biBitfields, zeroAlpha: true}, opaque(src)},
		{"32-bit bitfields in a V5 header", src, bmpSpec{header: 124, bpp: 32, compression: biBitfields}, src},
		{"32-bit bitfields in a V4 header, top-down", src, bmpSpec{header: 108, bpp: 32, compression: biBitfields, topDown: true}, src},
		{"16-bit 5-5-5", src, bmpSpec{header: 40, bpp: 16}, quantize(src, 5, 5, 5)},
		{"16-bit 5-6-5 bitfields", src, bmpSpec{header: 40, bpp: 16, compression: biBitfields}, quantize(src, 5, 6, 5)},
		{"8-bit palette", src, bmpSpec{header: 40, bpp: 8, palette: palette}, opaque(src)},
		{"8-bit palette, core header", src, bmpSpec{header: 12, bpp: 8, palette: palette}, opaque(src)},
		{"1-bit palette", mono, bmpSpec{header: 40, bpp: 1, palette: [][3]uint8{{0, 0, 0}, {255, 255, 255}}}, mono},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.spec.header == 12 && len(c.spec.palette) > 0 && c.spec.bpp == 8 {
				// The core header has no colour count, so its palette is
				// always 1 << bpp entries long.
				for len(c.spec.palette) < 256 {
					c.spec.palette = append(c.spec.palette, [3]uint8{})
				}
			}
			dib := makeDIB(t, c.img, c.spec)
			img, err := decodeDIB(dib, limits)
			if err != nil {
				t.Fatalf("decodeDIB: %v", err)
			}
			sameImage(t, img, c.want)

			// The same bitmap as a BMP file, through the pixel offset its
			// file header names.
			pixOff := len(dib) - (c.img.Bounds().Dx()*c.spec.bpp+31)/32*4*c.img.Bounds().Dy()
			img, err = decodeBMP(withFileHeader(dib, pixOff), limits)
			if err != nil {
				t.Fatalf("decodeBMP: %v", err)
			}
			sameImage(t, img, c.want)
		})
	}
}

func TestDecodeDIBRefuses(t *testing.T) {
	dib := makeDIB(t, testImage(), bmpSpec{header: 40, bpp: 24})
	huge := append([]byte(nil), dib...)
	binary.LittleEndian.PutUint32(huge[4:], 100000)
	binary.LittleEndian.PutUint32(huge[8:], 100000)
	rle := append([]byte(nil), dib...)
	binary.LittleEndian.PutUint32(rle[16:], 1) // BI_RLE8
	header := append([]byte(nil), dib...)
	binary.LittleEndian.PutUint32(header, 64) // OS/2 2.x
	zero := append([]byte(nil), dib...)
	binary.LittleEndian.PutUint32(zero[4:], 0)

	for name, c := range map[string]struct {
		dib      []byte
		tooLarge bool
	}{
		"empty":            {nil, false},
		"truncated header": {dib[:20], false},
		"truncated pixels": {dib[:len(dib)-1], false},
		"over the pixels":  {huge, true},
		"run-length":       {rle, false},
		"OS/2 2.x header":  {header, false},
		"no width":         {zero, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeDIB(c.dib, limits)
			if err == nil {
				t.Fatal("decodeDIB succeeded")
			}
			if errors.Is(err, ErrImageTooLarge) != c.tooLarge {
				t.Fatalf("decodeDIB = %v, ErrImageTooLarge %v", err, c.tooLarge)
			}
		})
	}
	for name, bmp := range map[string][]byte{
		"no file header":    dib,
		"offset past end":   withFileHeader(dib, len(dib)+1),
		"short file header": []byte("BM"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeBMP(bmp, limits); err == nil {
				t.Fatal("decodeBMP succeeded")
			}
		})
	}
}

func TestEncodeImage(t *testing.T) {
	src := testImage()
	encode := func(f func(*bytes.Buffer) error) []byte {
		var b bytes.Buffer
		if err := f(&b); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	pngData := encode(func(b *bytes.Buffer) error { return png.Encode(b, src) })
	gifData := encode(func(b *bytes.Buffer) error { return gif.Encode(b, src, nil) })
	jpegData := encode(func(b *bytes.Buffer) error { return jpeg.Encode(b, opaque(src), nil) })
	bmpData := withFileHeader(makeDIB(t, src, bmpSpec{header: 40, bpp: 24}), 40)

	for _, c := range []struct {
		mimeType string
		data     []byte
		exact    *image.NRGBA // the pixels a lossless format must keep
	}{
		{"image/png", pngData, src},
		{"image/bmp", bmpData, opaque(src)},
		{"image/x-MS-bmp", bmpData, opaque(src)},
		{"image/jpeg", jpegData, nil},
		{"image/gif", gifData, nil},
	} {
		t.Run(c.mimeType, func(t *testing.T) {
			out, err := encodeImage(c.mimeType, c.data, limits)
			if err != nil {
				t.Fatalf("encodeImage: %v", err)
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("the result is not a PNG: %v", err)
			}
			if img.Bounds() != src.Bounds() {
				t.Fatalf("bounds %v, want %v", img.Bounds(), src.Bounds())
			}
			if c.exact != nil {
				sameImage(t, img, c.exact)
			}
		})
	}

	t.Run("limits", func(t *testing.T) {
		for name, c := range map[string]struct {
			mimeType string
			data     []byte
			limits   ImageLimits
		}{
			"input over MaxBytes":   {"image/png", pngData, ImageLimits{MaxBytes: len(pngData) - 1, MaxPixels: 100}},
			"PNG over MaxPixels":    {"image/png", pngData, ImageLimits{MaxBytes: 1 << 20, MaxPixels: 14}},
			"BMP over MaxPixels":    {"image/bmp", bmpData, ImageLimits{MaxBytes: 1 << 20, MaxPixels: 14}},
			"JPEG over MaxPixels":   {"image/jpeg", jpegData, ImageLimits{MaxBytes: 1 << 20, MaxPixels: 14}},
			"GIF over MaxPixels":    {"image/gif", gifData, ImageLimits{MaxBytes: 1 << 20, MaxPixels: 14}},
			"BMP exactly at limits": {"image/bmp", bmpData, ImageLimits{MaxBytes: len(bmpData), MaxPixels: 15}},
		} {
			t.Run(name, func(t *testing.T) {
				_, err := encodeImage(c.mimeType, c.data, c.limits)
				if name == "BMP exactly at limits" {
					// The PNG of a 5×3 image is smaller than its BMP.
					if err != nil {
						t.Fatalf("encodeImage = %v, want success", err)
					}
					return
				}
				if !errors.Is(err, ErrImageTooLarge) {
					t.Fatalf("encodeImage = %v, want ErrImageTooLarge", err)
				}
			})
		}
	})

	t.Run("PNG over MaxBytes", func(t *testing.T) {
		if _, err := encodePNG(src, ImageLimits{MaxBytes: 20}); !errors.Is(err, ErrImageTooLarge) {
			t.Fatalf("encodePNG = %v, want ErrImageTooLarge", err)
		}
	})

	t.Run("refuses", func(t *testing.T) {
		if _, err := encodeImage("image/tiff", pngData, limits); err == nil || errors.Is(err, ErrImageTooLarge) {
			t.Fatalf("an unknown type = %v, want a decode error", err)
		}
		if _, err := encodeImage("image/png", jpegData, limits); err == nil || errors.Is(err, ErrImageTooLarge) {
			t.Fatalf("JPEG offered as PNG = %v, want a decode error", err)
		}
	})
}

func TestImageLimitsDefaults(t *testing.T) {
	got := ImageLimits{MaxBytes: -1}.withDefaults()
	if got.MaxBytes != DefaultMaxImageBytes || got.MaxPixels != DefaultMaxImagePixels {
		t.Fatalf("defaults = %+v", got)
	}
	got = ImageLimits{MaxBytes: 10, MaxPixels: 20}.withDefaults()
	if got.MaxBytes != 10 || got.MaxPixels != 20 {
		t.Fatalf("set limits changed: %+v", got)
	}
}

func TestBestImageType(t *testing.T) {
	for _, c := range []struct {
		offered []string
		want    string
	}{
		{[]string{"text/html", "image/bmp"}, "image/bmp"}, // WSLg
		{[]string{"image/bmp", "image/png", "TARGETS"}, "image/png"},
		{[]string{"image/x-ms-bmp"}, "image/x-ms-bmp"},
		{[]string{"image/jpeg", "image/gif"}, "image/jpeg"},
		{[]string{"UTF8_STRING", "text/plain", "image/tiff"}, ""},
		{nil, ""},
	} {
		if got := bestImageType(c.offered); got != c.want {
			t.Errorf("bestImageType(%q) = %q, want %q", c.offered, got, c.want)
		}
	}
}
