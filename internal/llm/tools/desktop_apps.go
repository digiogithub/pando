package tools

import "context"

const (
	DesktopAppsToolName        = "desktop_apps"
	desktopAppsToolDescription = `List running desktop applications and their top-level windows.

WHEN TO USE THIS TOOL:
- Use this first, before desktop_observe/desktop_find, to discover which
  application/window to target — it is cheap and never walks an
  accessibility tree.
- Use it again after launching or closing an application to refresh ids.
- When a browser session is open (via browser_navigate), it is listed here
  too, as one app among the others (id "browser", one window per open
  page/tab) — this is how you find a page's window id to target with
  desktop_observe/desktop_find. It never opens a browser itself; if none is
  open, no "browser" app appears.

HOW TO USE:
- No parameters required.

RETURNS:
- {ok:true, apps:[{id,name,pid,windows}], windows:[{id,appId,title,bounds,focused}]}
  on success.
- {ok:false, error:{code,message,suggestion}} when the desktop controller is
  disabled or unavailable on this platform (code PLATFORM_NOT_SUPPORTED), or
  every application is blocked by the desktop policy.`
)

type DesktopAppsParams struct{}

type DesktopAppsTool struct{}

func NewDesktopAppsTool() *DesktopAppsTool { return &DesktopAppsTool{} }

func (t *DesktopAppsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        DesktopAppsToolName,
		Description: desktopAppsToolDescription,
		Parameters:  map[string]any{},
		Required:    []string{},
	}
}

func (t *DesktopAppsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopAppsParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	apps, err := mgr.Apps(ctx)
	if err != nil {
		return desktopErrorResponse(err), nil
	}
	windows, err := mgr.Windows(ctx, "")
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	return NewStructuredResponse(map[string]any{
		"ok":      true,
		"apps":    apps,
		"windows": windows,
	}), nil
}
