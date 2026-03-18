package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ─── BrowserConsoleLogsTool ──────────────────────────────────────────────────

type BrowserConsoleLogsTool struct{}

func NewBrowserConsoleLogsTool() *BrowserConsoleLogsTool { return &BrowserConsoleLogsTool{} }

func (t *BrowserConsoleLogsTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_console_logs",
		Description: "Get captured JavaScript console messages from the current page.",
		Parameters: map[string]any{
			"clear": map[string]any{
				"type":        "boolean",
				"description": "Clear the log buffer after reading (default false)",
			},
			"level": map[string]any{
				"type":        "string",
				"description": `Filter by log level: "all", "error", "warn", "log", "info" (default "all")`,
				"enum":        []string{"all", "error", "warn", "log", "info"},
			},
		},
		Required: []string{},
	}
}

type browserConsoleLogsParams struct {
	Clear bool   `json:"clear"`
	Level string `json:"level"`
}

func (t *BrowserConsoleLogsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserConsoleLogsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	level := strings.ToLower(params.Level)
	if level == "" {
		level = "all"
	}

	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return NewTextErrorResponse("no session ID in context"), nil
	}

	sess, err := GetOrCreateBrowserSession(sessionID)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}

	sess.mu.Lock()
	entries := make([]BrowserConsoleEntry, len(sess.consoleLogs))
	copy(entries, sess.consoleLogs)
	if params.Clear {
		sess.consoleLogs = sess.consoleLogs[:0]
	}
	sess.mu.Unlock()

	if level != "all" {
		filtered := entries[:0]
		for _, e := range entries {
			if strings.EqualFold(e.Level, level) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	result, _ := json.Marshal(entries)
	return NewTextResponse(string(result)), nil
}

// ─── BrowserNetworkTool ──────────────────────────────────────────────────────

type BrowserNetworkTool struct{}

func NewBrowserNetworkTool() *BrowserNetworkTool { return &BrowserNetworkTool{} }

func (t *BrowserNetworkTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_network",
		Description: "Get captured network requests made by the current page.",
		Parameters: map[string]any{
			"clear": map[string]any{
				"type":        "boolean",
				"description": "Clear the network log after reading (default false)",
			},
			"filter_url": map[string]any{
				"type":        "string",
				"description": "Substring to filter URLs (empty = all)",
			},
		},
		Required: []string{},
	}
}

type browserNetworkParams struct {
	Clear     bool   `json:"clear"`
	FilterURL string `json:"filter_url"`
}

func (t *BrowserNetworkTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserNetworkParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return NewTextErrorResponse("no session ID in context"), nil
	}

	sess, err := GetOrCreateBrowserSession(sessionID)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}

	sess.mu.Lock()
	entries := make([]BrowserNetworkEntry, len(sess.networkLog))
	copy(entries, sess.networkLog)
	if params.Clear {
		sess.networkLog = sess.networkLog[:0]
	}
	sess.mu.Unlock()

	if params.FilterURL != "" {
		filtered := entries[:0]
		for _, e := range entries {
			if strings.Contains(e.URL, params.FilterURL) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	result, _ := json.Marshal(entries)
	return NewTextResponse(string(result)), nil
}

// ─── BrowserPDFTool ──────────────────────────────────────────────────────────

type BrowserPDFTool struct{}

func NewBrowserPDFTool() *BrowserPDFTool { return &BrowserPDFTool{} }

func (t *BrowserPDFTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "browser_pdf",
		Description: "Generate a PDF of the current page.",
		Parameters: map[string]any{
			"landscape": map[string]any{
				"type":        "boolean",
				"description": "Use landscape orientation (default false)",
			},
			"print_background": map[string]any{
				"type":        "boolean",
				"description": "Print background graphics (default true)",
			},
			"output_path": map[string]any{
				"type":        "string",
				"description": "Save PDF to this file path; if empty returns base64-encoded PDF",
			},
		},
		Required: []string{},
	}
}

type browserPDFParams struct {
	Landscape       bool   `json:"landscape"`
	PrintBackground *bool  `json:"print_background"`
	OutputPath      string `json:"output_path"`
}

func (t *BrowserPDFTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params browserPDFParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}

	printBackground := true
	if params.PrintBackground != nil {
		printBackground = *params.PrintBackground
	}

	browserCtx, cancel, err := getBrowserCtxWithTimeout(ctx)
	if err != nil {
		return NewTextErrorResponse("browser session error: " + err.Error()), nil
	}
	defer cancel()

	var pdfBuf []byte
	if err := chromedp.Run(browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			var runErr error
			pdfBuf, _, runErr = page.PrintToPDF().
				WithLandscape(params.Landscape).
				WithPrintBackground(printBackground).
				Do(ctx)
			return runErr
		}),
	); err != nil {
		return NewTextErrorResponse("pdf failed: " + err.Error()), nil
	}

	if params.OutputPath != "" {
		if err := os.WriteFile(params.OutputPath, pdfBuf, 0o644); err != nil {
			return NewTextErrorResponse(fmt.Sprintf("failed to write PDF to %s: %v", params.OutputPath, err)), nil
		}
		result, _ := json.Marshal(map[string]any{
			"saved": true,
			"path":  params.OutputPath,
		})
		return NewTextResponse(string(result)), nil
	}

	return NewTextResponse(base64.StdEncoding.EncodeToString(pdfBuf)), nil
}
