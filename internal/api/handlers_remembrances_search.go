// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/rag/treesitter"
)

// ---- KB search ----

// kbSearchRequest mirrors the kb_search_documents tool's parameters
// (internal/llm/tools/remembrances_kb.go), so the same request shape works
// whether the caller speaks MCP or REST.
type kbSearchRequest struct {
	Query           string   `json:"query"`
	Limit           int      `json:"limit"`
	Tags            []string `json:"tags"`
	SortByDate      bool     `json:"sort_by_date"`
	ExcludeOutdated *bool    `json:"exclude_outdated"`
	Scope           string   `json:"scope"`
	PathPrefix      string   `json:"path_prefix"`
}

// kbSearchResultItem mirrors the resultItem the kb_search_documents tool
// emits, field for field, so a REST caller sees an identical shape.
type kbSearchResultItem struct {
	FilePath     string                 `json:"file_path"`
	ChunkContent string                 `json:"chunk_content"`
	Score        float64                `json:"score"`
	Rank         int                    `json:"rank"`
	Tags         []string               `json:"tags,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	// Links and Backlinks are omitted for documents outside the wiki graph, so
	// a knowledge base that does not use the syntax reads exactly as before.
	Links     int `json:"links,omitempty"`
	Backlinks int `json:"backlinks,omitempty"`
}

// kbSearchRelatedView mirrors the tools package's kbRelatedView (a scored
// neighbour in the document graph), duplicated here since that type is
// unexported and local to internal/llm/tools.
type kbSearchRelatedView struct {
	FilePath string   `json:"file_path"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons,omitempty"`
}

func kbSearchRelatedViews(related []kb.RelatedDocument) []kbSearchRelatedView {
	if len(related) == 0 {
		return nil
	}
	out := make([]kbSearchRelatedView, len(related))
	for i, r := range related {
		out[i] = kbSearchRelatedView{FilePath: r.FilePath, Score: r.Score, Reasons: r.Reasons}
	}
	return out
}

// handleKBSearch runs a hybrid (vector + FTS) search over the knowledge base.
// POST /api/v1/remembrances/kb/search
// Body: {"query": "...", "limit": 5, "tags": [...], "sort_by_date": false,
//
//	"exclude_outdated": true, "scope": "", "path_prefix": ""}
func (s *Server) handleKBSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app == nil || s.app.Remembrances == nil || s.app.Remembrances.KB == nil {
		writeJSON(w, http.StatusOK, map[string]any{"count": 0, "results": []kbSearchResultItem{}})
		return
	}

	var req kbSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	// Same default/ceiling as kb_search_documents (limit default 5, max 20).
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Limit > 20 {
		req.Limit = 20
	}
	excludeOutdated := true
	if req.ExcludeOutdated != nil {
		excludeOutdated = *req.ExcludeOutdated
	}

	store := s.app.Remembrances.KB
	ctx := r.Context()
	opts := kb.SearchOptions{
		Tags:            req.Tags,
		SortByDate:      req.SortByDate,
		ExcludeOutdated: excludeOutdated,
		Scope:           req.Scope,
		PathPrefix:      req.PathPrefix,
	}
	results, stats, err := store.SearchDocumentsWithOptionsAndStats(ctx, req.Query, req.Limit, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kb search error: "+err.Error())
		return
	}
	if len(results) == 0 {
		empty := map[string]any{"count": 0, "results": []kbSearchResultItem{}}
		if stats.SkippedForDimensionMismatch > 0 {
			empty["warning"] = tools.StaleEmbeddingWarning(stats.SkippedForDimensionMismatch)
		}
		writeJSON(w, http.StatusOK, empty)
		return
	}

	items := make([]kbSearchResultItem, len(results))
	paths := make([]string, len(results))
	for i, res := range results {
		items[i] = kbSearchResultItem{
			FilePath:     res.Document.FilePath,
			ChunkContent: res.ChunkContent,
			Score:        res.Score,
			Rank:         res.Rank,
			Tags:         res.Document.Tags,
			CreatedAt:    res.Document.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:    res.Document.UpdatedAt.UTC().Format(time.RFC3339),
			Metadata:     res.Document.Metadata,
		}
		paths[i] = res.Document.FilePath
	}

	counts, err := store.LinkCountsFor(ctx, paths)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kb link counts error: "+err.Error())
		return
	}
	for i := range items {
		if c, ok := counts[items[i].FilePath]; ok {
			items[i].Links, items[i].Backlinks = c.Outgoing, c.Backlinks
		}
	}

	out := map[string]any{
		"count":   len(items),
		"results": items,
	}
	if stats.SkippedForDimensionMismatch > 0 {
		out["warning"] = tools.StaleEmbeddingWarning(stats.SkippedForDimensionMismatch)
	}

	// Same as kb_search_documents: hand over the top result's graph
	// neighbours only when it actually participates in the graph.
	if _, connected := counts[items[0].FilePath]; connected {
		related, err := store.RelatedDocuments(ctx, items[0].FilePath, 3)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "kb related error: "+err.Error())
			return
		}
		if v := kbSearchRelatedViews(related); v != nil {
			out["related_to_top_result"] = v
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// ---- Code search ----

// codeSearchRequest mirrors the code_hybrid_search tool's parameters
// (internal/llm/tools/remembrances_code.go).
type codeSearchRequest struct {
	ProjectID   string   `json:"project_id"`
	Query       string   `json:"query"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
	Languages   []string `json:"languages"`
	SymbolTypes []string `json:"symbol_types"`
	MinScore    float64  `json:"min_score"`
	IncludeDocs bool     `json:"include_docs"`
	Debug       bool     `json:"debug"`
}

// handleCodeSearch runs a hybrid (vector + FTS) search over an indexed code
// project. POST /api/v1/remembrances/code/search
// Body: {"project_id": "...", "query": "...", "limit": 20, "offset": 0,
//
//	"languages": [...], "symbol_types": [...], "min_score": 0,
//	"include_docs": false, "debug": false}
func (s *Server) handleCodeSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app == nil || s.app.Remembrances == nil || s.app.Remembrances.Code == nil {
		writeJSON(w, http.StatusOK, codeSearchEmptyResponse())
		return
	}

	var req codeSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	// Same default/ceiling as code_hybrid_search (limit default 20, max 50).
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	var langs []code.Language
	for _, l := range req.Languages {
		langs = append(langs, treesitter.Language(l))
	}
	var symTypes []code.SymbolType
	for _, st := range req.SymbolTypes {
		symTypes = append(symTypes, treesitter.SymbolType(st))
	}

	// Over-fetch enough candidates to cover offset+limit even after doc
	// filtering and the relevance cutoff trim the result set — mirrors
	// code_hybrid_search's headroom so REST and MCP page the same way.
	want := req.Offset + req.Limit
	fetchLimit := want
	if !req.IncludeDocs || req.MinScore > 0 {
		fetchLimit = want * 3
	}
	if fetchLimit > 150 {
		fetchLimit = 150
	}
	if fetchLimit < want {
		fetchLimit = want
	}

	results, err := s.app.Remembrances.Code.HybridSearch(r.Context(), req.ProjectID, req.Query, fetchLimit, langs, symTypes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hybrid search error: "+err.Error())
		return
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusOK, codeSearchEmptyResponse())
		return
	}

	// Rank/filter through the exact helper code_hybrid_search uses, so REST
	// and MCP order results identically for the same arguments.
	ranked := tools.RankAndFilterHybrid(results, req.MinScore, req.IncludeDocs, req.Debug)
	page, total, hasMore, nextOffset := paginateHybridResults(ranked, req.Offset, req.Limit)

	writeJSON(w, http.StatusOK, map[string]any{
		"count":       len(page),
		"total":       total,
		"offset":      req.Offset,
		"limit":       req.Limit,
		"has_more":    hasMore,
		"next_offset": nextOffset,
		"results":     page,
	})
}

func codeSearchEmptyResponse() map[string]any {
	return map[string]any{
		"count":    0,
		"total":    0,
		"offset":   0,
		"limit":    0,
		"has_more": false,
		"results":  []tools.HybridResultItem{},
	}
}

// paginateHybridResults slices the already-ranked full set into one page,
// matching the tool package's paginate() semantics (unexported there).
func paginateHybridResults(items []tools.HybridResultItem, offset, limit int) (page []tools.HybridResultItem, total int, hasMore bool, nextOffset int) {
	total = len(items)
	if offset >= total {
		return nil, total, false, 0
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page = items[offset:end]
	if end < total {
		hasMore = true
		nextOffset = end
	}
	return page, total, hasMore, nextOffset
}
