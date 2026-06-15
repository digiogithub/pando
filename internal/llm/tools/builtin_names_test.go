package tools

import "testing"

func TestIsBuiltinTool(t *testing.T) {
	builtin := []string{
		"bash", "edit", "view", "glob", "grep", "ls", "write", "patch",
		"cache_read", "cache_stats", "diagnostics", "tool_search",
		"mesnada_spawn_agent", "mesnada_get_task_output",
		"kb_add_document", "code_find_symbol", "code_get_symbols_overview",
		"hybrid_search_remembrances", "fetch", "google_search",
		"browser_navigate", "browser_pdf",
	}
	for _, name := range builtin {
		if !IsBuiltinTool(name) {
			t.Errorf("expected %q to be a built-in tool", name)
		}
	}

	notBuiltin := []string{
		"mcp_query_catalog", "mcp_call_tool", // gateway meta-tools, handled separately
		"some_mcp_server_tool", "weather_lookup", "",
	}
	for _, name := range notBuiltin {
		if IsBuiltinTool(name) {
			t.Errorf("expected %q NOT to be a built-in tool", name)
		}
	}
}
