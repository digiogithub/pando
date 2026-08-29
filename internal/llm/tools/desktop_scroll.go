package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopScrollToolName        = "desktop_scroll"
	desktopScrollToolDescription = `Scroll a desktop element by its qualified ref.

WHEN TO USE THIS TOOL:
- Use to reveal content that is not currently visible within a scrollable
  container, list, or document.
- On a browser page, prefer browser_scroll for a plain "scroll the page"
  action. Use this tool instead when scrolling an element addressed by a
  desktop_observe/desktop_find ref, e.g. a specific scrollable panel inside
  a mixed native+browser workflow.

HOW TO USE:
- Provide the qualified ref and a signed amount (backend-defined units;
  positive scrolls down/right, negative scrolls up/left). The element is
  re-resolved immediately before acting.
- Native Scroll is tried first; a physical scroll at the element's bounds
  center is used as a fallback (when allowed by configuration).

RETURNS:
- {ok:true, method:"native"|"physical", notes:[...]}.
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt.`
)

type DesktopScrollParams struct {
	Ref    string `json:"ref"`
	Amount int    `json:"amount"`
}

type DesktopScrollTool struct {
	permissions permission.Service
}

func NewDesktopScrollTool(permissions permission.Service) *DesktopScrollTool {
	return &DesktopScrollTool{permissions: permissions}
}

func (t *DesktopScrollTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopScrollToolName,
		Description: desktopScrollToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref, e.g. "@s8f3k2p9:e17"`,
			},
			"amount": map[string]any{
				"type":        "integer",
				"description": "Signed scroll amount, backend-defined units (positive = down/right, negative = up/left)",
			},
		},
		Required: []string{"ref", "amount"},
	}
}

func (t *DesktopScrollTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopScrollParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Ref == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("ref is required")), nil
	}

	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopScrollToolName, "scroll",
		fmt.Sprintf("Scroll desktop element %s by %d", params.Ref, params.Amount), params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result, err := mgr.Scroll(ctx, core.ElementRef(params.Ref), params.Amount)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"notes":  result.Notes,
	}), nil
}
