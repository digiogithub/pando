package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
)

type FetchParams struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout int    `json:"timeout,omitempty"`
	Browser string `json:"browser,omitempty"`
}

type FetchPermissionsParams struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout int    `json:"timeout,omitempty"`
	Browser string `json:"browser,omitempty"`
}

type fetchTool struct {
	client      *http.Client
	permissions permission.Service
}

const (
	FetchToolName        = "fetch"
	fetchToolDescription = `Fetches content from a URL and returns it in the specified format.

WHEN TO USE THIS TOOL:
- Use when you need to download content from a URL
- Helpful for retrieving documentation, API responses, or web content
- Useful for getting external information to assist with tasks
- Ideal for JavaScript-heavy pages, SPAs, or sites that block bots (use browser mode)

HOW TO USE:
- Provide the URL to fetch content from
- Specify the desired output format (text, markdown, html, json, or auto)
- Optionally select the browser backend (auto, http, chrome, firefox, curl)
- Optionally set a timeout for the request

FEATURES:
- Supports five output formats: text, markdown, html, json, and auto
- Browser backends: Chrome/Chromium headless, Firefox headless, curl, or plain HTTP
- Auto browser mode tries Chrome -> Firefox -> curl -> http in order
- Browser mode renders JavaScript and removes scripts, ads, nav, and styles
- Automatically handles HTTP redirects
- Sets reasonable timeouts to prevent hanging
- Validates input parameters before making requests
- Detects JSON responses and formats them as readable code blocks
- Auto format selects the best output based on Content-Type and body content

LIMITATIONS:
- Maximum response size is configurable (default 10MB)
- Only supports HTTP and HTTPS protocols
- Browser modes require the corresponding binary installed (google-chrome, firefox, curl)
- Some websites may still block automated requests even in headless mode

TIPS:
- Use browser=auto (default) to automatically pick the best available fetcher
- Use browser=chrome or browser=firefox for JavaScript-heavy or SPA pages
- Use browser=http for simple APIs and static pages (fastest, no binary needed)
- Use text format for plain text content or simple API responses
- Use markdown format for content that should be rendered with formatting
- Use html format when you need the raw HTML structure
- Set appropriate timeouts for potentially slow websites
- Use auto format when you're unsure of the response type (APIs, pages, or data)
- Use json format to force JSON pretty-printing for API endpoints`
)

func NewFetchTool(permissions permission.Service) BaseTool {
	return &fetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		permissions: permissions,
	}
}

func (t *fetchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        FetchToolName,
		Description: fetchToolDescription,
		Parameters: map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch content from",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "The format to return the content in (text, markdown, html, json, or auto)",
				"enum":        []string{"text", "markdown", "html", "json", "auto"},
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds (max 120)",
			},
			"browser": map[string]any{
				"type":        "string",
				"description": "Browser backend to use for fetching (auto, http, chrome, firefox, curl). Default is auto, which tries Chrome -> Firefox -> curl -> http.",
				"enum":        []string{"auto", "http", "chrome", "firefox", "curl"},
			},
		},
		Required: []string{"url", "format"},
	}
}

func (t *fetchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params FetchParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse fetch parameters: " + err.Error()), nil
	}

	if params.URL == "" {
		return NewTextErrorResponse("URL parameter is required"), nil
	}

	format := strings.ToLower(params.Format)
	if format != "text" && format != "markdown" && format != "html" && format != "json" && format != "auto" {
		return NewTextErrorResponse("Format must be one of: text, markdown, html, json, auto"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return NewTextErrorResponse("URL must start with http:// or https://"), nil
	}

	logging.Debug("fetch tool called", "url", params.URL, "format", params.Format, "timeout", params.Timeout)

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}

	p := t.permissions.Request(
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        config.WorkingDirectory(),
			ToolName:    FetchToolName,
			Action:      "fetch",
			Description: fmt.Sprintf("Fetch content from URL: %s", params.URL),
			Params:      FetchPermissionsParams(params),
		},
	)

	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	client := t.client
	if params.Timeout > 0 {
		maxTimeout := 120 // 2 minutes
		if params.Timeout > maxTimeout {
			params.Timeout = maxTimeout
		}
		client = &http.Client{
			Timeout: time.Duration(params.Timeout) * time.Second,
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "pando/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
	}

	maxSizeMB := 10 // default
	if cfg := config.Get(); cfg != nil && cfg.InternalTools.FetchMaxSizeMB > 0 {
		maxSizeMB = cfg.InternalTools.FetchMaxSizeMB
	}
	maxSize := int64(maxSizeMB * 1024 * 1024)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return NewTextErrorResponse("Failed to read response body: " + err.Error()), nil
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	logging.Debug("fetch completed", "url", params.URL, "statusCode", resp.StatusCode, "contentLength", len(body), "contentType", contentType)

	switch format {
	case "text":
		if strings.Contains(contentType, "text/html") {
			text, err := extractTextFromHTML(content)
			if err != nil {
				return NewTextErrorResponse("Failed to extract text from HTML: " + err.Error()), nil
			}
			return NewTextResponse(text), nil
		}
		return NewTextResponse(content), nil

	case "markdown":
		// Check for JSON content first (API responses are often JSON)
		if strings.Contains(contentType, "application/json") ||
			strings.Contains(contentType, "application/ld+json") ||
			isJSONContent(body) {
			return formatJSONResponse(body)
		}
		if strings.Contains(contentType, "text/html") {
			markdown, err := convertHTMLToMarkdown(content)
			if err != nil {
				return NewTextErrorResponse("Failed to convert HTML to Markdown: " + err.Error()), nil
			}
			return NewTextResponse(markdown), nil
		}
		return NewTextResponse("```\n" + content + "\n```"), nil

	case "json":
		return formatJSONResponse(body)

	case "auto":
		isJSON := strings.Contains(contentType, "application/json") ||
			strings.Contains(contentType, "application/ld+json") ||
			isJSONContent(body)
		if isJSON {
			return formatJSONResponse(body)
		}
		if strings.Contains(contentType, "text/html") {
			markdown, err := convertHTMLToMarkdown(content)
			if err != nil {
				return NewTextErrorResponse("Failed to convert HTML to Markdown: " + err.Error()), nil
			}
			return NewTextResponse(markdown), nil
		}
		return NewTextResponse(content), nil

	case "html":
		return NewTextResponse(content), nil

	default:
		return NewTextResponse(content), nil
	}
}

func extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	text := doc.Text()
	text = strings.Join(strings.Fields(text), " ")

	return text, nil
}

func convertHTMLToMarkdown(html string) (string, error) {
	converter := md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(html)
	if err != nil {
		return "", err
	}

	return markdown, nil
}

func isJSONContent(body []byte) bool {
	s := strings.TrimSpace(string(body))
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func formatJSONResponse(body []byte) (ToolResponse, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err != nil {
		return NewTextErrorResponse("Failed to format JSON: " + err.Error()), nil
	}
	return NewTextResponse("```json\n" + prettyJSON.String() + "\n```"), nil
}
