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
	codeIndexKeywords := []string{"code_hybrid_search", "code_find_symbol", "code_index_project"}

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
