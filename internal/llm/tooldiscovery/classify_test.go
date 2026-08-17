package tooldiscovery

import "testing"

func TestClassifySource(t *testing.T) {
	cases := []struct {
		name string
		want ToolSource
	}{
		// Core tools (note real names: "TodoWrite", not "todo_write").
		{"bash", SourceCore},
		{"edit", SourceCore},
		{"view", SourceCore},
		{"glob", SourceCore},
		{"grep", SourceCore},
		{"ls", SourceCore},
		{"write", SourceCore},
		{"patch", SourceCore},
		{"TodoWrite", SourceCore},
		{"cache_read", SourceCore},
		{"cache_stats", SourceCore},
		{"diagnostics", SourceCore},
		{"tool_search", SourceCore},

		// Gateway meta-tools.
		{"mcp_query_catalog", SourceGateway},
		{"mcp_call_tool", SourceGateway},

		// Mesnada — real names previously leaked to SourceMCP (deferred).
		{"mesnada_spawn_agent", SourceMesnada},
		{"mesnada_get_task_output", SourceMesnada},
		{"mesnada_list_tasks", SourceMesnada},

		// RAG / KB / code-intelligence.
		{"kb_add_document", SourceRAG},
		{"code_hybrid_search", SourceRAG},
		{"code_get_symbols_overview", SourceRAG},
		{"save_event", SourceRAG},
		{"hybrid_search_remembrances", SourceRAG},

		// Internal web/search/docs/browser/memory — real c7_* names previously
		// leaked to SourceMCP because the switch used "context7_*".
		{"fetch", SourceInternal},
		{"google_search", SourceInternal},
		{"c7_get_library_docs", SourceInternal},
		{"c7_resolve_library_id", SourceInternal},
		{"browser_navigate", SourceInternal},
		{"remember", SourceInternal},

		// Genuine MCP tools.
		{"mcp_github_create_pr", SourceMCP},
		{"github_list_repos", SourceMCP},
	}

	for _, tc := range cases {
		if got := ClassifySource(tc.name); got != tc.want {
			t.Errorf("ClassifySource(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
