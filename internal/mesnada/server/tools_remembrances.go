package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/events"
)

// toolKBAddDocument adds a document to the knowledge base.
func (s *Server) toolKBAddDocument(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Path     string                 `json:"path"`
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// Check if document exists to decide add vs update
	existing, err := s.remembrances.KB.GetDocument(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("kb get error: %w", err)
	}

	if existing != nil {
		if err := s.remembrances.KB.UpdateDocument(ctx, req.Path, req.Content, req.Metadata); err != nil {
			return nil, fmt.Errorf("kb update error: %w", err)
		}
		if err := s.remembrances.KB.WriteDocumentToFilesystem(req.Path, req.Content); err != nil {
			return nil, fmt.Errorf("kb filesystem mirror error: %w", err)
		}
		return map[string]interface{}{
			"path":   req.Path,
			"status": "updated",
		}, nil
	}

	if err := s.remembrances.KB.AddDocument(ctx, req.Path, req.Content, req.Metadata); err != nil {
		return nil, fmt.Errorf("kb add error: %w", err)
	}
	if err := s.remembrances.KB.WriteDocumentToFilesystem(req.Path, req.Content); err != nil {
		return nil, fmt.Errorf("kb filesystem mirror error: %w", err)
	}
	return map[string]interface{}{
		"path":   req.Path,
		"status": "added",
	}, nil
}

// toolKBImportPath imports markdown files from a path recursively into the KB.
func (s *Server) toolKBImportPath(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Path          string `json:"path"`
		DeleteMissing *bool  `json:"delete_missing"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	deleteMissing := true
	if req.DeleteMissing != nil {
		deleteMissing = *req.DeleteMissing
	}

	stats, err := s.remembrances.KB.SyncDirectoryWithStats(ctx, req.Path, deleteMissing)
	if err != nil {
		return nil, fmt.Errorf("kb import error: %w", err)
	}

	return map[string]interface{}{
		"path":           req.Path,
		"delete_missing": deleteMissing,
		"scanned":        stats.Scanned,
		"added":          stats.Added,
		"updated":        stats.Updated,
		"unchanged":      stats.Unchanged,
		"deleted":        stats.Deleted,
	}, nil
}

// toolKBSearchDocuments searches the knowledge base.
func (s *Server) toolKBSearchDocuments(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
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

	results, err := s.remembrances.KB.SearchDocuments(ctx, req.Query, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("kb search error: %w", err)
	}

	return map[string]interface{}{
		"count":   len(results),
		"results": results,
	}, nil
}

// toolKBGetDocument retrieves a document from the knowledge base by path.
func (s *Server) toolKBGetDocument(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	doc, err := s.remembrances.KB.GetDocument(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("kb get error: %w", err)
	}
	if doc == nil {
		return map[string]interface{}{
			"path":  req.Path,
			"found": false,
		}, nil
	}

	return map[string]interface{}{
		"found":      true,
		"path":       doc.FilePath,
		"content":    doc.Content,
		"metadata":   doc.Metadata,
		"created_at": doc.CreatedAt,
		"updated_at": doc.UpdatedAt,
	}, nil
}

// toolKBDeleteDocument removes a document from the knowledge base.
func (s *Server) toolKBDeleteDocument(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	if err := s.remembrances.KB.DeleteDocument(ctx, req.Path); err != nil {
		return nil, fmt.Errorf("kb delete error: %w", err)
	}
	if err := s.remembrances.KB.DeleteDocumentFromFilesystem(req.Path); err != nil {
		return nil, fmt.Errorf("kb filesystem mirror error: %w", err)
	}
	return map[string]interface{}{
		"path":   req.Path,
		"status": "deleted",
	}, nil
}

// toolSaveEvent stores a temporal event.
func (s *Server) toolSaveEvent(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Subject  string                 `json:"subject"`
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	id, err := s.remembrances.Events.SaveEvent(ctx, req.Subject, req.Content, req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("save event error: %w", err)
	}
	return map[string]interface{}{
		"id":     id,
		"status": "saved",
	}, nil
}

// toolSearchEvents searches temporal events.
func (s *Server) toolSearchEvents(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		FromDate string `json:"from_date"`
		ToDate   string `json:"to_date"`
		Subject  string `json:"subject"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	opts := events.SearchOptions{
		Query:   req.Query,
		Limit:   req.Limit,
		Subject: req.Subject,
	}

	if req.FromDate != "" {
		t, err := time.Parse(time.RFC3339, req.FromDate)
		if err != nil {
			return nil, fmt.Errorf("invalid from_date format (use RFC3339): %w", err)
		}
		opts.FromDate = &t
	}
	if req.ToDate != "" {
		t, err := time.Parse(time.RFC3339, req.ToDate)
		if err != nil {
			return nil, fmt.Errorf("invalid to_date format (use RFC3339): %w", err)
		}
		opts.ToDate = &t
	}

	results, err := s.remembrances.Events.SearchEvents(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("search events error: %w", err)
	}

	return map[string]interface{}{
		"count":   len(results),
		"results": results,
	}, nil
}

// toolCodeIndexProject indexes a project directory for code search.
func (s *Server) toolCodeIndexProject(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		ProjectID string `json:"project_id"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	jobID, err := s.remembrances.Code.IndexProject(ctx, req.ProjectID, req.Path, nil)
	if err != nil {
		return nil, fmt.Errorf("index project error: %w", err)
	}
	return map[string]interface{}{
		"project_id": req.ProjectID,
		"job_id":     jobID,
		"status":     "indexing",
	}, nil
}

// toolCodeHybridSearch searches code symbols using hybrid search.
func (s *Server) toolCodeHybridSearch(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Query     string `json:"query"`
		ProjectID string `json:"project_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, err := s.remembrances.Code.HybridSearch(ctx, req.ProjectID, req.Query, req.Limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("code hybrid search error: %w", err)
	}
	return map[string]interface{}{
		"count":   len(results),
		"results": results,
	}, nil
}

// toolCodeFindSymbol finds symbols by name in an indexed project.
func (s *Server) toolCodeFindSymbol(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Name       string `json:"name"`
		ProjectID  string `json:"project_id"`
		SymbolType string `json:"symbol_type"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	q := code.SymbolQuery{
		ProjectID:       req.ProjectID,
		NamePathPattern: req.Name,
		Limit:           req.Limit,
	}
	if req.SymbolType != "" {
		q.IncludeTypes = []code.SymbolType{code.SymbolType(req.SymbolType)}
	}
	symbols, err := s.remembrances.Code.FindSymbol(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find symbol error: %w", err)
	}
	return map[string]interface{}{
		"count":   len(symbols),
		"symbols": symbols,
	}, nil
}

// toolCodeGetSymbolsOverview returns top-level symbols in a file.
func (s *Server) toolCodeGetSymbolsOverview(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		FilePath  string `json:"file_path"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	symbols, err := s.remembrances.Code.GetSymbolsOverview(ctx, req.ProjectID, req.FilePath, 100)
	if err != nil {
		return nil, fmt.Errorf("get symbols overview error: %w", err)
	}
	return map[string]interface{}{
		"count":   len(symbols),
		"symbols": symbols,
	}, nil
}

// toolCodeGetProjectStats returns statistics for an indexed project.
func (s *Server) toolCodeGetProjectStats(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	stats, err := s.remembrances.Code.GetProjectStats(ctx, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project stats error: %w", err)
	}
	return stats, nil
}

// toolCodeDeleteProject deletes an indexed project and all related indexed data.
func (s *Server) toolCodeDeleteProject(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	if err := s.remembrances.Code.DeleteProject(ctx, req.ProjectID); err != nil {
		return nil, fmt.Errorf("delete project error: %w", err)
	}

	return map[string]interface{}{
		"project_id": req.ProjectID,
		"status":     "deleted",
	}, nil
}

// toolCodeReindexFile re-indexes a single file in a project.
func (s *Server) toolCodeReindexFile(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		ProjectID string `json:"project_id"`
		FilePath  string `json:"file_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if req.FilePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	if err := s.remembrances.Code.ReindexFile(ctx, req.ProjectID, req.FilePath); err != nil {
		return nil, fmt.Errorf("reindex file error: %w", err)
	}
	return map[string]interface{}{
		"project_id": req.ProjectID,
		"file_path":  req.FilePath,
		"status":     "reindexed",
	}, nil
}

// toolCodeListProjects lists all indexed projects.
func (s *Server) toolCodeListProjects(ctx context.Context, params json.RawMessage) (interface{}, error) {
	projects, err := s.remembrances.Code.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects error: %w", err)
	}
	return map[string]interface{}{
		"count":    len(projects),
		"projects": projects,
	}, nil
}
