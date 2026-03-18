package tools

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const (
	BrowserEvaluateToolName        = "browser_evaluate"
	browserEvaluateToolDescription = `Execute JavaScript in the browser and return the result as JSON.

WHEN TO USE THIS TOOL:
- Use to run arbitrary JavaScript expressions in the current page context
- Useful for reading DOM values, triggering actions, or extracting dynamic data

HOW TO USE:
- Provide a JavaScript expression to evaluate
- The result is serialized as JSON

RETURNS:
- JSON-encoded result of the JavaScript expression`
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
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
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

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return NewTextErrorResponse("failed to serialize result: " + err.Error()), nil
	}

	return NewTextResponse(string(resultJSON)), nil
}
