package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopFindToolName        = "desktop_find"
	desktopFindToolDescription = `Resolve a selector to matching elements within an app/window.

WHEN TO USE THIS TOOL:
- Use to jump straight to specific elements instead of rendering a whole
  desktop_observe tree, especially in dense apps.
- Use to re-check whether an element still exists (e.g. after an action).
- On a browser window (app_id "browser") this matches by accessibility
  role/name (e.g. button[name="Sign in"]), NOT by CSS selector. If you
  already know the CSS selector (id, class, data-* attribute) or need to
  match by DOM structure, use browser_click/browser_fill/browser_evaluate
  instead — they are exact and do not depend on how the accessibility tree
  happens to expose a name.

HOW TO USE:
- Provide a selector, e.g. button[name="Save"], textfield[name^="Search"],
  group > button, or the bare-quoted shorthand "Save" (matches name on any
  role). Combine with :visible/:enabled/:focused pseudo-filters.
- Optionally scope with app_id/window_id and cap the number of matches.

RETURNS:
- The matching elements rendered one per line as
  "@<snapshotId>:<elemId> role \"name\" ..." plus the snapshot id; refs are
  reusable by desktop_read/click/type/etc.
- {ok:false, error:{code,message,suggestion}} on failure (e.g. INVALID_ARGS
  for a malformed selector, ELEMENT_NOT_FOUND, PLATFORM_NOT_SUPPORTED,
  POLICY_DENIED).`
)

type DesktopFindParams struct {
	desktopScopeParams
	Selector string `json:"selector"`
	Limit    int    `json:"limit,omitempty"`
}

type DesktopFindTool struct{}

func NewDesktopFindTool() *DesktopFindTool { return &DesktopFindTool{} }

func (t *DesktopFindTool) Info() ToolInfo {
	props := map[string]any{
		"selector": map[string]any{
			"type":        "string",
			"description": `Selector DSL, e.g. button[name="Save"], textfield[name^="Search"], group > button, or "Save" shorthand`,
		},
		"limit": map[string]any{
			"type":        "integer",
			"description": "Maximum number of matches to return (optional; 0 means backend default)",
		},
	}
	for k, v := range desktopScopeProperties {
		props[k] = v
	}
	return ToolInfo{
		Name:        DesktopFindToolName,
		Description: desktopFindToolDescription,
		Parameters:  props,
		Required:    []string{"selector"},
	}
}

func (t *DesktopFindTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopFindParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Selector == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("selector is required")), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	elements, snap, err := mgr.Find(ctx, params.toScope(), params.Selector, params.Limit)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	rendered := core.RenderElements(elements, core.RenderOptions{MaxNodes: mgr.MaxNodes()})
	return NewStructuredResponse(map[string]any{
		"ok":         true,
		"snapshotId": snap.ID,
		"count":      len(elements),
		"elements":   rendered,
	}), nil
}
