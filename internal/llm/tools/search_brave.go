package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
)

const (
	BraveSearchToolName        = "brave_search"
	braveSearchAPIURL          = "https://api.search.brave.com/res/v1/web/search"
	braveSearchToolDescription = `Search the web using Brave Search API.

WHEN TO USE THIS TOOL:
- Use when you need up-to-date web search results
- Good alternative to Google Search with privacy-focused results
- Includes community discussions (Reddit, forums) in results

HOW TO USE:
- Provide a search query
- Use 'freshness' to filter by date (pd=past day, pw=past week, pm=past month, py=past year)
- Use 'count' to control number of results (1-20)
- Use 'country' for region-specific results

REQUIREMENTS:
- Requires BRAVE_API_KEY to be configured
- Set via env var or in the internalTools section of config`
)

type braveSearchTool struct {
	client      *http.Client
	permissions permission.Service
}

func NewBraveSearchTool(permissions permission.Service) BaseTool {
	return &braveSearchTool{
		client:      &http.Client{Timeout: 30 * time.Second},
		permissions: permissions,
	}
}

func (t *braveSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BraveSearchToolName,
		Description: braveSearchToolDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query string",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Number of results to return (1-20, default: 10)",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "Pagination offset (default: 0)",
			},
			"country": map[string]any{
				"type":        "string",
				"description": "Country code for results (e.g. 'US', 'GB')",
			},
			"search_lang": map[string]any{
				"type":        "string",
				"description": "Language code for search (e.g. 'en', 'es')",
			},
			"safesearch": map[string]any{
				"type":        "string",
				"description": "SafeSearch level",
				"enum":        []string{"strict", "moderate", "off"},
			},
			"freshness": map[string]any{
				"type":        "string",
				"description": "Time filter: pd (past day), pw (past week), pm (past month), py (past year)",
				"enum":        []string{"pd", "pw", "pm", "py"},
			},
		},
		Required: []string{"query"},
	}
}

func (t *braveSearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Query      string `json:"query"`
		Count      int    `json:"count"`
		Offset     int    `json:"offset"`
		Country    string `json:"country"`
		SearchLang string `json:"search_lang"`
		SafeSearch string `json:"safesearch"`
		Freshness  string `json:"freshness"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Query) == "" {
		return NewTextErrorResponse("query parameter is required"), nil
	}

	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.InternalTools.BraveAPIKey) == "" {
		return NewTextErrorResponse("Brave Search not configured: set BRAVE_API_KEY (or PANDO_BRAVE_API_KEY) environment variable"), nil
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required")
	}

	p := t.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        config.WorkingDirectory(),
		ToolName:    BraveSearchToolName,
		Action:      "web_search",
		Description: fmt.Sprintf("Search Brave for: %s", params.Query),
		Params:      params,
	})
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	logging.Debug("brave_search tool called", "query", params.Query)

	q := url.Values{}
	q.Set("q", params.Query)
	if params.Count >= 1 && params.Count <= 20 {
		q.Set("count", fmt.Sprint(params.Count))
	}
	if params.Offset > 0 {
		q.Set("offset", fmt.Sprint(params.Offset))
	}
	if params.Country != "" {
		q.Set("country", params.Country)
	}
	if params.SearchLang != "" {
		q.Set("search_lang", params.SearchLang)
	}
	switch params.SafeSearch {
	case "strict", "moderate", "off":
		q.Set("safesearch", params.SafeSearch)
	}
	switch params.Freshness {
	case "pd", "pw", "pm", "py":
		q.Set("freshness", params.Freshness)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", braveSearchAPIURL+"?"+q.Encode(), nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", cfg.InternalTools.BraveAPIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return NewTextErrorResponse("Failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Brave Search API error %d: %s", resp.StatusCode, string(body))), nil
	}

	var result struct {
		Query struct {
			Original string `json:"original"`
		} `json:"query"`
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				PageAge     string `json:"page_age"`
			} `json:"results"`
		} `json:"web"`
		Discussions struct {
			Results []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"results"`
		} `json:"discussions"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return NewTextErrorResponse("Failed to parse Brave Search response: " + err.Error()), nil
	}

	if len(result.Web.Results) == 0 {
		return NewTextResponse(fmt.Sprintf("No results found for: %s", params.Query)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Brave Search: %s\n\n", params.Query))

	for i, item := range result.Web.Results {
		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, item.Title))
		sb.WriteString(fmt.Sprintf("**URL:** %s\n", item.URL))
		if item.PageAge != "" {
			sb.WriteString(fmt.Sprintf("**Age:** %s\n", item.PageAge))
		}
		if item.Description != "" {
			sb.WriteString(item.Description + "\n")
		}
		sb.WriteString("\n---\n\n")
	}

	if len(result.Discussions.Results) > 0 {
		sb.WriteString("### Discussions:\n")
		limit := 3
		if len(result.Discussions.Results) < limit {
			limit = len(result.Discussions.Results)
		}
		for _, d := range result.Discussions.Results[:limit] {
			sb.WriteString(fmt.Sprintf("- [%s](%s)\n", d.Title, d.URL))
		}
	}

	return NewTextResponse(sb.String()), nil
}
