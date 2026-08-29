package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopObserveToolName        = "desktop_observe"
	desktopObserveToolDescription = `Take a semantic snapshot of an application/window's accessibility tree.

WHEN TO USE THIS TOOL:
- Use after desktop_apps to inspect the UI of a specific app/window before
  acting on it.
- Prefer this over desktop_screenshot: it returns structured, addressable
  elements instead of pixels, and is far cheaper in tokens.
- Works on browser windows too (app_id "browser"), reading the page's
  accessibility tree (roles/names), NOT its DOM/CSS structure. Use this
  when you want to treat the page as one more application on the desktop
  (e.g. alongside a native dialog you are also driving). Use browser_get_content/
  browser_evaluate instead when you need the actual HTML, specific CSS
  selectors, or JavaScript-computed values — the accessibility tree
  collapses a lot of DOM structure the model may still need.

HOW TO USE:
- Provide app_id and/or window_id from desktop_apps.
- Optionally cap the traversal depth (default from config, typically 3);
  start shallow and re-observe deeper only the subtree you need.

RETURNS:
- A compact indented tree of the form
  "@<snapshotId>:<elemId> role \"name\" value=\"...\" [flags...]", plus the
  snapshot id. Every element ref is qualified to this snapshot and must be
  re-used exactly as printed (desktop_read/click/type/... take this ref).
- {ok:false, error:{code,message,suggestion}} on failure (e.g.
  PLATFORM_NOT_SUPPORTED, APP_NOT_FOUND, ELEMENT_NOT_FOUND, POLICY_DENIED).`
)

type DesktopObserveParams struct {
	desktopScopeParams
	Depth int `json:"depth,omitempty"`
}

type DesktopObserveTool struct{}

func NewDesktopObserveTool() *DesktopObserveTool { return &DesktopObserveTool{} }

func (t *DesktopObserveTool) Info() ToolInfo {
	props := map[string]any{
		"depth": map[string]any{
			"type":        "integer",
			"description": "Maximum tree depth to descend below the window root (optional; defaults to the configured DesktopDefaultDepth)",
		},
	}
	for k, v := range desktopScopeProperties {
		props[k] = v
	}
	return ToolInfo{
		Name:        DesktopObserveToolName,
		Description: desktopObserveToolDescription,
		Parameters:  props,
		Required:    []string{},
	}
}

func (t *DesktopObserveTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopObserveParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	snap, err := mgr.Observe(ctx, params.toScope(), params.Depth)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	tree := core.RenderTree(snap, core.RenderOptions{MaxNodes: mgr.MaxNodes()})
	return NewStructuredResponse(map[string]any{
		"ok":         true,
		"snapshotId": snap.ID,
		"tree":       tree,
	}), nil
}
