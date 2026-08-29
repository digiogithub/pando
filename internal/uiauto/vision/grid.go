package vision

import (
	"image"
	"image/color"
	"image/draw"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// DefaultGridStep is the pixel spacing DrawGrid uses between grid lines
// (in both axes) when GridOptions.Step is <= 0.
const DefaultGridStep = 100

// GridOptions controls DrawGrid.
type GridOptions struct {
	// Step is the pixel spacing between grid lines, in both axes. <= 0
	// uses DefaultGridStep.
	Step int
	// LineColor overrides the default grid line color (a semi-transparent
	// red is used when unset, i.e. the zero color.RGBA).
	LineColor color.RGBA
	// LabelColor overrides the default axis label color (yellow when
	// unset).
	LabelColor color.RGBA
}

// DrawGrid returns a copy of img with a light coordinate grid and
// pixel-coordinate axis labels overlaid every Step pixels, to help a
// vision-capable model translate what it sees into the (x,y) arguments
// desktop_click_at expects. img itself is never mutated. The overlay is
// drawn in real (unscaled) image coordinates, so labels always read the
// same pixel coordinates a subsequent desktop_click_at call should use --
// draw it before any resize step in the screenshot pipeline.
func DrawGrid(img image.Image, opts GridOptions) image.Image {
	step := opts.Step
	if step <= 0 {
		step = DefaultGridStep
	}
	lineColor := opts.LineColor
	if (lineColor == color.RGBA{}) {
		lineColor = color.RGBA{R: 255, G: 0, B: 0, A: 200}
	}
	labelColor := opts.LabelColor
	if (labelColor == color.RGBA{}) {
		labelColor = color.RGBA{R: 255, G: 255, B: 0, A: 255}
	}

	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)

	for x := b.Min.X; x < b.Max.X; x += step {
		drawVLine(out, x, b.Min.Y, b.Max.Y, lineColor)
		drawLabel(out, x+2, b.Min.Y+12, strconv.Itoa(x), labelColor)
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		drawHLine(out, y, b.Min.X, b.Max.X, lineColor)
		drawLabel(out, b.Min.X+2, y+12, strconv.Itoa(y), labelColor)
	}
	return out
}

func drawVLine(dst *image.RGBA, x, y0, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		dst.Set(x, y, c)
	}
}

func drawHLine(dst *image.RGBA, y, x0, x1 int, c color.RGBA) {
	for x := x0; x < x1; x++ {
		dst.Set(x, y, c)
	}
}

func drawLabel(dst draw.Image, x, y int, s string, c color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}
