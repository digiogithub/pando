package imageopt

import (
	"bytes"
	"image"

	"github.com/disintegration/imaging"
)

// Region is a rectangular area of an image, in pixels.
type Region struct {
	X, Y, W, H int
}

// Tile is one piece of a tiled image, encoded and labeled by grid position.
type Tile struct {
	Index    int
	Row, Col int
	Region   Region
	Data     []byte
	MIMEType string
}

// Crop returns the given rectangular region of the source image, re-encoded in
// the source format (PNG stays PNG; other formats become JPEG). The region is
// clamped to the image bounds. Used for the overview+zoom reading pattern.
func Crop(data []byte, mime string, r Region, quality int) ([]byte, string, error) {
	if mime == "" {
		mime = DetectMIME(data)
	}
	img, err := decode(data, mime)
	if err != nil {
		return nil, "", err
	}
	if quality <= 0 {
		quality = 90
	}
	r = clampRegion(r, img.Bounds())
	rect := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	cropped := imaging.Crop(img, rect)
	return encode(cropped, mime, quality)
}

// TileImage splits the source image into a grid of tiles of at most tileSize on
// each side, with the given percentage of overlap between adjacent tiles. Dense,
// uniform content (scanned tables/documents) reads better tiled than downscaled.
func TileImage(data []byte, mime string, tileSize, overlapPct, quality int) ([]Tile, error) {
	if mime == "" {
		mime = DetectMIME(data)
	}
	img, err := decode(data, mime)
	if err != nil {
		return nil, err
	}
	if tileSize <= 0 {
		tileSize = 1024
	}
	if overlapPct < 0 || overlapPct > 90 {
		overlapPct = 10
	}
	if quality <= 0 {
		quality = 90
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	overlap := tileSize * overlapPct / 100
	step := tileSize - overlap
	if step <= 0 {
		step = tileSize
	}

	var tiles []Tile
	index, row := 0, 0
	for y := 0; y < h; y += step {
		col := 0
		th := minInt(tileSize, h-y)
		for x := 0; x < w; x += step {
			tw := minInt(tileSize, w-x)
			rect := image.Rect(b.Min.X+x, b.Min.Y+y, b.Min.X+x+tw, b.Min.Y+y+th)
			sub := imaging.Crop(img, rect)
			enc, encMIME, err := encode(sub, mime, quality)
			if err != nil {
				return nil, err
			}
			tiles = append(tiles, Tile{
				Index:    index,
				Row:      row,
				Col:      col,
				Region:   Region{X: x, Y: y, W: tw, H: th},
				Data:     enc,
				MIMEType: encMIME,
			})
			index++
			col++
			if x+tw >= w {
				break
			}
		}
		row++
		if y+th >= h {
			break
		}
	}
	return tiles, nil
}

// ImageDimensions decodes just the header to return the image's pixel size.
func ImageDimensions(data []byte) (w, h int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func clampRegion(r Region, b image.Rectangle) Region {
	maxW, maxH := b.Dx(), b.Dy()
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.W <= 0 || r.X+r.W > maxW {
		r.W = maxW - r.X
	}
	if r.H <= 0 || r.Y+r.H > maxH {
		r.H = maxH - r.Y
	}
	return r
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
