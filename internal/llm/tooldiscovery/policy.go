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
	// Start with the canonical core tools that must always be visible.
	coreTools := []string{
		"bash", "edit", "view", "glob", "grep", "write", "patch", "ls",
		"cache_read", "cache_stats", "todo_write", "tool_search",
		"mcp_query_catalog", "mcp_call_tool",
	}
	// Append cross-model aliases so that if a model emits an alias name (e.g.
	// "read" for "view") during tool-search result evaluation it is still treated
	// as a non-deferred, always-visible capability.
	nonDeferred := append(coreTools, tools.CrossModelAliasNames()...)
	return PolicyConfig{
		Mode:             ModeAuto,
		MaxDirectTools:   64,
		NonDeferredTools: nonDeferred,
		DeferredSources:  []string{string(SourceMCP), string(SourceLua)},
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
// mode is active so the model can always find deferred tools. When the
// registry holds catalog-only entries (remote MCP tools) tool_search is the
// only way to reach them, so it is included even below the activation
// threshold.
func (p *SelectionPolicy) Apply(reg *Registry, toolSearch tools.BaseTool) []tools.BaseTool {
	all := reg.All()
	total := len(all)

	activate := p.shouldActivate(total)
	if !activate {
		visible := reg.AllTools()
		if toolSearch != nil && reg.HasRemoteEntries() {
			visible = append([]tools.BaseTool{toolSearch}, visible...)
		}
		return visible
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
		source := ClassifySource(info.Name)
		nd := nonDeferredNames[strings.ToLower(info.Name)]
		// Always mark core tools as non-deferred.
		if source == SourceCore {
			nd = true
		}
		_ = reg.Register(t, source, nd) // ignore alias collision in build path
	}
	return reg
}

// ClassifySource infers the ToolSource from the tool name. The set of
// agent-native tools is sourced from tools.IsBuiltinTool so this classifier and
// the MCP gateway share a single source of truth: any built-in tool stays a
// directly-visible source and is never deferred as an MCP tool. Real MCP tools
// follow the "<server>_<toolname>" convention established in mcp-tools.go.
func ClassifySource(name string) ToolSource {
	switch name {
	case "mcp_query_catalog", "mcp_call_tool":
		return SourceGateway
	}
	if isCoreToolName(name) {
		return SourceCore
	}
	switch {
	case strings.HasPrefix(name, "mesnada_"):
		return SourceMesnada
	case isRAGToolName(name):
		return SourceRAG
	}
	// Any other built-in agent tool (web/search/docs/browser/memory) is internal
	// and must stay directly visible — never deferred as an MCP tool.
	if tools.IsBuiltinTool(name) {
		return SourceInternal
	}
	// Anything else with an underscore is a real MCP "<server>_<toolname>".
	if strings.Contains(name, "_") {
		return SourceMCP
	}
	return SourceInternal
}

// isCoreToolName reports whether name is one of the always-on core tools
// (file/edit/search/shell/cache/todo/diagnostics/tool_search).
func isCoreToolName(name string) bool {
	switch name {
	case tools.BashToolName, tools.EditToolName, tools.GlobToolName,
		tools.GrepToolName, tools.LSToolName, tools.ViewToolName,
		tools.WriteToolName, tools.PatchToolName, tools.TodoWriteToolName,
		tools.CacheReadToolName, tools.CacheStatsToolName,
		tools.DiagnosticsToolName, "tool_search":
		return true
	}
	return false
}

// isRAGToolName reports whether name belongs to the remembrances / KB /
// code-intelligence tool group.
func isRAGToolName(name string) bool {
	if strings.HasPrefix(name, "kb_") || strings.HasPrefix(name, "code_") {
		return true
	}
	switch name {
	case "save_event", "search_events", "hybrid_search_remembrances":
		return true
	}
	return false
}
