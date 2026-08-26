package design

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func patchedPNG(t *testing.T, w, h int, base, patch color.RGBA, patchW, patchH int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < patchW && y < patchH {
				img.Set(x, y, patch)
				continue
			}
			img.Set(x, y, base)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestIdenticalRendersDoNotDiffer(t *testing.T) {
	image := solidPNG(t, 40, 40, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	diff, err := ComparePNG(image, image, 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if diff.Changed != 0 || diff.Fraction != 0 {
		t.Errorf("identical images differ: %+v", diff)
	}
	if diff.Total != 1600 {
		t.Errorf("compared %d pixels, want 1600", diff.Total)
	}
}

// Sub-pixel noise on text edges is what the tolerance exists for; a regression
// check that fires on it is a check people turn off.
func TestATolerableShiftIsNotAChange(t *testing.T) {
	before := solidPNG(t, 20, 20, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	after := solidPNG(t, 20, 20, color.RGBA{R: 104, G: 100, B: 100, A: 255})

	diff, err := ComparePNG(before, after, 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if diff.Changed != 0 {
		t.Errorf("a 4/255 shift was reported as %d changed pixels", diff.Changed)
	}
	if diff.MaxDelta != 4 {
		t.Errorf("MaxDelta = %d, want the 4 it actually was", diff.MaxDelta)
	}

	strict, err := ComparePNG(before, after, 1)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if strict.Changed != strict.Total {
		t.Errorf("a caller asking for tolerance 1 still got %d/%d", strict.Changed, strict.Total)
	}
}

func TestChangedRegionIsMeasuredNotJustDetected(t *testing.T) {
	before := solidPNG(t, 100, 100, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	after := patchedPNG(t, 100, 100,
		color.RGBA{R: 255, G: 255, B: 255, A: 255},
		color.RGBA{R: 0, G: 0, B: 0, A: 255}, 10, 100)

	diff, err := ComparePNG(before, after, 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if diff.Changed != 1000 {
		t.Errorf("changed = %d, want the 1000 pixels of the stripe", diff.Changed)
	}
	if diff.Fraction != 0.1 {
		t.Errorf("fraction = %v, want 0.1", diff.Fraction)
	}
	if diff.MaxDelta != 255 {
		t.Errorf("MaxDelta = %d, want 255", diff.MaxDelta)
	}
}

// A render that changed size did regress, and comparing overlapping corners of
// two different layouts would report a small number for a large change.
func TestASizeChangeIsAFullDifference(t *testing.T) {
	diff, err := ComparePNG(
		solidPNG(t, 40, 40, color.RGBA{A: 255}),
		solidPNG(t, 40, 41, color.RGBA{A: 255}), 0)
	if err != nil {
		t.Fatalf("ComparePNG: %v", err)
	}
	if !diff.SizeMismatch || diff.Fraction != 1 {
		t.Errorf("a size change reported as %+v", diff)
	}
}

func TestComparePNGRejectsWhatIsNotAnImage(t *testing.T) {
	if _, err := ComparePNG([]byte("not a png"), solidPNG(t, 2, 2, color.RGBA{A: 255}), 0); err == nil {
		t.Error("ComparePNG accepted a non-image reference")
	}
}
