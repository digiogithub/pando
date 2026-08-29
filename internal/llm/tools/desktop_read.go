package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopReadToolName        = "desktop_read"
	desktopReadToolDescription = `Read the full details of a single element by its qualified ref.

WHEN TO USE THIS TOOL:
- Use after desktop_observe/desktop_find to inspect one element's full
  name/value/description/bounds/actions without re-rendering the whole tree.
- On a browser page, this reads the accessibility-tree view of an element
  (role/name/value), not its raw HTML/attributes. Use browser_get_content
  or browser_evaluate instead when you need the actual DOM/HTML or a
  JavaScript-computed value.

HOW TO USE:
- Provide the qualified ref, e.g. "@s8f3k2p9:e17", exactly as printed by
  desktop_observe/desktop_find.

RETURNS:
- The full Element (role, name, value, description, bounds, enabled,
  visible, focused, actions, native platform data).
- {ok:false, error:{code,message,suggestion}} on failure (STALE_REF,
  SNAPSHOT_NOT_FOUND, ELEMENT_NOT_FOUND, POLICY_DENIED, INVALID_ARGS).`
)

type DesktopReadParams struct {
	Ref string `json:"ref"`
}

type DesktopReadTool struct{}

func NewDesktopReadTool() *DesktopReadTool { return &DesktopReadTool{} }

func (t *DesktopReadTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopReadToolName,
		Description: desktopReadToolDescription,
		Parameters: map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": `Qualified element ref, e.g. "@s8f3k2p9:e17", as returned by desktop_observe/desktop_find`,
			},
		},
		Required: []string{"ref"},
	}
}

func (t *DesktopReadTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopReadParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Ref == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("ref is required")), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	el, err := mgr.Read(ctx, core.ElementRef(params.Ref))
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":      true,
		"element": el,
	}), nil
}
