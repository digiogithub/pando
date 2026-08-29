package tools

import (
	"context"

	"github.com/chromedp/chromedp"
)

const (
	BrowserEvaluateToolName        = "browser_evaluate"
	browserEvaluateToolDescription = `Execute JavaScript in the browser and return the result as a structured response.

WHEN TO USE THIS TOOL:
- Use to run arbitrary JavaScript expressions in the current page context
- Useful for reading DOM values, triggering actions, or extracting dynamic data
- No desktop_* equivalent exists for this: the accessibility tree
  (desktop_observe/desktop_find) exposes roles/names/values, never raw JS
  execution or DOM internals. Use this tool for anything that needs actual
  JavaScript, not desktop_read (which only reads the accessibility-tree
  view of an element).

HOW TO USE:
- Provide a JavaScript expression to evaluate
- The result is serialized through Pando's structured formatter

RETURNS:
- Structured result of the JavaScript expression (serialized as TOON when possible, with TOML/JSON fallback)`
)

type BrowserEvaluateParams struct {
	Expression string `json:"expression"`
}

type BrowserEvaluateTool struct{}

func NewBrowserEvaluateTool() *BrowserEvaluateTool {
	return &BrowserEvaluateTool{}
}

func (t *BrowserEvaluateTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BrowserEvaluateToolName,
		Description: browserEvaluateToolDescription,
		Parameters: map[string]any{
			"expression": map[string]any{
				"type":        "string",
				"description": "JavaScript expression to execute in the browser context",
			},
		},
		Required: []string{"expression"},
	}
}

func (t *BrowserEvaluateTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BrowserEvaluateParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	if params.Expression == "" {
		return NewTextErrorResponse("expression parameter is required"), nil
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser not available: " + err.Error()), nil
	}
	defer cancel()

	var result interface{}
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(params.Expression, &result)); err != nil {
		return NewTextErrorResponse("JavaScript evaluation failed: " + err.Error()), nil
	}

	return NewStructuredResponse(result), nil
}
