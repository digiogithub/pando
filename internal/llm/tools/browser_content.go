package tools

import (
	"context"
	"encoding/json"

	"github.com/chromedp/chromedp"
)

const (
	BrowserGetContentToolName        = "browser_get_content"
	browserGetContentToolDescription = `Get the HTML, text content, or title of the current page.

WHEN TO USE THIS TOOL:
- Use to extract text, HTML structure, or title from the current browser page
- Useful for reading page content after navigation, scraping, or analysis

HOW TO USE:
- Optionally specify format: "html", "text", or "title" (default "text")
- Optionally specify a CSS selector to scope the content (default "body")

RETURNS:
- Page content in the requested format`
)

type BrowserGetContentParams struct {
	Format   string `json:"format,omitempty"`
	Selector string `json:"selector,omitempty"`
}

type BrowserGetContentTool struct{}

func NewBrowserGetContentTool() *BrowserGetContentTool {
	return &BrowserGetContentTool{}
}

func (t *BrowserGetContentTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BrowserGetContentToolName,
		Description: browserGetContentToolDescription,
		Parameters: map[string]any{
			"format": map[string]any{
				"type":        "string",
				"description": `Output format: "html", "text", or "title" (default "text")`,
				"enum":        []string{"html", "text", "title"},
			},
			"selector": map[string]any{
				"type":        "string",
				"description": `CSS selector to scope content extraction (default "body", ignored for "title" format)`,
			},
		},
		Required: []string{},
	}
}

func (t *BrowserGetContentTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BrowserGetContentParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	format := params.Format
	if format == "" {
		format = "text"
	}

	selector := params.Selector
	if selector == "" {
		selector = "body"
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser not available: " + err.Error()), nil
	}
	defer cancel()

	var result string
	var action chromedp.Action

	switch format {
	case "html":
		action = chromedp.OuterHTML(selector, &result, chromedp.ByQuery)
	case "title":
		action = chromedp.Title(&result)
	default: // "text"
		action = chromedp.Text(selector, &result, chromedp.ByQuery)
	}

	if err := chromedp.Run(browserCtx, action); err != nil {
		return NewTextErrorResponse("failed to get page content: " + err.Error()), nil
	}

	resp := NewTextResponse(result)

	// Auto-cache large responses
	cache := GetSessionCache(ctx)
	return InterceptToolResponse(cache, call.ID, BrowserGetContentToolName, resp), nil
}
