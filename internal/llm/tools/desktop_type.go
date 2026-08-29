package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopTypeToolName        = "desktop_type"
	desktopTypeToolDescription = `Enter text into a desktop element by its qualified ref.

WHEN TO USE THIS TOOL:
- Use to fill a text field, search box, or any editable element.
- On a browser page, prefer browser_fill for pure scripted form-filling by
  CSS selector. Use this tool instead when the field is part of a mixed
  desktop workflow (e.g. typing into a native OS dialog the page triggered,
  or when you are already navigating the page via desktop_observe/
  desktop_find refs rather than CSS selectors).

HOW TO USE:
- Provide the qualified ref and the text to enter. The element is
  re-resolved immediately before acting.
- Native SetValue is tried first, then a native Type action, then a
  physical focus+keyboard-simulation fallback (when allowed by
  configuration).

RETURNS:
- {ok:true, method:"native"|"physical", notes:[...]} describing how the
  text was entered.
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt.`
)

type DesktopTypeParams struct {
	Ref  string `json:"ref"`
	Text string `json:"text"`
}

type DesktopTypeTool struct {
	permissions permission.Service
}

func NewDesktopTypeTool(permissions permission.Service) *DesktopTypeTool {
	return &DesktopTypeTool{permissions: permissions}
}

func (t *DesktopTypeTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopTypeToolName,
		Description: desktopTypeToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref, e.g. "@s8f3k2p9:e17"`,
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to enter into the element",
			},
		},
		Required: []string{"ref", "text"},
	}
}

func (t *DesktopTypeTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopTypeParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Ref == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("ref is required")), nil
	}

	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopTypeToolName, "type",
		fmt.Sprintf("Type text into desktop element %s", params.Ref), params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result, err := mgr.Type(ctx, core.ElementRef(params.Ref), params.Text)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"notes":  result.Notes,
	}), nil
}
