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

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
)

const (
	ExaSearchToolName        = "exa_search"
	exaSearchAPIURL          = "https://api.exa.ai/search"
	exaSearchToolDescription = `Search the web using Exa AI Search API.

WHEN TO USE THIS TOOL:
- Use when you need high-quality, semantically relevant web search results
- Ideal for research, finding recent information, or locating specific content
- Supports neural/semantic search and content extraction with highlights

HOW TO USE:
- Provide a search query
- Use 'num_results' to control number of results (1-100, default: 10)
- Use 'type' to select search mode: auto (default), neural, fast, instant
- Use 'include_highlights' to get relevant text snippets from each result

REQUIREMENTS:
- Requires EXA_API_KEY to be configured
- Set via env var or in the internalTools section of config`
)

type exaSearchTool struct {
	client      *http.Client
	permissions permission.Service
}

func NewExaSearchTool(permissions permission.Service) BaseTool {
	return &exaSearchTool{
		client:      &http.Client{Timeout: 30 * time.Second},
		permissions: permissions,
	}
}

func (t *exaSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ExaSearchToolName,
		Description: exaSearchToolDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query string",
			},
			"num_results": map[string]any{
				"type":        "number",
				"description": "Number of results to return (1-100, default: 10)",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Search type: auto (default), neural, fast, instant",
				"enum":        []string{"auto", "neural", "fast", "instant"},
			},
			"include_highlights": map[string]any{
				"type":        "boolean",
				"description": "Include relevant text highlights from each result (default: true)",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Filter by content category: company, research_paper, news, tweet, personal_site, github",
				"enum":        []string{"company", "research_paper", "news", "tweet", "personal_site", "github"},
			},
		},
		Required: []string{"query"},
	}
}

func (t *exaSearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Query             string `json:"query"`
		NumResults        int    `json:"num_results"`
		Type              string `json:"type"`
		IncludeHighlights *bool  `json:"include_highlights"`
		Category          string `json:"category"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Query) == "" {
		return NewTextErrorResponse("query parameter is required"), nil
	}

	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.InternalTools.ExaAPIKey) == "" {
		return NewTextErrorResponse("Exa Search not configured: set EXA_API_KEY (or PANDO_EXA_API_KEY) environment variable"), nil
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required")
	}

	p := t.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        config.WorkingDirectory(),
		ToolName:    ExaSearchToolName,
		Action:      "web_search",
		Description: fmt.Sprintf("Search Exa for: %s", params.Query),
		Params:      params,
	})
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	logging.Debug("exa_search tool called", "query", params.Query)

	// Build request body
	numResults := params.NumResults
	if numResults < 1 || numResults > 100 {
		numResults = 10
	}

	searchType := params.Type
	if searchType == "" {
		searchType = "auto"
	}

	includeHighlights := true
	if params.IncludeHighlights != nil {
		includeHighlights = *params.IncludeHighlights
	}

	reqBody := map[string]any{
		"query":      params.Query,
		"numResults": numResults,
		"type":       searchType,
	}
	if includeHighlights {
		reqBody["contents"] = map[string]any{
			"highlights": map[string]any{
				"maxCharacters": 4000,
			},
		}
	}
	if params.Category != "" {
		reqBody["category"] = params.Category
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", exaSearchAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("x-api-key", cfg.InternalTools.ExaAPIKey)
	req.Header.Set("Content-Type", "application/json")
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
		return NewTextErrorResponse(fmt.Sprintf("Exa Search API error %d: %s", resp.StatusCode, string(body))), nil
	}

	var result struct {
		Results []struct {
			Title         string   `json:"title"`
			URL           string   `json:"url"`
			PublishedDate string   `json:"publishedDate"`
			Author        string   `json:"author"`
			Highlights    []string `json:"highlights"`
			Text          string   `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return NewTextErrorResponse("Failed to parse Exa Search response: " + err.Error()), nil
	}

	if len(result.Results) == 0 {
		return NewTextResponse(fmt.Sprintf("No results found for: %s", params.Query)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Exa Search: %s\n\n", params.Query))

	for i, item := range result.Results {
		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, item.Title))
		sb.WriteString(fmt.Sprintf("**URL:** %s\n", item.URL))
		if item.PublishedDate != "" {
			sb.WriteString(fmt.Sprintf("**Published:** %s\n", item.PublishedDate))
		}
		if item.Author != "" {
			sb.WriteString(fmt.Sprintf("**Author:** %s\n", item.Author))
		}
		if len(item.Highlights) > 0 {
			sb.WriteString("\n")
			for _, h := range item.Highlights {
				sb.WriteString("> " + h + "\n")
			}
		} else if item.Text != "" {
			maxLen := 500
			text := item.Text
			if len(text) > maxLen {
				text = text[:maxLen] + "..."
			}
			sb.WriteString("\n" + text + "\n")
		}
		sb.WriteString("\n---\n\n")
	}

	return NewTextResponse(sb.String()), nil
}
