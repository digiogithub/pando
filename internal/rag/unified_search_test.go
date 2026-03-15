package rag

import (
	"testing"

	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/internal/rag/sessions"
)

func TestConvertKBResults(t *testing.T) {
	input := []kb.SearchResult{
		{
			Document:     kb.Document{FilePath: "docs/foo.md", Metadata: map[string]interface{}{"tag": "test"}},
			ChunkContent: "hello world",
			Score:        0.9,
			Rank:         1,
		},
		{
			Document:     kb.Document{FilePath: "docs/bar.md"},
			ChunkContent: "second chunk",
			Score:        0.7,
			Rank:         2,
		},
	}

	got := convertKBResults(input)

	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].Source != "kb" {
		t.Errorf("expected source=kb, got %s", got[0].Source)
	}
	if got[0].FilePath != "docs/foo.md" {
		t.Errorf("unexpected FilePath: %s", got[0].FilePath)
	}
	if got[0].Content != "hello world" {
		t.Errorf("unexpected Content: %s", got[0].Content)
	}
}

func TestConvertSessionResults(t *testing.T) {
	input := []sessions.SessionSearchResult{
		{
			SessionID:  "sess-1",
			Title:      "My Session",
			Content:    "user asked about foo",
			Role:       "user",
			Similarity: 0.85,
			Score:      0.85,
			Source:     "session",
			TurnStart:  2,
			TurnEnd:    4,
		},
	}

	got := convertSessionResults(input)

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Source != "session" {
		t.Errorf("expected source=session, got %s", got[0].Source)
	}
	if got[0].SessionID != "sess-1" {
		t.Errorf("unexpected SessionID: %s", got[0].SessionID)
	}
	if got[0].Role != "user" {
		t.Errorf("unexpected Role: %s", got[0].Role)
	}
	if got[0].TurnStart != 2 || got[0].TurnEnd != 4 {
		t.Errorf("unexpected turns: %d-%d", got[0].TurnStart, got[0].TurnEnd)
	}
}

func TestUnifiedRRFFuse_KBOnly(t *testing.T) {
	kbResults := []UnifiedSearchResult{
		{Source: "kb", FilePath: "a.md", Content: "alpha", Score: 0.9},
		{Source: "kb", FilePath: "b.md", Content: "beta", Score: 0.7},
	}

	fused := unifiedRRFFuse(kbResults, nil, 5)

	if len(fused) != 2 {
		t.Fatalf("expected 2 results, got %d", len(fused))
	}
	// All should be kb source.
	for _, r := range fused {
		if r.Source != "kb" {
			t.Errorf("expected source=kb, got %s", r.Source)
		}
	}
	// Scores should be descending.
	if fused[0].Score < fused[1].Score {
		t.Errorf("results not sorted by score descending")
	}
}

func TestUnifiedRRFFuse_SessionsOnly(t *testing.T) {
	sessResults := []UnifiedSearchResult{
		{Source: "session", SessionID: "s1", Content: "conv about foo", Score: 0.8},
		{Source: "session", SessionID: "s2", Content: "conv about bar", Score: 0.6},
	}

	fused := unifiedRRFFuse(nil, sessResults, 5)

	if len(fused) != 2 {
		t.Fatalf("expected 2 results, got %d", len(fused))
	}
	for _, r := range fused {
		if r.Source != "session" {
			t.Errorf("expected source=session, got %s", r.Source)
		}
	}
	if fused[0].Score < fused[1].Score {
		t.Errorf("results not sorted by score descending")
	}
}

func TestUnifiedRRFFuse_Combined_RRFOrdering(t *testing.T) {
	// KB result ranked first, session result also ranked first.
	// KB weight (1.0) > session weight (0.8), so top KB result should beat top session result.
	kbResults := []UnifiedSearchResult{
		{Source: "kb", FilePath: "x.md", Content: "kb top"},
	}
	sessResults := []UnifiedSearchResult{
		{Source: "session", SessionID: "s1", Content: "sess top"},
	}

	fused := unifiedRRFFuse(kbResults, sessResults, 5)

	if len(fused) != 2 {
		t.Fatalf("expected 2 results, got %d", len(fused))
	}
	// KB result should be ranked higher due to higher weight.
	if fused[0].Source != "kb" {
		t.Errorf("expected kb result first (higher weight), got %s", fused[0].Source)
	}
}

func TestUnifiedRRFFuse_LimitRespected(t *testing.T) {
	var kbResults []UnifiedSearchResult
	for i := 0; i < 10; i++ {
		kbResults = append(kbResults, UnifiedSearchResult{
			Source:   "kb",
			FilePath: "doc",
			Content:  string(rune('a' + i)), // unique content to avoid dedup
		})
	}

	fused := unifiedRRFFuse(kbResults, nil, 3)

	if len(fused) != 3 {
		t.Errorf("expected 3 results (limit), got %d", len(fused))
	}
}

func TestNewUnifiedSearcher_NilSessions(t *testing.T) {
	u := NewUnifiedSearcher(nil, nil)
	if u == nil {
		t.Fatal("expected non-nil UnifiedSearcher")
	}
	if u.sessions != nil {
		t.Error("expected sessions to be nil")
	}
}
