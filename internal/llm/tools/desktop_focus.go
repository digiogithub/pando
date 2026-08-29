package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopFocusToolName        = "desktop_focus"
	desktopFocusToolDescription = `Focus a desktop element or window by its qualified ref.

WHEN TO USE THIS TOOL:
- Use to bring a window/element to the foreground or move keyboard focus to
  it before typing or sending key presses.
- Use this (not a browser_* tool) to switch OS-level focus between the
  browser window and a native app/dialog — browser_* tools always act
  inside the browser's own DOM and have no notion of window focus.

HOW TO USE:
- Provide the qualified ref from desktop_observe/desktop_find. The element
  is re-resolved immediately before acting.
- Native Focus is tried first; a physical click at the element's bounds
  center is used as a fallback (when allowed by configuration).

RETURNS:
- {ok:true, method:"native"|"physical", notes:[...]}.
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt.`
)

type DesktopFocusParams struct {
	Ref string `json:"ref"`
}

type DesktopFocusTool struct {
	permissions permission.Service
}

func NewDesktopFocusTool(permissions permission.Service) *DesktopFocusTool {
	return &DesktopFocusTool{permissions: permissions}
}

func (t *DesktopFocusTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopFocusToolName,
		Description: desktopFocusToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref, e.g. "@s8f3k2p9:e17"`,
			},
		},
		Required: []string{"ref"},
	}
}

func (t *DesktopFocusTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopFocusParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Ref == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("ref is required")), nil
	}

	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopFocusToolName, "focus",
		fmt.Sprintf("Focus desktop element %s", params.Ref), params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result, err := mgr.Focus(ctx, core.ElementRef(params.Ref))
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"notes":  result.Notes,
	}), nil
}
