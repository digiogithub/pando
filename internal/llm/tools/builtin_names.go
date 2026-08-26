package tools

import "strings"

// builtinToolNames is the canonical set of tools implemented natively by the
// agent. These are always invoked directly and never live in the MCP gateway
// registry. It is the single source of truth used by the MCP gateway to detect
// when a model mistakenly routes a built-in tool through mcp_call_tool and to
// return an actionable redirect instead of a bare "tool not found".
//
// The gateway meta-tools (mcp_query_catalog, mcp_call_tool) are intentionally
// excluded: the gateway handles them explicitly.
var builtinToolNames = map[string]struct{}{
	// Core file / edit / search / shell / todo / cache / diagnostics.
	BashToolName:         {},
	EditToolName:         {},
	GlobToolName:         {},
	GrepToolName:         {},
	LSToolName:           {},
	ViewToolName:         {},
	WriteToolName:        {},
	PatchToolName:        {},
	TodoWriteToolName:    {},
	CacheReadToolName:    {},
	CacheStatsToolName:   {},
	SavingsStatsToolName: {},
	PandoSetupToolName:   {},
	DiagnosticsToolName:  {},
	"tool_search":        {},

	// Design Studio tools are always builtin: they are never routed through the
	// MCP gateway.
	DesignCreateToolName:     {},
	DesignPatchToolName:      {},
	DesignRenderToolName:     {},
	DesignScreenshotToolName: {},
	DesignInspectToolName:    {},
	DesignVersionsToolName:   {},
	DesignExportToolName:     {},
	DesignCanvasToolName:     {},
	DesignSystemToolName:     {},
	DesignCritiqueToolName:   {},
	DesignSkillsToolName:     {},
	DesignPresentToolName:    {},

	// Memory tools.
	rememberToolName: {},
	recallToolName:   {},
	forgetToolName:   {},

	// Internal web / search / docs tools.
	FetchToolName:            {},
	GoogleSearchToolName:     {},
	BraveSearchToolName:      {},
	PerplexitySearchToolName: {},
	ExaSearchToolName:        {},
	SourcegraphToolName:      {},
	Context7ResolveToolName:  {},
	Context7DocsToolName:     {},

	// Mesnada delegated-agent tools.
	mesnadaSpawnToolName:     {},
	mesnadaGetTaskToolName:   {},
	mesnadaListTasksToolName: {},
	mesnadaWaitTaskToolName:  {},
	mesnadaCancelToolName:    {},
	mesnadaOutputToolName:    {},
	mesnadaNoteToolName:      {},
	mesnadaSwarmToolName:     {},

	// Remembrances / KB / code-intelligence (RAG) tools.
	kbAddDocumentToolName:            {},
	kbSearchDocumentsToolName:        {},
	kbGetDocumentToolName:            {},
	kbDeleteDocumentToolName:         {},
	kbRelatedDocumentsToolName:       {},
	saveEventToolName:                {},
	searchEventsToolName:             {},
	hybridSearchRemembrancesToolName: {},
	codeIndexProjectToolName:         {},
	codeIndexStatusToolName:          {},
	codeHybridSearchToolName:         {},
	codeFindSymbolToolName:           {},
	codeFindReferencesToolName:       {},
	codeGetSymbolsOverviewName:       {},
	codeGetProjectStatsToolName:      {},
	codeDeleteProjectToolName:        {},
	codeReindexFileToolName:          {},
	codeListProjectsToolName:         {},
	codeSearchPatternToolName:        {},
	codeImpactAnalysisToolName:       {},
	codeRelatedFilesToolName:         {},
}

// IsBuiltinTool reports whether name is an agent-native tool that is always
// callable directly and is never registered in the MCP gateway catalog. Browser
// tools share the "browser_" prefix and are matched as a group.
func IsBuiltinTool(name string) bool {
	if _, ok := builtinToolNames[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "browser_")
}
