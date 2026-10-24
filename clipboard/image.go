package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
)

// ErrNoImage is returned by PasteImage when the clipboard offers no image in
// a format it reads: it is empty, holds only text, or holds an image only in
// some other format.
var ErrNoImage = errors.New("clipboard: no image on the clipboard")

// ErrImageTooLarge is returned, wrapped with the sizes involved, by
// PasteImage when the clipboard image is over a limit in ImageLimits.
var ErrImageTooLarge = errors.New("clipboard: image over the limit")

// The defaults for a zero field of ImageLimits.
const (
	DefaultMaxImageBytes  = 32 << 20   // 32 MiB
	DefaultMaxImagePixels = 50_000_000 // about 8660 × 5770; 200 MB decoded
)

// ImageLimits bounds what PasteImage reads and decodes. A zero field takes
// its default.
type ImageLimits struct {
	// MaxBytes bounds the image as the clipboard encodes it and the PNG
	// PasteImage returns. Default DefaultMaxImageBytes.
	MaxBytes int
	// MaxPixels bounds the decoded image's width × height, checked from the
	// image's header before any pixel is decoded. Default
	// DefaultMaxImagePixels. Decoding takes four bytes a pixel, eight for a
	// 16-bit PNG.
	MaxPixels int
}

func (l ImageLimits) withDefaults() ImageLimits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultMaxImageBytes
	}
	if l.MaxPixels <= 0 {
		l.MaxPixels = DefaultMaxImagePixels
	}
	return l
}

// PasteImage returns the image on the system clipboard, encoded as PNG.
//
// It reads, in this order of preference, an image the clipboard offers as
// PNG, BMP, JPEG or GIF (the first frame), decodes it in Go and encodes it
// again as PNG, so what it returns is always a PNG that decodes, without the
// source's metadata. A clipboard holding no image in one of those formats,
// such as one holding only text, is ErrNoImage. An image over a limit is
// ErrImageTooLarge, and an image that does not decode is an error naming
// the format. No error carries image contents.
//
// On GTK the clipboard owner chooses the formats it offers. A Windows image
// pasted under WSLg arrives as BMP, which WebKitGTK does not hand to the
// page, so a paste event there carries no image file and PasteImage is the
// way to read it.
//
// GTK 4 stops reading at MaxBytes. GTK 3 receives the whole transfer before
// PasteImage can measure it, so there MaxBytes bounds what is kept and
// decoded, not what GTK buffers for the call. macOS and Windows return
// ErrUnsupported for now. Like Paste it needs a running tuohi app on GTK,
// and is safe to call from any goroutine.
func PasteImage(limits ImageLimits) ([]byte, error) {
	return pasteImage(limits.withDefaults())
}

// imageTypes are the MIME types PasteImage reads, most preferred first.
// PNG passes through losslessly at its own depth; BMP is how WSLg offers a
// Windows image; JPEG and GIF are what remains that the standard library
// decodes.
var imageTypes = []string{
	"image/png",
	"image/bmp", "image/x-bmp", "image/x-MS-bmp",
	"image/jpeg",
	"image/gif",
}

// bestImageType returns the type of imageTypes most preferred among
// offered, or "" when none is. X11 target names are case-sensitive, but
// owners spell the BMP types inconsistently.
func bestImageType(offered []string) string {
	for _, want := range imageTypes {
		for _, t := range offered {
			if strings.EqualFold(t, want) {
				return t
			}
		}
	}
	return ""
}

// encodeImage decodes data, offered as mimeType, checks it against limits
// and returns it as PNG.
func encodeImage(mimeType string, data []byte, limits ImageLimits) ([]byte, error) {
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("%w: %s is over %d bytes", ErrImageTooLarge, mimeType, limits.MaxBytes)
	}
	var decodeConfig func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	switch strings.ToLower(mimeType) {
	case "image/png":
		decodeConfig, decode = png.DecodeConfig, png.Decode
	case "image/jpeg":
		decodeConfig, decode = jpeg.DecodeConfig, jpeg.Decode
	case "image/gif":
		decodeConfig, decode = gif.DecodeConfig, gif.Decode
	case "image/bmp", "image/x-bmp", "image/x-ms-bmp":
		img, err := decodeBMP(data, limits)
		if err != nil {
			return nil, err
		}
		return encodePNG(img, limits)
	default:
		return nil, fmt.Errorf("clipboard: cannot decode %s", mimeType)
	}
	cfg, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("clipboard: decode %s: %w", mimeType, err)
	}
	if err := checkPixels(cfg.Width, cfg.Height, limits); err != nil {
		return nil, err
	}
	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("clipboard: decode %s: %w", mimeType, err)
	}
	return encodePNG(img, limits)
}

// checkPixels reports ErrImageTooLarge for an image of w × h over the pixel
// limit, and an error for one with no pixels.
func checkPixels(w, h int, limits ImageLimits) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("clipboard: image is %d×%d", w, h)
	}
	if int64(w)*int64(h) > int64(limits.MaxPixels) {
		return fmt.Errorf("%w: %d×%d is over %d pixels", ErrImageTooLarge, w, h, limits.MaxPixels)
	}
	return nil
}

// encodePNG encodes img, failing as soon as the PNG passes MaxBytes.
func encodePNG(img image.Image, limits ImageLimits) ([]byte, error) {
	w := &boundedBuffer{max: limits.MaxBytes}
	if err := png.Encode(w, img); err != nil {
		if errors.Is(err, ErrImageTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("clipboard: encode PNG: %w", err)
	}
	return w.buf.Bytes(), nil
}

// boundedBuffer is a bytes.Buffer that refuses to grow past max.
type boundedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.max {
		return 0, fmt.Errorf("%w: the PNG is over %d bytes", ErrImageTooLarge, b.max)
	}
	return b.buf.Write(p)
}
