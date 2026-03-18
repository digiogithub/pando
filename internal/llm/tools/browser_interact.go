package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// ─── BrowserClickTool ────────────────────────────────────────────────────────

type BrowserClickTool struct{}

func NewBrowserClickTool() *BrowserClickTool { return &BrowserClickTool{} }

func (t *BrowserClickTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_click",
		Description: "Click on an element identified by a CSS selector.",
		Parameters: map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to click",
			},
			"wait_after_ms": map[string]any{
				"type":        "integer",
				"description": "Milliseconds to wait after click (default 300)",
			},
		},
		Required: []string{"selector"},
	}
}

type browserClickParams struct {
	Selector    string `json:"selector"`
	WaitAfterMs int    `json:"wait_after_ms"`
}

func (t *BrowserClickTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserClickParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Selector == "" {
		return NewTextErrorResponse("selector is required"), nil
	}
	if params.WaitAfterMs <= 0 {
		params.WaitAfterMs = 300
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}
	defer cancel()

	if err := chromedp.Run(browserCtx,
		chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
		chromedp.Click(params.Selector, chromedp.ByQuery),
		chromedp.Sleep(time.Duration(params.WaitAfterMs)*time.Millisecond),
	); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("click failed: %v", err)), nil
	}

	result, _ := json.Marshal(map[string]any{
		"clicked":  true,
		"selector": params.Selector,
	})
	return NewTextResponse(string(result)), nil
}

// ─── BrowserFillTool ─────────────────────────────────────────────────────────

type BrowserFillTool struct{}

func NewBrowserFillTool() *BrowserFillTool { return &BrowserFillTool{} }

func (t *BrowserFillTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_fill",
		Description: "Fill a form input or textarea with a value.",
		Parameters: map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the input or textarea element",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to fill into the element",
			},
			"clear_first": map[string]any{
				"type":        "boolean",
				"description": "Clear existing value before filling (default true)",
			},
		},
		Required: []string{"selector", "value"},
	}
}

type browserFillParams struct {
	Selector   string `json:"selector"`
	Value      string `json:"value"`
	ClearFirst *bool  `json:"clear_first"`
}

func (t *BrowserFillTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserFillParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.Selector == "" {
		return NewTextErrorResponse("selector is required"), nil
	}

	clearFirst := true
	if params.ClearFirst != nil {
		clearFirst = *params.ClearFirst
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}
	defer cancel()

	actions := []chromedp.Action{
		chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
	}
	if clearFirst {
		actions = append(actions, chromedp.Clear(params.Selector, chromedp.ByQuery))
	}
	actions = append(actions, chromedp.SendKeys(params.Selector, params.Value, chromedp.ByQuery))

	if err := chromedp.Run(browserCtx, actions...); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("fill failed: %v", err)), nil
	}

	result, _ := json.Marshal(map[string]any{
		"filled":   true,
		"selector": params.Selector,
	})
	return NewTextResponse(string(result)), nil
}

// ─── BrowserScrollTool ───────────────────────────────────────────────────────

type BrowserScrollTool struct{}

func NewBrowserScrollTool() *BrowserScrollTool { return &BrowserScrollTool{} }

func (t *BrowserScrollTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_scroll",
		Description: "Scroll the page by the specified pixel amounts.",
		Parameters: map[string]any{
			"x": map[string]any{
				"type":        "integer",
				"description": "Horizontal scroll pixels (default 0)",
			},
			"y": map[string]any{
				"type":        "integer",
				"description": "Vertical scroll pixels (default 500)",
			},
		},
		Required: []string{},
	}
}

type browserScrollParams struct {
	X *int `json:"x"`
	Y *int `json:"y"`
}

func (t *BrowserScrollTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserScrollParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	x := 0
	if params.X != nil {
		x = *params.X
	}
	y := 500
	if params.Y != nil {
		y = *params.Y
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}
	defer cancel()

	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate(fmt.Sprintf("window.scrollBy(%d, %d)", x, y), nil),
	); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("scroll failed: %v", err)), nil
	}

	result, _ := json.Marshal(map[string]any{
		"scrolled": true,
		"x":        x,
		"y":        y,
	})
	return NewTextResponse(string(result)), nil
}
