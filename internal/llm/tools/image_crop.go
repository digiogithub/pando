package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/imageopt"
)

const (
	ImageCropToolName        = "image_crop"
	imageCropToolDescription = `Return a rectangular region of an image file at native resolution.

WHEN TO USE THIS TOOL:
- Use the overview+zoom pattern: after seeing a downscaled image, crop a specific
  region (a table, a signature, a QR code, dense UI text) to read fine detail
  that was lost when the whole image was rescaled.
- Cheaper than re-sending the full high-resolution image.

HOW TO USE:
- Provide the image file path (absolute, or relative to the working directory).
- Provide x, y (top-left corner) and width, height in pixels. Omit width/height
  to crop to the image edge. Out-of-bounds values are clamped.

RETURNS:
- The cropped image, sent to the model as a real image block.`
)

type ImageCropParams struct {
	Path   string `json:"path"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type ImageCropTool struct{}

func NewImageCropTool() *ImageCropTool { return &ImageCropTool{} }

func (t *ImageCropTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ImageCropToolName,
		Description: imageCropToolDescription,
		Parameters: map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path to the image file (absolute or relative to the working directory)"},
			"x":      map[string]any{"type": "integer", "description": "Left edge of the crop region, in pixels"},
			"y":      map[string]any{"type": "integer", "description": "Top edge of the crop region, in pixels"},
			"width":  map[string]any{"type": "integer", "description": "Crop width in pixels; omit to crop to the right edge (optional)"},
			"height": map[string]any{"type": "integer", "description": "Crop height in pixels; omit to crop to the bottom edge (optional)"},
		},
		Required: []string{"path", "x", "y"},
	}
}

func (t *ImageCropTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params ImageCropParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Path == "" {
		return NewTextErrorResponse("path is required"), nil
	}

	path := params.Path
	if !filepath.IsAbs(path) {
		if cfg := config.Get(); cfg != nil && cfg.WorkingDir != "" {
			path = filepath.Join(cfg.WorkingDir, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return NewTextErrorResponse("unable to read image: " + err.Error()), nil
	}
	mime := imageopt.DetectMIME(data)
	if mime == "" {
		return NewTextErrorResponse("file is not a supported image (png/jpeg/webp/gif)"), nil
	}

	cropped, _, err := imageopt.Crop(data, mime, imageopt.Region{
		X: params.X, Y: params.Y, W: params.Width, H: params.Height,
	}, 90)
	if err != nil {
		return NewTextErrorResponse("crop failed: " + err.Error()), nil
	}
	return ToolResponse{
		Type:    ToolResponseTypeImage,
		Content: base64.StdEncoding.EncodeToString(cropped),
	}, nil
}
