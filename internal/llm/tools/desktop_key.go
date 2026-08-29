package tools

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopKeyToolName        = "desktop_key"
	desktopKeyToolDescription = `Send a key press or chord, targeted at an element or globally.

WHEN TO USE THIS TOOL:
- Use for keyboard shortcuts (e.g. "Ctrl+A", "Enter", "Escape", "Tab") that
  desktop_type/desktop_click cannot express.
- Use this (not a browser_* tool) for OS-level shortcuts (Alt+Tab, Cmd+Q) or
  for a global key press with no browser_* equivalent; use browser_evaluate
  to dispatch a synthetic DOM keyboard event scoped to the page instead.

HOW TO USE:
- Provide the key/chord (e.g. "Enter", "Ctrl+S", "Alt+F4").
- Optionally provide a ref to target a specific element; omit it to send
  the key globally (requires physical input to be allowed).

RETURNS:
- {ok:true, method:"native"|"physical", notes:[...]}.
- {ok:false, error:{code,message,suggestion}} on failure, including
  PERM_DENIED when the user declines the permission prompt, and
  PLATFORM_NOT_SUPPORTED for a global key press when physical input is
  unavailable/disallowed.`
)

type DesktopKeyParams struct {
	Ref string `json:"ref,omitempty"`
	Key string `json:"key"`
}

type DesktopKeyTool struct {
	permissions permission.Service
}

func NewDesktopKeyTool(permissions permission.Service) *DesktopKeyTool {
	return &DesktopKeyTool{permissions: permissions}
}

func (t *DesktopKeyTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopKeyToolName,
		Description: desktopKeyToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref to target, e.g. "@s8f3k2p9:e17" (optional; omit to send the key globally)`,
			},
			"key": map[string]any{
				"type":        "string",
				"description": `Key or chord, e.g. "Enter", "Escape", "Ctrl+S", "Alt+F4"`,
			},
		},
		Required: []string{"key"},
	}
}

func (t *DesktopKeyTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopKeyParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Key == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("key is required")), nil
	}

	desc := fmt.Sprintf("Send key %q globally", params.Key)
	if params.Ref != "" {
		desc = fmt.Sprintf("Send key %q to desktop element %s", params.Key, params.Ref)
	}
	if resp, ok := requestDesktopPermission(ctx, t.permissions, DesktopKeyToolName, "key", desc, params); !ok {
		return resp, nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result, err := mgr.Key(ctx, core.ElementRef(params.Ref), params.Key)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":     true,
		"method": result.Method,
		"notes":  result.Notes,
	}), nil
}
