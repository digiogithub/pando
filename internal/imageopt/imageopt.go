// Package imageopt provides a single, reusable image normalization pipeline
// used before images are sent to LLM providers, both for user attachments and
// for tool-result images (e.g. browser screenshots).
//
// The optimization is intentionally provider-agnostic: it decodes the image,
// resizes it down to the model's vision long-side limit, and recompresses it
// (with progressive shrinking) so the encoded payload stays within a byte
// budget. Sending a 4K photo untouched gives the model no extra detail because
// the provider rescales it anyway — it only costs bandwidth and latency.
package imageopt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/disintegration/imaging"

	// Register decoders for the formats we accept. webp is decode-only.
	_ "golang.org/x/image/webp"
)

// Supported MIME types (decode). Animations use only the first frame.
const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
	MIMEWEBP = "image/webp"
	MIMEGIF  = "image/gif"
)

// Options controls how an image is normalized. A zero value disables all work
// (AutoResize false), returning the input untouched.
type Options struct {
	// AutoResize enables the resize/recompress pipeline. When false, Normalize
	// returns the input unchanged.
	AutoResize bool
	// MaxLongSidePx caps the longest image side in pixels. When 0, DefaultLongSidePx.
	MaxLongSidePx int
	// MaxWidth / MaxHeight are optional additional pixel caps (0 = unbounded).
	MaxWidth  int
	MaxHeight int
	// MaxBase64Bytes caps the base64-encoded payload size. 0 = unbounded.
	MaxBase64Bytes int
	// Quality is the JPEG quality used when recompressing (1-100). 0 => 85.
	Quality int
}

// Meta describes the result of a Normalize call, for placeholders and logging.
type Meta struct {
	Width       int
	Height      int
	Bytes       int  // decoded output byte length
	Base64Bytes int  // base64-encoded length
	Resized     bool // true if the image was decoded and re-encoded
	MIMEType    string
}

// DetectMIME returns the image MIME type by inspecting magic bytes, not the
// file extension. Returns "" when the bytes are not a recognized image.
func DetectMIME(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return MIMEJPEG
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return MIMEPNG
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return MIMEWEBP
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return MIMEGIF
	default:
		return ""
	}
}

// ToDataURI returns a data: URI for the given image bytes and MIME type.
func ToDataURI(data []byte, mime string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func base64Len(n int) int { return (n + 2) / 3 * 4 }

// Normalize resizes and recompresses data according to opts. When the image is
// already within all limits (or AutoResize is false), the original bytes are
// returned unchanged. The returned MIME type may differ from the input (an
// oversized PNG photo is re-encoded as JPEG to hit the byte budget).
func Normalize(data []byte, mime string, opts Options) (out []byte, outMIME string, meta Meta, err error) {
	if mime == "" {
		mime = DetectMIME(data)
	}
	if mime == "" {
		return data, mime, Meta{Bytes: len(data), Base64Bytes: base64Len(len(data)), MIMEType: mime}, fmt.Errorf("imageopt: unrecognized image format")
	}

	longSide := opts.MaxLongSidePx
	if longSide <= 0 {
		longSide = DefaultLongSidePx
	}

	// Fast path: caller disabled optimization, or the image already fits every
	// limit — return it untouched to avoid re-encoding artifacts (esp. on text).
	cfg, _, decErr := image.DecodeConfig(bytes.NewReader(data))
	if !opts.AutoResize {
		return data, mime, Meta{Width: cfg.Width, Height: cfg.Height, Bytes: len(data), Base64Bytes: base64Len(len(data)), MIMEType: mime}, nil
	}
	if decErr == nil && withinLimits(cfg.Width, cfg.Height, len(data), longSide, opts) {
		return data, mime, Meta{Width: cfg.Width, Height: cfg.Height, Bytes: len(data), Base64Bytes: base64Len(len(data)), MIMEType: mime}, nil
	}

	img, err := decode(data, mime)
	if err != nil {
		// Can't decode: return original, let the caller/provider decide.
		return data, mime, Meta{Bytes: len(data), Base64Bytes: base64Len(len(data)), MIMEType: mime}, err
	}

	quality := opts.Quality
	if quality <= 0 {
		quality = 85
	}

	// Resize down to the pixel caps (never upscale).
	resized := fitToCaps(img, longSide, opts.MaxWidth, opts.MaxHeight)

	// Encode, then shrink progressively until the base64 payload fits the budget.
	encoded, encMIME, err := encode(resized, mime, quality)
	if err != nil {
		return data, mime, Meta{}, err
	}
	for opts.MaxBase64Bytes > 0 && base64Len(len(encoded)) > opts.MaxBase64Bytes {
		b := resized.Bounds()
		nextLong := maxInt(b.Dx(), b.Dy()) * 85 / 100 // shrink ~15% per step
		if nextLong < 64 {
			// Give up shrinking dimensions; drop JPEG quality as a last resort.
			if quality > 40 {
				quality -= 15
				encoded, encMIME, err = encodeJPEG(resized, quality)
				if err != nil {
					return data, mime, Meta{}, err
				}
				continue
			}
			break
		}
		resized = fitToCaps(resized, nextLong, 0, 0)
		encoded, encMIME, err = encode(resized, mime, quality)
		if err != nil {
			return data, mime, Meta{}, err
		}
	}

	rb := resized.Bounds()
	return encoded, encMIME, Meta{
		Width:       rb.Dx(),
		Height:      rb.Dy(),
		Bytes:       len(encoded),
		Base64Bytes: base64Len(len(encoded)),
		Resized:     true,
		MIMEType:    encMIME,
	}, nil
}

func withinLimits(w, h, byteLen, longSide int, opts Options) bool {
	if maxInt(w, h) > longSide {
		return false
	}
	if opts.MaxWidth > 0 && w > opts.MaxWidth {
		return false
	}
	if opts.MaxHeight > 0 && h > opts.MaxHeight {
		return false
	}
	if opts.MaxBase64Bytes > 0 && base64Len(byteLen) > opts.MaxBase64Bytes {
		return false
	}
	return true
}

// fitToCaps scales img down so its long side <= longSide and it fits within
// maxW/maxH (when > 0). Never upscales.
func fitToCaps(img image.Image, longSide, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	targetW, targetH := w, h
	if longSide > 0 && maxInt(w, h) > longSide {
		if w >= h {
			targetW = longSide
			targetH = 0
		} else {
			targetH = longSide
			targetW = 0
		}
	}
	res := img
	if targetW != w || targetH != h {
		if targetW == 0 || targetH == 0 {
			res = imaging.Resize(img, targetW, targetH, imaging.Lanczos)
		}
	}
	// Enforce explicit width/height caps with Fit (preserves aspect ratio).
	rb := res.Bounds()
	if (maxW > 0 && rb.Dx() > maxW) || (maxH > 0 && rb.Dy() > maxH) {
		fw, fh := maxW, maxH
		if fw == 0 {
			fw = rb.Dx()
		}
		if fh == 0 {
			fh = rb.Dy()
		}
		res = imaging.Fit(res, fw, fh, imaging.Lanczos)
	}
	return res
}

func decode(data []byte, mime string) (image.Image, error) {
	if mime == MIMEGIF {
		// Use only the first frame of an animation.
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(g.Image) == 0 {
			return nil, fmt.Errorf("imageopt: empty gif")
		}
		return g.Image[0], nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// encode re-encodes img. PNG stays PNG (lossless, good for UI/text). Everything
// else (jpeg/webp/gif) is emitted as JPEG, the widely supported lossy format.
func encode(img image.Image, srcMIME string, quality int) ([]byte, string, error) {
	if srcMIME == MIMEPNG {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), MIMEPNG, nil
	}
	return encodeJPEG(img, quality)
}

func encodeJPEG(img image.Image, quality int) ([]byte, string, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), MIMEJPEG, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
