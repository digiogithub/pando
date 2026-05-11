package prompt

import (
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// CapabilityDetector detects available capabilities based on configuration,
// MCP server names, and available tool names.
type CapabilityDetector struct {
	config     *config.Config
	mcpServers []string
	tools      []string
}

// NewCapabilityDetector creates a new CapabilityDetector.
func NewCapabilityDetector(cfg *config.Config, mcpServers []string, tools []string) *CapabilityDetector {
	return &CapabilityDetector{
		config:     cfg,
		mcpServers: mcpServers,
		tools:      tools,
	}
}

// Detect returns a map of capability names to their availability.
func (d *CapabilityDetector) Detect() map[string]bool {
	caps := map[string]bool{
		"remembrances":  false,
		"orchestration": false,
		"web_search":    false,
		"code_indexing": false,
		"lsp":           false,
	}

	for _, server := range d.mcpServers {
		lower := strings.ToLower(server)
		if strings.Contains(lower, "remembrances") {
			caps["remembrances"] = true
		}
		if strings.Contains(lower, "mesnada") {
			caps["orchestration"] = true
		}
	}

	webSearchKeywords := []string{"google_search", "brave_search", "perplexity_search", "web_search", "fetch"}
	codeIndexKeywords := []string{
		"code_hybrid_search",
		"code_find_symbol",
		"code_find_references",
		"code_get_symbols_overview",
		"code_search_pattern",
		"code_list_projects",
		"code_index_project",
	}
	remembrancesKeywords := []string{
		"kb_",
		"search_events",
		"save_event",
		"hybrid_search_remembrances",
		"to_remember",
		"last_to_remember",
		"save_fact",
		"get_fact",
		"list_facts",
	}
	orchestrationKeywords := []string{
		"mesnada_",
		"spawn_agent",
		"wait_task",
		"wait_multiple",
		"get_task",
		"get_task_output",
		"list_tasks",
		"cancel_task",
		"pause_task",
		"resume_task",
		"delete_task",
		"set_progress",
		"get_stats",
	}

	for _, tool := range d.tools {
		lower := strings.ToLower(tool)
		for _, kw := range webSearchKeywords {
			if strings.Contains(lower, kw) {
				caps["web_search"] = true
				break
			}
		}
		for _, kw := range codeIndexKeywords {
			if strings.Contains(lower, kw) {
				caps["code_indexing"] = true
				break
			}
		}
		for _, kw := range remembrancesKeywords {
			if strings.Contains(lower, kw) {
				caps["remembrances"] = true
				break
			}
		}
		for _, kw := range orchestrationKeywords {
			if strings.Contains(lower, kw) {
				caps["orchestration"] = true
				break
			}
		}
	}

	// LSP: check if config has any non-disabled LSP entry
	if d.config != nil {
		for _, lspCfg := range d.config.LSP {
			if !lspCfg.Disabled {
				caps["lsp"] = true
				break
			}
		}
	}

	return caps
}

// DetectWebSearchDetails returns detailed web search provider availability.
func (d *CapabilityDetector) DetectWebSearchDetails() (google, brave, perplexity bool) {
	for _, tool := range d.tools {
		lower := strings.ToLower(tool)
		if strings.Contains(lower, "google_search") {
			google = true
		}
		if strings.Contains(lower, "brave_search") {
			brave = true
		}
		if strings.Contains(lower, "perplexity_search") {
			perplexity = true
		}
	}
	return
}
