package tools

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

const (
	DesktopWaitToolName        = "desktop_wait"
	desktopWaitToolDescription = `Wait for a selector to satisfy a condition, instead of polling manually.

WHEN TO USE THIS TOOL:
- Use after an action that triggers an async UI change (a dialog opening, a
  spinner disappearing, a button becoming enabled) instead of guessing with
  repeated desktop_find calls.
- On a browser page, this waits on the accessibility tree, driven by real
  CDP DOM events where available. If you only need to wait for page
  navigation/load, browser_navigate's wait_for already covers that more
  directly.

HOW TO USE:
- Provide a selector and a condition: exists, notexists, visible, enabled,
  or focused.
- Optionally scope with app_id/window_id and override the timeout in
  seconds (default from config).

RETURNS:
- {ok:true, element:{...}} with the matched element (omitted for
  "notexists") once the condition is satisfied.
- {ok:false, error:{code,message,suggestion}} on TIMEOUT or other failure
  (INVALID_ARGS, PLATFORM_NOT_SUPPORTED, POLICY_DENIED).`
)

type DesktopWaitParams struct {
	desktopScopeParams
	Selector  string `json:"selector"`
	Condition string `json:"condition"`
	Timeout   int    `json:"timeout,omitempty"`
}

type DesktopWaitTool struct{}

func NewDesktopWaitTool() *DesktopWaitTool { return &DesktopWaitTool{} }

func (t *DesktopWaitTool) Info() ToolInfo {
	props := map[string]any{
		"selector": map[string]any{
			"type":        "string",
			"description": `Selector DSL, e.g. button[name="OK"]:enabled`,
		},
		"condition": map[string]any{
			"type":        "string",
			"description": "One of: exists, notexists, visible, enabled, focused",
			"enum":        []string{"exists", "notexists", "visible", "enabled", "focused"},
		},
		"timeout": map[string]any{
			"type":        "integer",
			"description": "Timeout in seconds (optional; defaults to the configured DesktopActionTimeout)",
		},
	}
	for k, v := range desktopScopeProperties {
		props[k] = v
	}
	return ToolInfo{
		Name:        DesktopWaitToolName,
		Description: desktopWaitToolDescription,
		Parameters:  props,
		Required:    []string{"selector", "condition"},
	}
}

func (t *DesktopWaitTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesktopWaitParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Selector == "" {
		return desktopErrorResponse(core.NewInvalidArgsError("selector is required")), nil
	}
	cond := core.Condition(params.Condition)
	switch cond {
	case core.ConditionExists, core.ConditionNotExists, core.ConditionVisible, core.ConditionEnabled, core.ConditionFocused:
	default:
		return desktopErrorResponse(core.NewInvalidArgsError("condition must be one of: exists, notexists, visible, enabled, focused")), nil
	}

	mgr, err := desktopManager()
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	var timeout time.Duration
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
	}

	el, err := mgr.Wait(ctx, params.toScope(), params.Selector, cond, timeout)
	if err != nil {
		return desktopErrorResponse(err), nil
	}

	result := map[string]any{"ok": true}
	if el != nil {
		result["element"] = el
	}
	return NewStructuredResponse(result), nil
}
