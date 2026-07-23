package imageopt

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8((x * 7) % 256), uint8((y * 13) % 256), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestDetectMIME(t *testing.T) {
	if got := DetectMIME(makePNG(t, 4, 4)); got != MIMEPNG {
		t.Errorf("png: got %q", got)
	}
	if got := DetectMIME(makeJPEG(t, 8, 8)); got != MIMEJPEG {
		t.Errorf("jpeg: got %q", got)
	}
	if got := DetectMIME([]byte("not an image")); got != "" {
		t.Errorf("garbage: got %q, want empty", got)
	}
}

func TestNormalizeSkipsWhenWithinLimits(t *testing.T) {
	data := makePNG(t, 100, 80)
	out, mime, meta, err := Normalize(data, MIMEPNG, Options{AutoResize: true, MaxLongSidePx: 1568})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if meta.Resized {
		t.Errorf("expected no resize for small image")
	}
	if !bytes.Equal(out, data) {
		t.Errorf("expected untouched bytes")
	}
	if mime != MIMEPNG {
		t.Errorf("mime changed to %q", mime)
	}
}

func TestNormalizeResizesOversized(t *testing.T) {
	data := makePNG(t, 4000, 2000)
	_, _, meta, err := Normalize(data, MIMEPNG, Options{AutoResize: true, MaxLongSidePx: 1568})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !meta.Resized {
		t.Fatalf("expected resize")
	}
	if meta.Width != 1568 {
		t.Errorf("long side = %d, want 1568", meta.Width)
	}
	if meta.Height != 784 {
		t.Errorf("height = %d, want 784 (aspect preserved)", meta.Height)
	}
}

func TestNormalizeRespectsByteBudget(t *testing.T) {
	data := makeJPEG(t, 3000, 3000)
	budget := 200 * 1024 // 200 KB base64
	_, mime, meta, err := Normalize(data, MIMEJPEG, Options{
		AutoResize:     true,
		MaxLongSidePx:  2576,
		MaxBase64Bytes: budget,
		Quality:        85,
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if meta.Base64Bytes > budget {
		t.Errorf("base64 %d exceeds budget %d", meta.Base64Bytes, budget)
	}
	if mime != MIMEJPEG {
		t.Errorf("mime = %q", mime)
	}
}

func TestNormalizeAutoResizeOff(t *testing.T) {
	data := makePNG(t, 4000, 4000)
	out, _, meta, err := Normalize(data, MIMEPNG, Options{AutoResize: false, MaxLongSidePx: 1568})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if meta.Resized || !bytes.Equal(out, data) {
		t.Errorf("AutoResize=false must return input untouched")
	}
}

func TestCropClampsAndEncodes(t *testing.T) {
	data := makePNG(t, 200, 100)
	out, mime, err := Crop(data, MIMEPNG, Region{X: 150, Y: 50, W: 999, H: 999}, 90)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	if mime != MIMEPNG {
		t.Errorf("mime = %q", mime)
	}
	w, h, err := ImageDimensions(out)
	if err != nil {
		t.Fatalf("dims: %v", err)
	}
	if w != 50 || h != 50 {
		t.Errorf("clamped crop = %dx%d, want 50x50", w, h)
	}
}

func TestTileImageGrid(t *testing.T) {
	data := makePNG(t, 2048, 1024)
	tiles, err := TileImage(data, MIMEPNG, 1024, 0, 90)
	if err != nil {
		t.Fatalf("tile: %v", err)
	}
	if len(tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(tiles))
	}
	for i, tl := range tiles {
		if tl.Index != i {
			t.Errorf("tile %d has index %d", i, tl.Index)
		}
		if len(tl.Data) == 0 {
			t.Errorf("tile %d empty", i)
		}
	}
}
