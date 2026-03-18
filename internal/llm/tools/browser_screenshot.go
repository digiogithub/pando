package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const (
	BrowserScreenshotToolName        = "browser_screenshot"
	browserScreenshotToolDescription = `Take a screenshot of the current page or a specific element.

WHEN TO USE THIS TOOL:
- Use to visually inspect the current state of the browser page
- Useful for verifying navigation results, UI elements, or page layout

HOW TO USE:
- Optionally provide a CSS selector to capture a specific element
- Optionally set JPEG quality (1-100, default 80)
- Optionally enable full_page to capture the full scrollable page

RETURNS:
- Base64-encoded PNG image of the page or element`
)

type BrowserScreenshotParams struct {
	Selector string `json:"selector,omitempty"`
	Quality  int    `json:"quality,omitempty"`
	FullPage bool   `json:"full_page,omitempty"`
}

type BrowserScreenshotTool struct{}

func NewBrowserScreenshotTool() *BrowserScreenshotTool {
	return &BrowserScreenshotTool{}
}

func (t *BrowserScreenshotTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BrowserScreenshotToolName,
		Description: browserScreenshotToolDescription,
		Parameters: map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to capture; if empty, captures the full page (optional)",
			},
			"quality": map[string]any{
				"type":        "integer",
				"description": "JPEG quality 1-100 (default 80, only used when format is jpeg) (optional)",
			},
			"full_page": map[string]any{
				"type":        "boolean",
				"description": "Capture the full scrollable page instead of just the viewport (default false) (optional)",
			},
		},
		Required: []string{},
	}
}

func (t *BrowserScreenshotTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BrowserScreenshotParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	quality := params.Quality
	if quality <= 0 || quality > 100 {
		quality = 80
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser not available: " + err.Error()), nil
	}
	defer cancel()

	var buf []byte
	var action chromedp.Action

	switch {
	case params.FullPage:
		action = chromedp.FullScreenshot(&buf, quality)
	case params.Selector != "":
		action = chromedp.Screenshot(params.Selector, &buf, chromedp.NodeVisible)
	default:
		action = chromedp.FullScreenshot(&buf, quality)
	}

	if err := chromedp.Run(browserCtx, action); err != nil {
		return NewTextErrorResponse("screenshot failed: " + err.Error()), nil
	}

	encoded := base64.StdEncoding.EncodeToString(buf)
	return ToolResponse{
		Type:    ToolResponseTypeImage,
		Content: encoded,
	}, nil
}
