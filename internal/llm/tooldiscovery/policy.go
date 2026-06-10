package tooldiscovery

import (
	"strings"

	"github.com/digiogithub/pando/internal/llm/tools"
)

// PolicyMode controls when deferred tool selection is active.
type PolicyMode string

const (
	// ModeAuto activates discovery when total tool count exceeds MaxDirectTools.
	ModeAuto PolicyMode = "auto"
	// ModeAlways forces discovery mode regardless of tool count.
	ModeAlways PolicyMode = "always"
	// ModeOff disables discovery — all tools are always visible.
	ModeOff PolicyMode = "off"
)

// PolicyConfig parameterises the selection policy.
type PolicyConfig struct {
	Mode             PolicyMode
	MaxDirectTools   int
	NonDeferredTools []string // tool names always kept visible
	DeferredSources  []string // source labels deferred by default
}

// DefaultPolicyConfig returns conservative defaults.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		Mode:           ModeAuto,
		MaxDirectTools: 64,
		NonDeferredTools: []string{
			"bash", "edit", "view", "glob", "grep", "write", "patch", "ls",
			"cache_read", "cache_stats", "todo_write", "tool_search",
			"mcp_query_catalog", "mcp_call_tool",
		},
		DeferredSources: []string{string(SourceMCP), string(SourceLua)},
	}
}

// SelectionPolicy decides which tools are immediately visible and which are deferred.
type SelectionPolicy struct {
	cfg          PolicyConfig
	nonDeferred  map[string]bool
	deferSources map[string]bool
}

// NewSelectionPolicy creates a SelectionPolicy from cfg.
func NewSelectionPolicy(cfg PolicyConfig) *SelectionPolicy {
	nd := make(map[string]bool, len(cfg.NonDeferredTools))
	for _, n := range cfg.NonDeferredTools {
		nd[strings.ToLower(n)] = true
	}
	ds := make(map[string]bool, len(cfg.DeferredSources))
	for _, s := range cfg.DeferredSources {
		ds[strings.ToLower(s)] = true
	}
	return &SelectionPolicy{cfg: cfg, nonDeferred: nd, deferSources: ds}
}

// Apply returns the visible tool slice given the full set in the registry.
// If toolSearch is non-nil it is prepended to the visible set when discovery
// mode is active so the model can always find deferred tools.
func (p *SelectionPolicy) Apply(reg *Registry, toolSearch tools.BaseTool) []tools.BaseTool {
	all := reg.All()
	total := len(all)

	activate := p.shouldActivate(total)
	if !activate {
		return reg.AllTools()
	}

	visible := make([]tools.BaseTool, 0, p.cfg.MaxDirectTools)

	// Always add tool_search first so it is the most prominent tool.
	if toolSearch != nil {
		visible = append(visible, toolSearch)
	}

	// Also add any session-discovered tools (from prior tool_search calls).
	discovered := make(map[string]bool)
	for _, t := range reg.DiscoveredTools() {
		name := t.Info().Name
		if !discovered[name] {
			visible = append(visible, t)
			discovered[name] = true
		}
	}

	for _, e := range all {
		name := e.Metadata.CanonicalName
		if name == "tool_search" || discovered[name] {
			continue
		}
		if p.isNonDeferred(e) {
			visible = append(visible, e.Tool)
		}
	}

	return visible
}

// IsDeferred reports whether a given entry should be hidden initially.
func (p *SelectionPolicy) IsDeferred(e Entry) bool {
	return p.shouldActivate(1) && !p.isNonDeferred(e)
}

func (p *SelectionPolicy) isNonDeferred(e Entry) bool {
	name := strings.ToLower(e.Metadata.CanonicalName)
	if p.nonDeferred[name] {
		return true
	}
	// Explicit NonDeferred flag from tool metadata takes precedence.
	if e.Metadata.NonDeferred {
		return true
	}
	// Tools whose source is not in the deferred sources list are non-deferred.
	src := strings.ToLower(string(e.Metadata.Source))
	return !p.deferSources[src]
}

func (p *SelectionPolicy) shouldActivate(total int) bool {
	switch p.cfg.Mode {
	case ModeAlways:
		return true
	case ModeOff:
		return false
	default: // ModeAuto
		return total > p.cfg.MaxDirectTools
	}
}

// BuildRegistry assembles a Registry from a flat tool slice, classifying each
// tool by source based on its name prefix and optionally implemented metadata.
func BuildRegistry(allTools []tools.BaseTool, nonDeferredNames map[string]bool) *Registry {
	reg := NewRegistry()
	for _, t := range allTools {
		info := t.Info()
		source := classifySource(info.Name)
		nd := nonDeferredNames[strings.ToLower(info.Name)]
		// Always mark core tools as non-deferred.
		if source == SourceCore {
			nd = true
		}
		_ = reg.Register(t, source, nd) // ignore alias collision in build path
	}
	return reg
}

// classifySource infers the ToolSource from the tool name. MCP tools follow
// the "<server>_<toolname>" convention established in mcp-tools.go.
func classifySource(name string) ToolSource {
	switch name {
	case "bash", "edit", "view", "glob", "grep", "ls", "write", "patch",
		"todo_write", "cache_read", "cache_stats", "diagnostics", "tool_search":
		return SourceCore
	case "mcp_query_catalog", "mcp_call_tool":
		return SourceGateway
	case "mesnada_spawn", "mesnada_get_task", "mesnada_list_tasks",
		"mesnada_wait_task", "mesnada_cancel_task", "mesnada_get_output":
		return SourceMesnada
	case "kb_add_document", "kb_import_path", "kb_search_documents",
		"kb_get_document", "kb_delete_document", "save_event", "search_events",
		"hybrid_search_remembrances", "code_index_project", "code_index_status",
		"code_hybrid_search", "code_find_symbol", "code_find_references",
		"code_get_symbols_overview", "code_get_project_stats", "code_delete_project",
		"code_reindex_file", "code_list_projects", "code_search_pattern":
		return SourceRAG
	case "fetch", "google_search", "brave_search", "perplexity_search",
		"exa_search", "sourcegraph", "context7_get_library_docs",
		"context7_resolve_library_id":
		return SourceInternal
	}
	// Browser tools
	if strings.HasPrefix(name, "browser_") {
		return SourceInternal
	}
	// Anything else with an underscore and matching known MCP prefixes is MCP.
	// In practice all MCP tools are "<server>_<tool>" format.
	if strings.Contains(name, "_") {
		return SourceMCP
	}
	return SourceInternal
}
