package tools

import (
	"context"
	"testing"
)

// stubSearchProvider records the limit it was called with and returns a fixed
// set of entries so we can assert on how many tool_search requests.
type stubSearchProvider struct {
	gotLimit int
	entries  []ToolSearchEntry
}

func (s *stubSearchProvider) SearchTools(_ string, limit int) []ToolSearchEntry {
	s.gotLimit = limit
	return s.entries
}

func (s *stubSearchProvider) MarkDiscovered(string) {}

func TestToolSearch_DefaultLimitWiring(t *testing.T) {
	prov := &stubSearchProvider{entries: []ToolSearchEntry{{CanonicalName: "x", Description: "d"}}}
	// Configured default of 5 should be used when the caller omits "limit".
	tool := NewToolSearchTool(prov, 5)

	if _, err := tool.Run(context.Background(), ToolCall{Input: `{"query":"anything"}`}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.gotLimit != 5 {
		t.Errorf("expected configured default limit 5, got %d", prov.gotLimit)
	}

	// An explicit limit overrides the default.
	if _, err := tool.Run(context.Background(), ToolCall{Input: `{"query":"anything","limit":3}`}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.gotLimit != 3 {
		t.Errorf("expected explicit limit 3, got %d", prov.gotLimit)
	}
}

func TestToolSearch_DefaultLimitFallback(t *testing.T) {
	prov := &stubSearchProvider{entries: []ToolSearchEntry{{CanonicalName: "x", Description: "d"}}}
	// A non-positive configured default falls back to 8.
	tool := NewToolSearchTool(prov, 0)

	if _, err := tool.Run(context.Background(), ToolCall{Input: `{"query":"anything"}`}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.gotLimit != 8 {
		t.Errorf("expected fallback limit 8, got %d", prov.gotLimit)
	}
}
