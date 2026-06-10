package tooldiscovery_test

import (
	"testing"

	"github.com/digiogithub/pando/internal/llm/tooldiscovery"
)

func buildSearchRegistry() *tooldiscovery.Registry {
	reg := tooldiscovery.NewRegistry()
	tools := []struct {
		name   string
		desc   string
		source tooldiscovery.ToolSource
	}{
		{"bash", "Execute shell commands in the terminal", tooldiscovery.SourceCore},
		{"edit", "Edit file content with line-level precision", tooldiscovery.SourceCore},
		{"google_search", "Search the web using Google Custom Search API", tooldiscovery.SourceInternal},
		{"brave_search", "Search the web using Brave Search API", tooldiscovery.SourceInternal},
		{"mcp_github_list_repos", "List GitHub repositories for the authenticated user", tooldiscovery.SourceMCP},
		{"mcp_github_create_pr", "Create a pull request on GitHub", tooldiscovery.SourceMCP},
		{"code_hybrid_search", "Hybrid semantic and keyword search over indexed code", tooldiscovery.SourceRAG},
	}
	for _, td := range tools {
		_ = reg.Register(&stubTool{name: td.name, desc: td.desc}, td.source, false)
	}
	return reg
}

func TestSearch_BasicRelevance(t *testing.T) {
	reg := buildSearchRegistry()

	results := reg.Search("github pull request", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'github pull request'")
	}
	// mcp_github_create_pr should rank highest.
	if results[0].CanonicalName != "mcp_github_create_pr" {
		t.Errorf("expected mcp_github_create_pr first, got %s", results[0].CanonicalName)
	}
}

func TestSearch_WebSearch(t *testing.T) {
	reg := buildSearchRegistry()

	results := reg.Search("search web", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'search web'")
	}
	// Both search tools should appear.
	names := make(map[string]bool)
	for _, r := range results {
		names[r.CanonicalName] = true
	}
	if !names["google_search"] && !names["brave_search"] {
		t.Error("expected at least one web search tool in results")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	reg := buildSearchRegistry()
	if results := reg.Search("", 5); len(results) != 0 {
		t.Errorf("expected empty results for empty query, got %d", len(results))
	}
}

func TestSearch_LimitRespected(t *testing.T) {
	reg := buildSearchRegistry()
	results := reg.Search("search", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}
