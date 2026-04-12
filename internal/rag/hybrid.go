package rag

import (
	"context"
	"sort"

	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/rag/kb"
)

type HybridSearchResult struct {
	Source     string                 `json:"source"`
	Title      string                 `json:"title,omitempty"`
	Path       string                 `json:"path,omitempty"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	Rank       int                    `json:"rank"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	ProjectID  string                 `json:"project_id,omitempty"`
	FilePath   string                 `json:"file_path,omitempty"`
	SymbolName string                 `json:"symbol_name,omitempty"`
}

type HybridSearchOptions struct {
	Query           string
	Limit           int
	ProjectIDs      []string
	IncludeKB       bool
	IncludeSessions bool
	IncludeCode     bool
}

func (s *RemembrancesService) HybridSearch(ctx context.Context, opts HybridSearchOptions) ([]HybridSearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if !opts.IncludeKB && !opts.IncludeSessions && !opts.IncludeCode {
		opts.IncludeKB = true
		opts.IncludeSessions = true
		opts.IncludeCode = true
	}

	results := make([]HybridSearchResult, 0)

	if opts.IncludeKB && s.KB != nil {
		kbResults, err := s.KB.SearchDocuments(ctx, opts.Query, opts.Limit)
		if err != nil {
			return nil, err
		}
		for _, r := range kbResults {
			results = append(results, HybridSearchResult{
				Source:   "kb",
				Title:    r.Document.FilePath,
				Path:     r.Document.FilePath,
				Content:  r.ChunkContent,
				Score:    r.Score,
				Rank:     r.Rank,
				Metadata: r.Document.Metadata,
				FilePath: r.Document.FilePath,
			})
		}
	}

	if opts.IncludeSessions && s.Events != nil {
		eventResults, err := s.Events.SearchEvents(ctx, events.SearchOptions{Query: opts.Query, Subject: "session", Limit: opts.Limit})
		if err != nil {
			return nil, err
		}
		for _, r := range eventResults {
			item := HybridSearchResult{
				Source:   "session",
				Title:    r.Event.Subject,
				Content:  r.Event.Content,
				Score:    r.Score,
				Rank:     r.Rank,
				Metadata: r.Event.Metadata,
			}
			if sessionID, ok := r.Event.Metadata["session_id"].(string); ok {
				item.SessionID = sessionID
			}
			results = append(results, item)
		}
	}

	if opts.IncludeCode && s.Code != nil {
		for _, projectID := range opts.ProjectIDs {
			codeResults, err := s.Code.HybridSearch(ctx, projectID, opts.Query, opts.Limit, nil, nil)
			if err != nil {
				return nil, err
			}
			for _, r := range codeResults {
				if r.Symbol == nil {
					continue
				}
				results = append(results, HybridSearchResult{
					Source:     "code",
					Title:      r.Symbol.NamePath,
					Path:       r.Symbol.FilePath,
					Content:    r.Symbol.DocString,
					Score:      r.Score,
					Rank:       r.Rank,
					ProjectID:  r.Symbol.ProjectID,
					FilePath:   r.Symbol.FilePath,
					SymbolName: r.Symbol.NamePath,
					Metadata: map[string]interface{}{
						"language":    r.Symbol.Language,
						"symbol_type": r.Symbol.SymbolType,
						"start_line":  r.Symbol.StartLine,
						"end_line":    r.Symbol.EndLine,
					},
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	for i := range results {
		results[i].Rank = i + 1
	}
	return results, nil
}

var (
	_ = kb.SearchResult{}
	_ = code.HybridSearchResult{}
)
