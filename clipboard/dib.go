package clipboard

// A decoder for device-independent bitmaps: the BMP format, and CF_DIB and
// CF_DIBV5 on the Windows clipboard, which are BMP without the 14-byte file
// header. gdk-pixbuf's BMP loader is one of the optional loaders it stopped
// building by default in 2.42.11, and the Go standard library has none, so
// tuohi carries its own.
//
// It reads what clipboards carry: the OS/2 core header and the Windows
// header in its 40, 52, 56, 108 and 124 byte versions, top-down and
// bottom-up rows, 1, 4 and 8 bit palettes, 16 bit 5-5-5, 24 bit, and 32 bit,
// uncompressed or with bitfield masks. Run-length and embedded JPEG or PNG
// compression are refused. Colour profiles are ignored.
//
// A 32-bit bitmap's fourth byte is reserved in the original format and
// alpha in later use, and Windows programs fill it either way, so a 32-bit
// bitmap's alpha is honoured only when some pixel has a non-zero alpha: one
// whose alpha is all zero is opaque, not invisible. A 16-bit bitmap has
// alpha only through an explicit mask, which is honoured as it is.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math/bits"
)

const (
	biRGB            = 0
	biBitfields      = 3
	biAlphaBitfields = 6
)

// decodeBMP decodes a BMP file, file header included.
func decodeBMP(data []byte, limits ImageLimits) (image.Image, error) {
	if len(data) < 14 || data[0] != 'B' || data[1] != 'M' {
		return nil, errors.New("clipboard: decode BMP: no BMP file header")
	}
	off := binary.LittleEndian.Uint32(data[10:])
	if off < 14 || uint64(off) > uint64(len(data)) {
		return nil, fmt.Errorf("clipboard: decode BMP: pixel offset %d is outside the %d-byte file", off, len(data))
	}
	return decodeDIBAt(data[14:], int(off)-14, limits)
}

// decodeDIB decodes a bitmap without a file header, its pixels following
// its header, masks and palette.
func decodeDIB(dib []byte, limits ImageLimits) (image.Image, error) {
	return decodeDIBAt(dib, -1, limits)
}

// channel extracts one colour channel from a pixel through its bitfield
// mask, scaled to eight bits.
type channel struct {
	mask  uint32
	shift int
	width int
}

func newChannel(mask uint32) channel {
	if mask == 0 {
		return channel{}
	}
	shift := bits.TrailingZeros32(mask)
	return channel{mask: mask, shift: shift, width: bits.OnesCount32(mask >> shift)}
}

func (c channel) get(px uint32) uint8 {
	if c.mask == 0 {
		return 0
	}
	v := (px & c.mask) >> c.shift
	if c.width >= 8 {
		return uint8(v >> (c.width - 8))
	}
	return uint8(v * 255 / (1<<c.width - 1))
}

// decodeDIBAt decodes the bitmap in dib with its pixels at pixOff, or, when
// pixOff is negative, right after its header, masks and palette.
func decodeDIBAt(dib []byte, pixOff int, limits ImageLimits) (image.Image, error) {
	le := binary.LittleEndian
	if len(dib) < 4 {
		return nil, errors.New("clipboard: decode BMP: truncated header")
	}
	hsize := int(le.Uint32(dib))
	var (
		w, h        int64
		bpp         int
		compression uint32
		colors      int64
		topDown     bool
		entry       = 4 // bytes per palette entry
	)
	switch hsize {
	case 12: // BITMAPCOREHEADER
		if len(dib) < hsize {
			return nil, errors.New("clipboard: decode BMP: truncated header")
		}
		w, h = int64(le.Uint16(dib[4:])), int64(le.Uint16(dib[6:]))
		bpp, entry = int(le.Uint16(dib[10:])), 3
	case 40, 52, 56, 108, 124: // BITMAPINFOHEADER, its V2 and V3, BITMAPV4HEADER, BITMAPV5HEADER
		if len(dib) < hsize {
			return nil, errors.New("clipboard: decode BMP: truncated header")
		}
		w, h = int64(int32(le.Uint32(dib[4:]))), int64(int32(le.Uint32(dib[8:])))
		bpp = int(le.Uint16(dib[14:]))
		compression = le.Uint32(dib[16:])
		colors = int64(le.Uint32(dib[32:]))
	default:
		return nil, fmt.Errorf("clipboard: decode BMP: header of %d bytes is not supported", hsize)
	}
	if h < 0 {
		topDown, h = true, -h
	}
	if w > int64(limits.MaxPixels) || h > int64(limits.MaxPixels) {
		return nil, fmt.Errorf("%w: %d×%d is over %d pixels", ErrImageTooLarge, w, h, limits.MaxPixels)
	}
	if err := checkPixels(int(w), int(h), limits); err != nil {
		return nil, err
	}

	pos := hsize
	var r, g, b, a channel
	switch compression {
	case biRGB:
		switch bpp {
		case 16:
			r, g, b = newChannel(0x7c00), newChannel(0x03e0), newChannel(0x001f)
		case 32:
			r, g, b, a = newChannel(0x00ff0000), newChannel(0x0000ff00), newChannel(0x000000ff), newChannel(0xff000000)
		}
	case biBitfields, biAlphaBitfields:
		if bpp != 16 && bpp != 32 {
			return nil, fmt.Errorf("clipboard: decode BMP: bitfields at %d bits a pixel", bpp)
		}
		// The masks follow a 40-byte header, and are part of the later ones,
		// which carry an alpha mask from 56 bytes on.
		n := 3
		if hsize >= 56 || hsize == 40 && compression == biAlphaBitfields {
			n = 4
		}
		at := 40
		if hsize == 40 {
			if len(dib) < pos+4*n {
				return nil, errors.New("clipboard: decode BMP: truncated bitfield masks")
			}
			pos += 4 * n
		}
		mask := func(i int) uint32 { return le.Uint32(dib[at+4*i:]) }
		r, g, b = newChannel(mask(0)), newChannel(mask(1)), newChannel(mask(2))
		if n == 4 {
			a = newChannel(mask(3))
		}
	default:
		return nil, fmt.Errorf("clipboard: decode BMP: compression %d is not supported", compression)
	}

	var palette [][3]uint8
	switch bpp {
	case 1, 4, 8:
		n := int64(1) << bpp
		if colors > 0 && colors < n {
			n = colors
		}
		if int64(len(dib)-pos) < n*int64(entry) {
			return nil, errors.New("clipboard: decode BMP: truncated palette")
		}
		palette = make([][3]uint8, n)
		for i := range palette {
			e := dib[pos+i*entry:]
			palette[i] = [3]uint8{e[2], e[1], e[0]}
		}
		pos += int(n) * entry
	case 16, 24, 32:
		// An optional palette may follow, for displays with few colours.
		if colors > 0 && colors <= 1<<16 {
			pos += int(colors) * entry
		}
	default:
		return nil, fmt.Errorf("clipboard: decode BMP: %d bits a pixel is not supported", bpp)
	}
	if pixOff < 0 {
		pixOff = pos
	}
	if pixOff > len(dib) {
		return nil, errors.New("clipboard: decode BMP: truncated before its pixels")
	}

	stride := (w*int64(bpp) + 31) / 32 * 4
	if int64(len(dib)-pixOff) < stride*h {
		return nil, errors.New("clipboard: decode BMP: truncated pixels")
	}
	width, height := int(w), int(h)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	// masked splits a 16 or 32-bit pixel through the masks. A bitmap
	// without an alpha mask is opaque.
	masked := func(v uint32) [4]uint8 {
		px := [4]uint8{r.get(v), g.get(v), b.get(v), 0xff}
		if a.mask != 0 {
			px[3] = a.get(v)
		}
		return px
	}
	alpha := false // some pixel has a non-zero alpha
	for y := range height {
		row := dib[pixOff+y*int(stride):]
		dy := height - 1 - y
		if topDown {
			dy = y
		}
		out := img.Pix[dy*img.Stride:]
		for x := range width {
			var px [4]uint8 // R, G, B, A
			switch bpp {
			case 1, 4, 8:
				bit := x * bpp
				idx := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if idx < len(palette) {
					px = [4]uint8{palette[idx][0], palette[idx][1], palette[idx][2], 0xff}
				} else {
					px[3] = 0xff
				}
			case 16:
				px = masked(uint32(le.Uint16(row[2*x:])))
			case 24:
				p := row[3*x:]
				px = [4]uint8{p[2], p[1], p[0], 0xff}
			case 32:
				px = masked(le.Uint32(row[4*x:]))
			}
			alpha = alpha || px[3] != 0
			copy(out[4*x:], px[:])
		}
	}
	if bpp == 32 && !alpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	return img, nil
}
