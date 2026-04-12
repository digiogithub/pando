package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/rag"
)

const hybridSearchRemembrancesToolName = "hybrid_search_remembrances"

type HybridSearchRemembrancesTool struct {
	service *rag.RemembrancesService
}

func NewHybridSearchRemembrancesTool(service *rag.RemembrancesService) BaseTool {
	return &HybridSearchRemembrancesTool{service: service}
}

func (t *HybridSearchRemembrancesTool) Info() ToolInfo {
	return ToolInfo{
		Name:        hybridSearchRemembrancesToolName,
		Description: "Searches remembrances using hybrid search across the knowledge base, indexed conversation sessions, and indexed code projects.",
		Parameters: map[string]any{
			"query":            map[string]any{"type": "string", "description": "Natural language query to search across remembrances."},
			"limit":            map[string]any{"type": "integer", "description": "Maximum number of results to return (default: 10, max: 50)."},
			"project_ids":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional code project IDs to include in code search."},
			"include_kb":       map[string]any{"type": "boolean", "description": "Include KB results (default: true)."},
			"include_sessions": map[string]any{"type": "boolean", "description": "Include indexed conversation sessions stored as events (default: true)."},
			"include_code":     map[string]any{"type": "boolean", "description": "Include indexed code projects (default: true when project_ids provided)."},
		},
		Required: []string{"query"},
	}
}

func (t *HybridSearchRemembrancesTool) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	var req struct {
		Query           string   `json:"query"`
		Limit           int      `json:"limit"`
		ProjectIDs      []string `json:"project_ids"`
		IncludeKB       *bool    `json:"include_kb"`
		IncludeSessions *bool    `json:"include_sessions"`
		IncludeCode     *bool    `json:"include_code"`
	}
	if err := json.Unmarshal([]byte(params.Input), &req); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("invalid parameters: %v", err)), nil
	}
	if req.Query == "" {
		return NewTextErrorResponse("query is required"), nil
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 50 {
		req.Limit = 50
	}

	includeKB := true
	if req.IncludeKB != nil {
		includeKB = *req.IncludeKB
	}
	includeSessions := true
	if req.IncludeSessions != nil {
		includeSessions = *req.IncludeSessions
	}
	includeCode := len(req.ProjectIDs) > 0
	if req.IncludeCode != nil {
		includeCode = *req.IncludeCode
	}

	results, err := t.service.HybridSearch(ctx, rag.HybridSearchOptions{
		Query:           req.Query,
		Limit:           req.Limit,
		ProjectIDs:      req.ProjectIDs,
		IncludeKB:       includeKB,
		IncludeSessions: includeSessions,
		IncludeCode:     includeCode,
	})
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("hybrid remembrances search error: %v", err)), nil
	}
	out, err := json.MarshalIndent(map[string]any{"count": len(results), "results": results}, "", "  ")
	if err != nil {
		return NewTextErrorResponse("failed to marshal results"), nil
	}
	return NewTextResponse(string(out)), nil
}
