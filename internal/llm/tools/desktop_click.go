package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopClickToolName        = "desktop_click"
	desktopClickToolDescription = `Click a desktop element by its qualified ref.

WHEN TO USE THIS TOOL:
- Use to activate a button, link, menu item, checkbox, or any element with
  an invoke/press action.
- On a browser page, prefer this over browser_click when the click is part
  of a broader desktop workflow that also touches native windows/dialogs
  (e.g. a browser file-picker or a native save dialog triggered by the
  page) — the ref came from desktop_find/desktop_observe and is addressed
  by accessibility role/name, not a CSS selector. For pure, scripted page
  automation where you already have a CSS selector, use browser_click
  instead; it is more direct and does not require an accessibility ref.

HOW TO USE:
- Provide the qualified ref from desktop_observe/desktop_find. The element
  is re-resolved immediately before acting.
- The accessibility action (Invoke) is tried first; if it is unsupported or
  fails, a physical mouse click at the element's bounds center is used as a
  fallback (when allowed by configuration).

RETURNS:
- {ok:true, method:"native"|"physical", notes:[...]} describing how the
  click was carried out.
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt.`
)

type DesktopClickParams struct {
	Ref string `json:"ref"`
}

type DesktopClickTool struct {
	permissions permission.Service
}

func NewDesktopClickTool(permissions permission.Service) *DesktopClickTool {
	return &DesktopClickTool{permissions: permissions}
}

func (t *DesktopClickTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopClickToolName,
		Description: desktopClickToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref, e.g. "@s8f3k2p9:e17"`,
			},
		},
		Required: []string{"ref"},
	}
}

func (t *DesktopClickTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopClickParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Ref == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("ref is required")), nil
	}

	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopClickToolName, "click",
		fmt.Sprintf("Click desktop element %s", params.Ref), params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result, err := mgr.Click(ctx, core.ElementRef(params.Ref))
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"notes":  result.Notes,
	}), nil
}
