package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/digiogithub/pando/internal/imageopt"
	"github.com/digiogithub/pando/internal/permission"
)

const (
	DesktopScreenshotToolName        = "desktop_screenshot"
	desktopScreenshotToolDescription = `Capture a screenshot of the screen, a window, or an element's bounds.

WHEN TO USE THIS TOOL:
- Use as a fallback when desktop_observe/desktop_find cannot describe what
  you need (custom-drawn UI, canvas/game content, visual verification) —
  prefer the semantic tools whenever the accessibility tree has the answer.
- Captures the whole screen/window, so it can show a browser window
  alongside native windows in one image — something browser_screenshot
  (page/element only) cannot do. Use browser_screenshot instead when you
  only need a picture of the page or one page element.

HOW TO USE:
- Provide target: "screen" (default), "window:<windowId>", or a qualified
  element ref (e.g. "@s8f3k2p9:e17") to crop to that element's bounds.
- This captures the user's whole screen or window, so it always asks for
  permission first, even though it does not change anything.
- Set grid:true to overlay a light coordinate grid with axis labels
  (unscaled real screen pixel coordinates) on the image. Use this when you
  intend to follow up with desktop_click_at: it makes it far easier to
  estimate accurate (x,y) values from the image than eyeballing a bare
  screenshot. Off by default to keep plain screenshots uncluttered.

RETURNS:
- The captured image, sent to the model as a real image block (normalized
  through the shared image pipeline).
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt, and
  PLATFORM_NOT_SUPPORTED when no screen-capture backend is available.`
)

type DesktopScreenshotParams struct {
	Target string `json:"target,omitempty"`
	Grid   bool   `json:"grid,omitempty"`
}

type DesktopScreenshotTool struct {
	permissions permission.Service
}

func NewDesktopScreenshotTool(permissions permission.Service) *DesktopScreenshotTool {
	return &DesktopScreenshotTool{permissions: permissions}
}

func (t *DesktopScreenshotTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopScreenshotToolName,
		Description: desktopScreenshotToolDescription,
		Parameters: map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": `"screen" (default), "window:<windowId>", or a qualified element ref to crop to its bounds`,
			},
			"grid": map[string]any{
				"type":        "boolean",
				"description": "Overlay a coordinate grid + axis labels (real screen pixels) to help estimate desktop_click_at coordinates. Default false.",
			},
		},
		Required: []string{},
	}
}

func (t *DesktopScreenshotTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopScreenshotParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	target := params.Target
	if target == "" {
		target = "screen"
	}

	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopScreenshotToolName, "screenshot",
		fmt.Sprintf("Capture a screenshot of %s", target), params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	data, mime, err := mgr.Screenshot(ctx, target, params.Grid)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	normalized, _, _, err := imageopt.Normalize(data, mime, imageopt.Options{
		AutoResize: true,
	})
	if err != nil {
		normalized = data
	}

	return ToolResponse{
		Type:    ToolResponseTypeImage,
		Content: base64.StdEncoding.EncodeToString(normalized),
	}, nil
}
