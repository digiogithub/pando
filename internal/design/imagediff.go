package design

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"math"
)

// ImageDiff is the result of comparing two renders of the same artifact.
type ImageDiff struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// Changed is how many pixels differ by more than the tolerance, and Total
	// how many were compared.
	Changed int `json:"changed"`
	Total   int `json:"total"`
	// Fraction is Changed/Total, which is the number a regression check reads.
	Fraction float64 `json:"fraction"`
	// MaxDelta is the largest single-channel difference seen, so a caller can
	// tell "everything moved a shade" from "one region changed completely".
	MaxDelta int `json:"max_delta"`
	// SizeMismatch reports that the two images do not even have the same
	// dimensions, which is a regression on its own and makes Fraction 1.
	SizeMismatch bool `json:"size_mismatch"`
}

// defaultPixelTolerance absorbs the sub-pixel noise two runs of the same
// renderer produce on text edges without hiding an actual visual change.
const defaultPixelTolerance = 8

// ComparePNG measures how much two PNG renders differ. tolerance is the
// per-channel difference below which two pixels count as the same; pass 0 for
// the default.
//
// This is a perceptual-diff floor, not a perceptual model: it answers "did this
// render change" for a regression check, which is the question the fixture
// suite asks.
func ComparePNG(before, after []byte, tolerance int) (ImageDiff, error) {
	if tolerance <= 0 {
		tolerance = defaultPixelTolerance
	}
	first, _, err := image.Decode(bytes.NewReader(before))
	if err != nil {
		return ImageDiff{}, fmt.Errorf("design: decode the reference image: %w", err)
	}
	second, _, err := image.Decode(bytes.NewReader(after))
	if err != nil {
		return ImageDiff{}, fmt.Errorf("design: decode the compared image: %w", err)
	}

	a, b := first.Bounds(), second.Bounds()
	if a.Dx() != b.Dx() || a.Dy() != b.Dy() {
		return ImageDiff{
			Width: a.Dx(), Height: a.Dy(),
			Changed: a.Dx() * a.Dy(), Total: a.Dx() * a.Dy(),
			Fraction: 1, SizeMismatch: true,
		}, nil
	}

	diff := ImageDiff{Width: a.Dx(), Height: a.Dy(), Total: a.Dx() * a.Dy()}
	for y := 0; y < a.Dy(); y++ {
		for x := 0; x < a.Dx(); x++ {
			r1, g1, b1, a1 := first.At(a.Min.X+x, a.Min.Y+y).RGBA()
			r2, g2, b2, a2 := second.At(b.Min.X+x, b.Min.Y+y).RGBA()
			delta := maxInt(
				channelDelta(r1, r2), channelDelta(g1, g2),
				channelDelta(b1, b2), channelDelta(a1, a2),
			)
			if delta > diff.MaxDelta {
				diff.MaxDelta = delta
			}
			if delta > tolerance {
				diff.Changed++
			}
		}
	}
	if diff.Total > 0 {
		diff.Fraction = math.Round(float64(diff.Changed)/float64(diff.Total)*10000) / 10000
	}
	return diff, nil
}

// channelDelta converts the 16-bit channels image.Color reports back to the
// 8-bit scale a tolerance is written in.
func channelDelta(a, b uint32) int {
	d := int(a>>8) - int(b>>8)
	if d < 0 {
		return -d
	}
	return d
}

func maxInt(values ...int) int {
	out := 0
	for _, v := range values {
		if v > out {
			out = v
		}
	}
	return out
}
