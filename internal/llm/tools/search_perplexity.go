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
	PerplexitySearchToolName        = "perplexity_search"
	perplexitySearchAPIURL          = "https://api.perplexity.ai/chat/completions"
	perplexitySearchToolDescription = `Search and get AI-synthesized answers using Perplexity AI.

WHEN TO USE THIS TOOL:
- Use when you need synthesized answers with citations, not just links
- Best for complex questions that benefit from an AI-generated summary
- Useful for current events, technical explanations, or research questions

HOW TO USE:
- Provide your question or search query
- Choose a model: sonar-pro (balanced), sonar-reasoning (step-by-step), sonar-deep-research (thorough)
- Use 'search_recency_filter' to prioritize recent results
- Set 'return_citations' to true (default) to include sources

REQUIREMENTS:
- Requires PERPLEXITY_API_KEY to be configured
- Set via env var or in the internalTools section of config`
)

type perplexitySearchTool struct {
	client      *http.Client
	permissions permission.Service
}

func NewPerplexitySearchTool(permissions permission.Service) BaseTool {
	return &perplexitySearchTool{
		client:      &http.Client{Timeout: 60 * time.Second},
		permissions: permissions,
	}
}

func (t *perplexitySearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        PerplexitySearchToolName,
		Description: perplexitySearchToolDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query or question",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Perplexity model to use (default: sonar-pro)",
				"enum":        []string{"sonar-pro", "sonar-reasoning", "sonar-deep-research"},
			},
			"system_message": map[string]any{
				"type":        "string",
				"description": "Optional system message to set context for the search",
			},
			"max_tokens": map[string]any{
				"type":        "number",
				"description": "Maximum tokens in the response (1-4096, default: 1000)",
			},
			"temperature": map[string]any{
				"type":        "number",
				"description": "Response randomness (0.0-2.0, default: 0.2)",
			},
			"search_recency_filter": map[string]any{
				"type":        "string",
				"description": "Filter results by recency",
				"enum":        []string{"month", "week", "day", "hour"},
			},
			"return_citations": map[string]any{
				"type":        "boolean",
				"description": "Include source citations (default: true)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *perplexitySearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Query               string  `json:"query"`
		Model               string  `json:"model"`
		SystemMessage       string  `json:"system_message"`
		MaxTokens           int     `json:"max_tokens"`
		Temperature         float64 `json:"temperature"`
		SearchRecencyFilter string  `json:"search_recency_filter"`
		ReturnCitations     *bool   `json:"return_citations"`
	}
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Query) == "" {
		return NewTextErrorResponse("query parameter is required"), nil
	}

	cfg := config.Get()
	if cfg == nil || strings.TrimSpace(cfg.InternalTools.PerplexityAPIKey) == "" {
		return NewTextErrorResponse("Perplexity Search not configured: set PERPLEXITY_API_KEY (or PANDO_PERPLEXITY_API_KEY) environment variable"), nil
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required")
	}

	p := t.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        config.WorkingDirectory(),
		ToolName:    PerplexitySearchToolName,
		Action:      "web_search",
		Description: fmt.Sprintf("Perplexity search: %s", params.Query),
		Params:      params,
	})
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	// Set defaults
	model := "sonar-pro"
	if params.Model == "sonar-reasoning" || params.Model == "sonar-deep-research" {
		model = params.Model
	}
	maxTokens := 1000
	if params.MaxTokens >= 1 && params.MaxTokens <= 4096 {
		maxTokens = params.MaxTokens
	}
	temperature := 0.2
	if params.Temperature >= 0.0 && params.Temperature <= 2.0 && params.Temperature != 0.0 {
		temperature = params.Temperature
	}
	recency := "month"
	switch params.SearchRecencyFilter {
	case "week", "day", "hour":
		recency = params.SearchRecencyFilter
	}
	returnCitations := true
	if params.ReturnCitations != nil {
		returnCitations = *params.ReturnCitations
	}

	// Build messages
	messages := []map[string]string{}
	if params.SystemMessage != "" {
		messages = append(messages, map[string]string{"role": "system", "content": params.SystemMessage})
	}
	messages = append(messages, map[string]string{"role": "user", "content": params.Query})

	reqBody := map[string]any{
		"model":                    model,
		"messages":                 messages,
		"max_tokens":               maxTokens,
		"temperature":              temperature,
		"return_citations":         returnCitations,
		"return_images":            false,
		"return_related_questions": false,
		"search_recency_filter":    recency,
	}

	logging.Debug("perplexity_search tool called", "query", params.Query, "model", model)

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", perplexitySearchAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return ToolResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.InternalTools.PerplexityAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return NewTextErrorResponse("Failed to read response: " + err.Error()), nil
	}

	if resp.StatusCode != http.StatusOK {
		return NewTextErrorResponse(fmt.Sprintf("Perplexity API error %d: %s", resp.StatusCode, string(respBody))), nil
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Citations     []string `json:"citations"`
		SearchResults []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Date    string `json:"date"`
		} `json:"search_results"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return NewTextErrorResponse("Failed to parse Perplexity response: " + err.Error()), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Perplexity Search: %s\n", params.Query))
	sb.WriteString(fmt.Sprintf("**Model:** %s | **Tokens used:** %d\n\n", model, result.Usage.TotalTokens))

	if len(result.Choices) > 0 {
		sb.WriteString(result.Choices[0].Message.Content)
		sb.WriteString("\n\n")
	}

	if returnCitations && len(result.Citations) > 0 {
		sb.WriteString("### Sources:\n")
		for i, c := range result.Citations {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
		}
		sb.WriteString("\n")
	}

	if len(result.SearchResults) > 0 {
		sb.WriteString("### Related Results:\n")
		limit := 5
		if len(result.SearchResults) < limit {
			limit = len(result.SearchResults)
		}
		for i, r := range result.SearchResults[:limit] {
			sb.WriteString(fmt.Sprintf("%d. **%s** — %s\n", i+1, r.Title, r.URL))
			if r.Snippet != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
			}
			if r.Date != "" {
				sb.WriteString(fmt.Sprintf("   *(dated: %s)*\n", r.Date))
			}
		}
	}

	return NewTextResponse(sb.String()), nil
}
