package tools

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const (
	BrowserNavigateToolName        = "browser_navigate"
	browserNavigateToolDescription = `Navigate the browser to a URL and wait for the page to load.

WHEN TO USE THIS TOOL:
- Use when you need to open a specific URL in the managed browser session
- Useful before taking screenshots, reading content, or interacting with a page

HOW TO USE:
- Provide the URL to navigate to
- Optionally specify a CSS selector to wait for after navigation
- Optionally override the default timeout in seconds

RETURNS:
- JSON object with url, title, and status fields`
)

type BrowserNavigateParams struct {
	URL     string `json:"url"`
	WaitFor string `json:"wait_for,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type BrowserNavigateTool struct{}

func NewBrowserNavigateTool() *BrowserNavigateTool {
	return &BrowserNavigateTool{}
}

func (t *BrowserNavigateTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BrowserNavigateToolName,
		Description: browserNavigateToolDescription,
		Parameters: map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to navigate to (must start with http:// or https://)",
			},
			"wait_for": map[string]any{
				"type":        "string",
				"description": "CSS selector to wait for after navigation (optional)",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Override default timeout in seconds (optional)",
			},
		},
		Required: []string{"url"},
	}
}

func (t *BrowserNavigateTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BrowserNavigateParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	if params.URL == "" {
		return NewTextErrorResponse("url parameter is required"), nil
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser not available: " + err.Error()), nil
	}
	defer cancel()

	var title string
	actions := []chromedp.Action{
		chromedp.Navigate(params.URL),
		chromedp.WaitVisible(`body`, chromedp.ByQuery),
		chromedp.Title(&title),
	}

	if params.WaitFor != "" {
		actions = append(actions, chromedp.WaitVisible(params.WaitFor, chromedp.ByQuery))
	}

	if err := chromedp.Run(browserCtx, actions...); err != nil {
		return NewTextErrorResponse("browser navigation failed: " + err.Error()), nil
	}

	result, _ := json.Marshal(map[string]string{
		"url":    params.URL,
		"title":  title,
		"status": "loaded",
	})
	return NewTextResponse(string(result)), nil
}
