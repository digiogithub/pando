package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/rag"
)

func (s *Server) toolHybridSearchRemembrances(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Query           string   `json:"query"`
		Limit           int      `json:"limit"`
		ProjectIDs      []string `json:"project_ids"`
		IncludeKB       *bool    `json:"include_kb"`
		IncludeSessions *bool    `json:"include_sessions"`
		IncludeCode     *bool    `json:"include_code"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 10
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
	results, err := s.remembrances.HybridSearch(ctx, rag.HybridSearchOptions{
		Query:           req.Query,
		Limit:           req.Limit,
		ProjectIDs:      req.ProjectIDs,
		IncludeKB:       includeKB,
		IncludeSessions: includeSessions,
		IncludeCode:     includeCode,
	})
	if err != nil {
		return nil, fmt.Errorf("hybrid remembrances search error: %w", err)
	}
	return map[string]interface{}{
		"count":   len(results),
		"results": results,
	}, nil
}
