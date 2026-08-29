package tools

import (
	"context"

	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/uiauto"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// desktopManager returns the shared uiauto Manager (internal/uiauto),
// wrapping any non-DesktopError failure (e.g. "configuration not loaded")
// as a PLATFORM_NOT_SUPPORTED DesktopError so every desktop_* tool can
// render errors uniformly through desktopErrorResponse.
func desktopManager() (*uiauto.Manager, error) {
	mgr, err := uiauto.Shared()
	if err != nil {
		if _, ok := core.AsDesktopError(err); ok {
			return nil, err
		}
		return nil, core.NewPlatformNotSupportedError("desktop controller is not available: " + err.Error())
	}
	return mgr, nil
}

// desktopErrorResponse renders err as the structured
// {"ok":false,"error":{"code":...,"message":...,"suggestion":...}} payload
// the model expects. Any error that is not already a *core.DesktopError is
// wrapped as ACTION_FAILED so a bare Go error string is never surfaced.
func desktopErrorResponse(err error) ToolResponse {
	de, ok := core.AsDesktopError(err)
	if !ok {
		de = core.NewActionFailedError(err.Error())
	}
	resp := NewStructuredResponse(de.Payload())
	resp.IsError = true
	return resp
}

// desktopPermDeniedResponse renders a PERM_DENIED payload for a
// user-declined permission prompt on a mutating (or, for desktop_screenshot,
// sensitive read-only) desktop action.
func desktopPermDeniedResponse(action string) ToolResponse {
	de := core.NewPermDeniedError("the user declined the desktop " + action + " request")
	resp := NewStructuredResponse(de.Payload())
	resp.IsError = true
	return resp
}

// requestDesktopPermission asks permissions for a desktop action. On grant
// it returns ("", true); on denial it returns a ready-to-return
// PERM_DENIED ToolResponse and false.
func requestDesktopPermission(ctx context.Context, permissions permission.Service, toolName, action, description string, params any) (ToolResponse, bool) {
	sessionID, _ := GetContextValues(ctx)
	granted := permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolName:    toolName,
		Action:      action,
		Description: description,
		Params:      params,
	})
	if !granted {
		return desktopPermDeniedResponse(action), false
	}
	return ToolResponse{}, true
}

// desktopScopeParams is the common app/window targeting embedded by tools
// that operate on an app/window scope rather than a single element ref.
type desktopScopeParams struct {
	AppID    string `json:"app_id,omitempty"`
	WindowID string `json:"window_id,omitempty"`
}

func (p desktopScopeParams) toScope() core.Scope {
	return core.Scope{AppID: p.AppID, WindowID: p.WindowID}
}

var desktopScopeProperties = map[string]any{
	"app_id": map[string]any{
		"type":        "string",
		"description": "Application id/name to scope this to, as returned by desktop_apps (optional; omit to search across apps where supported)",
	},
	"window_id": map[string]any{
		"type":        "string",
		"description": "Window id to scope this to, as returned by desktop_apps (optional; defaults to the app's first/frontmost window)",
	},
}
