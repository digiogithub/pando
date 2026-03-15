package rag

import (
	"context"
	"sort"
	"sync"

	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/rag/sessions"
)

// UnifiedSearchResult combines results from KB documents and session conversations.
type UnifiedSearchResult struct {
	Source     string                 // "kb" or "session"
	Content    string
	FilePath   string                 // for KB results
	SessionID  string                 // for session results
	Title      string
	Score      float64
	Similarity float64
	Role       string // for session results: "user", "assistant", "mixed"
	TurnStart  int
	TurnEnd    int
	Metadata   map[string]interface{}
}

// UnifiedSearcher combines KB and session search results using RRF fusion.
type UnifiedSearcher struct {
	kb       *kb.KBStore
	sessions *sessions.SessionRAGStore
}

// NewUnifiedSearcher creates a new UnifiedSearcher.
// sessions may be nil, in which case only KB search is performed.
func NewUnifiedSearcher(kbStore *kb.KBStore, sessStore *sessions.SessionRAGStore) *UnifiedSearcher {
	return &UnifiedSearcher{
		kb:       kbStore,
		sessions: sessStore,
	}
}

// Search searches across KB and/or session RAG, fusing results with RRF.
// source: "kb", "sessions", or "all" (default: "all").
// Returns top-N unified results sorted by score descending.
func (u *UnifiedSearcher) Search(ctx context.Context, query string, limit int, source string) ([]UnifiedSearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	if source == "" {
		source = "all"
	}

	// Force KB-only when sessions store is unavailable.
	if u.sessions == nil && source != "kb" {
		source = "kb"
	}

	switch source {
	case "kb":
		return u.searchKBOnly(ctx, query, limit)
	case "sessions":
		return u.searchSessionsOnly(ctx, query, limit)
	default: // "all"
		return u.searchAll(ctx, query, limit)
	}
}

func (u *UnifiedSearcher) searchKBOnly(ctx context.Context, query string, limit int) ([]UnifiedSearchResult, error) {
	results, err := u.kb.SearchDocuments(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return convertKBResults(results), nil
}

func (u *UnifiedSearcher) searchSessionsOnly(ctx context.Context, query string, limit int) ([]UnifiedSearchResult, error) {
	results, err := u.sessions.SearchSessions(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return convertSessionResults(results), nil
}

func (u *UnifiedSearcher) searchAll(ctx context.Context, query string, limit int) ([]UnifiedSearchResult, error) {
	// Fetch more candidates for better RRF fusion quality.
	subLimit := limit * 3

	type kbResult struct {
		items []kb.SearchResult
		err   error
	}
	type sessResult struct {
		items []sessions.SessionSearchResult
		err   error
	}

	kbCh := make(chan kbResult, 1)
	sessCh := make(chan sessResult, 1)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		items, err := u.kb.SearchDocuments(ctx, query, subLimit)
		kbCh <- kbResult{items, err}
	}()

	go func() {
		defer wg.Done()
		items, err := u.sessions.SearchSessions(ctx, query, subLimit)
		sessCh <- sessResult{items, err}
	}()

	wg.Wait()

	kb := <-kbCh
	sess := <-sessCh

	if kb.err != nil {
		return nil, kb.err
	}
	if sess.err != nil {
		return nil, sess.err
	}

	kbUnified := convertKBResults(kb.items)
	sessUnified := convertSessionResults(sess.items)

	return unifiedRRFFuse(kbUnified, sessUnified, limit), nil
}

// convertKBResults maps kb.SearchResult to UnifiedSearchResult.
func convertKBResults(results []kb.SearchResult) []UnifiedSearchResult {
	out := make([]UnifiedSearchResult, len(results))
	for i, r := range results {
		out[i] = UnifiedSearchResult{
			Source:   "kb",
			Content:  r.ChunkContent,
			FilePath: r.Document.FilePath,
			Title:    r.Document.FilePath,
			Score:    r.Score,
			Metadata: r.Document.Metadata,
		}
	}
	return out
}

// convertSessionResults maps sessions.SessionSearchResult to UnifiedSearchResult.
func convertSessionResults(results []sessions.SessionSearchResult) []UnifiedSearchResult {
	out := make([]UnifiedSearchResult, len(results))
	for i, r := range results {
		out[i] = UnifiedSearchResult{
			Source:     "session",
			Content:    r.Content,
			SessionID:  r.SessionID,
			Title:      r.Title,
			Score:      r.Score,
			Similarity: r.Similarity,
			Role:       r.Role,
			TurnStart:  r.TurnStart,
			TurnEnd:    r.TurnEnd,
		}
	}
	return out
}

// unifiedRRFFuse merges KB and session result lists using Reciprocal Rank Fusion
// with per-source weights. KB weight=1.0, session weight=0.8.
func unifiedRRFFuse(kbResults, sessResults []UnifiedSearchResult, limit int) []UnifiedSearchResult {
	const rrfK = 60.0
	const kbWeight = 1.0
	const sessWeight = 0.8

	type key struct {
		source  string
		id      string // FilePath for kb, SessionID for session
		content string
	}

	type entry struct {
		result UnifiedSearchResult
		rrf    float64
	}

	byKey := make(map[key]*entry)

	for rank, r := range kbResults {
		k := key{source: "kb", id: r.FilePath, content: r.Content}
		score := kbWeight / (rrfK + float64(rank+1))
		if e, ok := byKey[k]; ok {
			e.rrf += score
		} else {
			rc := r
			byKey[k] = &entry{result: rc, rrf: score}
		}
	}

	for rank, r := range sessResults {
		k := key{source: "session", id: r.SessionID, content: r.Content}
		score := sessWeight / (rrfK + float64(rank+1))
		if e, ok := byKey[k]; ok {
			e.rrf += score
		} else {
			rc := r
			byKey[k] = &entry{result: rc, rrf: score}
		}
	}

	fused := make([]*entry, 0, len(byKey))
	for _, e := range byKey {
		fused = append(fused, e)
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].rrf > fused[j].rrf
	})

	results := make([]UnifiedSearchResult, 0, limit)
	for i, e := range fused {
		if i >= limit {
			break
		}
		e.result.Score = e.rrf
		results = append(results, e.result)
	}

	return results
}
