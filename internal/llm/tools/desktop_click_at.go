package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
)

const (
	DesktopClickAtToolName        = "desktop_click_at"
	desktopClickAtToolDescription = `Click a raw screen coordinate. This is the VISION FALLBACK, not a shortcut.

WHEN TO USE THIS TOOL:
- Only when desktop_observe/desktop_find genuinely cannot describe the
  target: custom-drawn UI (canvas apps, games), a remote desktop window
  rendered as a single opaque surface, or an app whose accessibility
  implementation is broken/missing. Always try desktop_find/desktop_observe
  first -- this tool acts blind, with no idea what is actually at (x,y).
- A quick signal before reaching for this: call desktop_find with a broad
  selector first. If it returns nothing and the backend genuinely lacks
  accessibility support for this app (PLATFORM_NOT_SUPPORTED/no matches
  despite the element being visible), coordinate clicking is warranted.

HOW TO USE:
- First call desktop_screenshot with grid:true to get an image annotated
  with real screen pixel coordinates, then estimate (x,y) for the target
  from that image.
- Provide x, y (required). The coordinates are validated against the
  actual captured display bounds before anything is sent to the OS.
- Requires the desktop policy to allow physical input
  (DesktopAllowPhysicalInput); always asks for permission first, since this
  is a blind coordinate action with no semantic target to describe.

RETURNS:
- {ok:true, method:"physical", source:"vision"} -- every result from this
  tool is marked source:"vision" so it is always distinguishable from a
  semantic (accessibility-tree) action.
- {ok:false, error:{code,message,suggestion}} on failure, including
  INVALID_ARGS for out-of-bounds coordinates, POLICY_DENIED when physical
  input is disabled, and PERM_DENIED when the user declines the prompt.`
)

type DesktopClickAtParams struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type DesktopClickAtTool struct {
	permissions permission.Service
}

func NewDesktopClickAtTool(permissions permission.Service) *DesktopClickAtTool {
	return &DesktopClickAtTool{permissions: permissions}
}

func (t *DesktopClickAtTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopClickAtToolName,
		Description: desktopClickAtToolDescription,
		Parameters: map[string]any{
			"x": map[string]any{
				"type":        "integer",
				"description": "X coordinate in real (unscaled) screen pixels, as read off a grid-annotated desktop_screenshot",
			},
			"y": map[string]any{
				"type":        "integer",
				"description": "Y coordinate in real (unscaled) screen pixels, as read off a grid-annotated desktop_screenshot",
			},
		},
		Required: []string{"x", "y"},
	}
}

func (t *DesktopClickAtTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopClickAtParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	description := fmt.Sprintf(
		"Blind coordinate click at screen position (%d,%d) -- no semantic element target, this is the vision fallback",
		params.X, params.Y)
	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopClickAtToolName, "click_at", description, params); !ok {
		return resp, nil
	}

	result, err := mgr.ClickAt(ctx, params.X, params.Y)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"source": "vision",
		"notes":  result.Notes,
	}), nil
}
