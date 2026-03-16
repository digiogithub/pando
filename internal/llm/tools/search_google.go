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
	GoogleSearchToolName        = "google_search"
	googleSearchAPIURL          = "https://www.googleapis.com/customsearch/v1"
	googleSearchToolDescription = `Search the web using Google Custom Search API.

WHEN TO USE THIS TOOL:
- Use when you need up-to-date information from the web
- Helpful for finding documentation, articles, or recent news
- Useful for researching topics that may not be in your training data

HOW TO USE:
- Provide a search query string
- Optionally limit results with 'num' (1-10)
- Use 'date_restrict' to limit to recent results (e.g. "d7" for past 7 days)
- Use 'site_search' to search within a specific site

REQUIREMENTS:
- Requires GOOGLE_API_KEY and GOOGLE_SEARCH_ENGINE_ID to be configured
- Set via env vars or in the internalTools section of config`
)

type googleSearchTool struct {
	client      *http.Client
	permissions permission.Service
}

func NewGoogleSearchTool(permissions permission.Service) BaseTool {
	return &googleSearchTool{
		client:      &http.Client{Timeout: 30 * time.Second},
		permissions: permissions,
	}
}

func (t *googleSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        GoogleSearchToolName,
		Description: googleSearchToolDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query string",
			},
			"num": map[string]any{
				"type":        "number",
				"description": "Number of results to return (1-10, default: 10)",
			},
			"start": map[string]any{
				"type":        "number",
				"description": "Index of the first result (1-91, default: 1)",
			},
			"safe": map[string]any{
				"type":        "string",
				"description": "SafeSearch setting",
				"enum":        []string{"active", "off"},
			},
			"lr": map[string]any{
				"type":        "string",
				"description": "Restrict to documents in a language (e.g. 'lang_en')",
			},
			"gl": map[string]any{
				"type":        "string",
				"description": "Geolocation country code (e.g. 'us', 'gb')",
			},
			"date_restrict": map[string]any{
				"type":        "string",
				"description": "Restrict results by date (e.g. 'd7' past 7 days, 'm1' past month)",
			},
			"site_search": map[string]any{
				"type":        "string",
				"description": "Limit results to a specific site (e.g. 'github.com')",
			},
			"search_type": map[string]any{
				"type":        "string",
				"description": "Type of search ('image' for image search)",
				"enum":        []string{"image"},
			},
		},
		Required: []string{"query"},
	}
}

func (t *googleSearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Query        string `json:"query"`
		Num          int    `json:"num"`
		Start        int    `json:"start"`
		Safe         string `json:"safe"`
		LR           string `json:"lr"`
		GL           string `json:"gl"`
		DateRestrict string `json:"date_restrict"`
		SiteSearch   string `json:"site_search"`
		SearchType   string `json:"search_type"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Query) == "" {
		return NewTextErrorResponse("query parameter is required"), nil
	}

	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.InternalTools.GoogleAPIKey) == "" {
		return NewTextErrorResponse("Google Search not configured: set GOOGLE_API_KEY (or PANDO_GOOGLE_API_KEY) environment variable"), nil
	}
	if strings.TrimSpace(cfg.InternalTools.GoogleSearchEngineID) == "" {
		return NewTextErrorResponse("Google Search not configured: set GOOGLE_SEARCH_ENGINE_ID (or PANDO_GOOGLE_SEARCH_ENGINE_ID) environment variable"), nil
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required")
	}

	p := t.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        config.WorkingDirectory(),
		ToolName:    GoogleSearchToolName,
		Action:      "web_search",
		Description: fmt.Sprintf("Search Google for: %s", params.Query),
		Params:      params,
	})
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	logging.Debug("google_search tool called", "query", params.Query)

	q := url.Values{}
	q.Set("key", cfg.InternalTools.GoogleAPIKey)
	q.Set("cx", cfg.InternalTools.GoogleSearchEngineID)
	q.Set("q", params.Query)
	if params.Num >= 1 && params.Num <= 10 {
		q.Set("num", fmt.Sprint(params.Num))
	}
	if params.Start >= 1 {
		q.Set("start", fmt.Sprint(params.Start))
	}
	if params.Safe == "active" || params.Safe == "off" {
		q.Set("safe", params.Safe)
	}
	if params.LR != "" {
		q.Set("lr", params.LR)
	}
	if params.GL != "" {
		q.Set("gl", params.GL)
	}
	if params.DateRestrict != "" {
		q.Set("dateRestrict", params.DateRestrict)
	}
	if params.SiteSearch != "" {
		q.Set("siteSearch", params.SiteSearch)
	}
	if params.SearchType == "image" {
		q.Set("searchType", params.SearchType)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", googleSearchAPIURL+"?"+q.Encode(), nil)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

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
		return NewTextErrorResponse(fmt.Sprintf("Google API error %d: %s", resp.StatusCode, string(body))), nil
	}

	var result struct {
		SearchInformation struct {
			FormattedTotalResults string `json:"formattedTotalResults"`
			FormattedSearchTime   string `json:"formattedSearchTime"`
		} `json:"searchInformation"`
		Items []struct {
			Title       string `json:"title"`
			Link        string `json:"link"`
			DisplayLink string `json:"displayLink"`
			Snippet     string `json:"snippet"`
		} `json:"items"`
		Spelling struct {
			CorrectedQuery string `json:"correctedQuery"`
		} `json:"spelling"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return NewTextErrorResponse("Failed to parse Google API response: " + err.Error()), nil
	}

	if len(result.Items) == 0 {
		return NewTextResponse(fmt.Sprintf("No results found for: %s", params.Query)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Google Search: %s\n", params.Query))
	if result.SearchInformation.FormattedTotalResults != "" {
		sb.WriteString(fmt.Sprintf("**Total Results:** %s | **Search Time:** %ss\n",
			result.SearchInformation.FormattedTotalResults,
			result.SearchInformation.FormattedSearchTime))
	}
	sb.WriteString("\n")

	for i, item := range result.Items {
		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, item.Title))
		sb.WriteString(fmt.Sprintf("**URL:** %s\n", item.Link))
		if item.DisplayLink != "" && item.DisplayLink != item.Link {
			sb.WriteString(fmt.Sprintf("**Site:** %s\n", item.DisplayLink))
		}
		if item.Snippet != "" {
			sb.WriteString(item.Snippet + "\n")
		}
		sb.WriteString("\n---\n\n")
	}

	if result.Spelling.CorrectedQuery != "" {
		sb.WriteString(fmt.Sprintf("> **Did you mean:** %s\n", result.Spelling.CorrectedQuery))
	}

	return NewTextResponse(sb.String()), nil
}
