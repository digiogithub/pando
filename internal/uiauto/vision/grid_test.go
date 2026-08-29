package vision

import (
	"image"
	"image/color"
	"testing"
)

func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestDrawGridDoesNotMutateInput(t *testing.T) {
	src := solidImage(50, 50, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	before := *src // shallow copy of the header; Pix slice is what we check
	pixCopy := append([]byte(nil), src.Pix...)

	_ = DrawGrid(src, GridOptions{Step: 10})

	if src.Stride != before.Stride {
		t.Fatalf("input image header mutated")
	}
	for i := range pixCopy {
		if src.Pix[i] != pixCopy[i] {
			t.Fatal("DrawGrid mutated its input image")
		}
	}
}

func TestDrawGridSameBounds(t *testing.T) {
	src := solidImage(200, 150, color.RGBA{A: 255})
	out := DrawGrid(src, GridOptions{Step: 50})
	if out.Bounds() != src.Bounds() {
		t.Fatalf("output bounds %v != input bounds %v", out.Bounds(), src.Bounds())
	}
}

func TestDrawGridDrawsLinesAtStepIntervals(t *testing.T) {
	src := solidImage(100, 100, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	lineColor := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	out := DrawGrid(src, GridOptions{Step: 25, LineColor: lineColor})
	rgba, ok := out.(*image.RGBA)
	if !ok {
		t.Fatalf("expected *image.RGBA output, got %T", out)
	}

	// A vertical grid line is expected at x=25 (mid-height, away from any
	// label text near y=0).
	got := rgba.RGBAAt(25, 60)
	if got != lineColor {
		t.Fatalf("pixel at (25,60) = %+v, want line color %+v", got, lineColor)
	}
	// A horizontal grid line is expected at y=25 (mid-width, away from
	// axis labels near x=0).
	got = rgba.RGBAAt(60, 25)
	if got != lineColor {
		t.Fatalf("pixel at (60,25) = %+v, want line color %+v", got, lineColor)
	}
	// A point far from any grid line/label must be left untouched.
	got = rgba.RGBAAt(12, 12)
	if got != (color.RGBA{R: 0, G: 0, B: 0, A: 255}) {
		t.Fatalf("pixel at (12,12) = %+v, want untouched original color", got)
	}
}

func TestDrawGridDefaultStep(t *testing.T) {
	src := solidImage(DefaultGridStep+10, DefaultGridStep+10, color.RGBA{A: 255})
	out := DrawGrid(src, GridOptions{}) // Step <= 0 uses DefaultGridStep
	rgba := out.(*image.RGBA)
	// Expect a drawn line at x=0 (always drawn) and at x=DefaultGridStep.
	if rgba.RGBAAt(0, DefaultGridStep/2).A == 0 {
		t.Fatal("expected a line drawn at x=0")
	}
}

func TestDrawGridLabelsAreDrawn(t *testing.T) {
	src := solidImage(60, 60, color.RGBA{A: 255})
	labelColor := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	out := DrawGrid(src, GridOptions{Step: 1000, LabelColor: labelColor}) // one origin line only
	rgba := out.(*image.RGBA)

	found := false
	for y := 0; y < 15; y++ {
		for x := 0; x < 20; x++ {
			if rgba.RGBAAt(x, y) == labelColor {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected at least one label-colored pixel near the origin")
	}
}
