package agent

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/userinput"
)

var globalLuaManagerForTools *luaengine.FilterManager

func appendLuaTools(base []tools.BaseTool) []tools.BaseTool {
	if globalLuaManagerForTools == nil {
		return base
	}
	return append(base, tools.NewLuaTools(globalLuaManagerForTools)...)
}

// ToolDescription carries the minimal information the ContextTrimmer needs about each tool.
type ToolDescription struct {
	Name        string
	Description string
}

// ContextTrimmer analyzes the user's first message and recommends a minimal tool set
// to reduce context window usage by filtering irrelevant tools from the initial API call.
// It is wired from app.go via SetContextTrimmer.
type ContextTrimmer interface {
	// ProfileTask returns the recommended tool names for the task.
	// Returns nil/empty to indicate "use all tools" (e.g., on error or low confidence).
	ProfileTask(ctx context.Context, firstMessage string, availableTools []ToolDescription) ([]string, error)
}

// alwaysIncludedTools is the core safe set that must remain in the tool list regardless
// of context trimming. These tools are needed for basic file operations that any task may require.
var alwaysIncludedTools = map[string]bool{
	"bash":  true,
	"edit":  true,
	"view":  true,
	"glob":  true,
	"grep":  true,
	"write": true,
	"patch": true,
	"ls":    true,
	// AskUserQuestion lets the agent ask the user for input; it must never be
	// trimmed away, otherwise the agent loses the ability to clarify mid-task.
	tools.AskUserQuestionToolName: true,
	// pando_setup is how the agent inspects its own configuration, models and
	// session. Trimming it on the first message would hide that for the whole
	// session, since the need for it usually appears mid-task.
	tools.PandoSetupToolName: true,
}

// askUserQuestionEnabled reports whether the AskUserQuestion tool should be
// registered. It is enabled by default and only removed when explicitly
// disabled via [InternalTools] AskUserQuestionDisabled.
func askUserQuestionEnabled() bool {
	cfg := config.Get()
	if cfg == nil {
		return true
	}
	return !cfg.InternalTools.AskUserQuestionDisabled
}

// maybeAskUserQuestionTool returns the AskUserQuestion tool wrapped in a slice
// (empty when disabled) so it can be spread into a base tool list.
func maybeAskUserQuestionTool(userInput userinput.Service) []tools.BaseTool {
	if !askUserQuestionEnabled() {
		return nil
	}
	return []tools.BaseTool{tools.NewAskUserQuestionTool(userInput)}
}

// filterToolsByNames returns the subset of allTools whose names are in the allowed set
// or in alwaysIncludedTools. If names is empty, all tools are returned unchanged.
func filterToolsByNames(allTools []tools.BaseTool, names []string) []tools.BaseTool {
	if len(names) == 0 {
		return allTools
	}
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		allowed[n] = true
	}
	result := make([]tools.BaseTool, 0, len(names)+len(alwaysIncludedTools))
	for _, t := range allTools {
		name := t.Info().Name
		if allowed[name] || alwaysIncludedTools[name] {
			result = append(result, t)
		}
	}
	return result
}

// toolDescriptionsFrom builds a []ToolDescription slice from a []tools.BaseTool
// so the ContextTrimmer knows the name and purpose of each available tool.
func toolDescriptionsFrom(baseTools []tools.BaseTool) []ToolDescription {
	descs := make([]ToolDescription, len(baseTools))
	for i, t := range baseTools {
		info := t.Info()
		descs[i] = ToolDescription{Name: info.Name, Description: info.Description}
	}
	return descs
}

func CoderAgentTools(
	permissions permission.Service,
	history history.Service,
	lspProvider tools.LSPProvider,
	userInput userinput.Service,
	sessions session.Service,
) []tools.BaseTool {
	ctx := context.Background()
	otherTools := GetMcpTools(ctx, permissions)
	if lspProvider != nil {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspProvider))
	}
	cfg := config.Get()
	if cfg != nil {
		it := cfg.InternalTools
		if it.FetchEnabled {
			otherTools = append(otherTools, tools.NewFetchTool(permissions))
		}
		if it.GoogleSearchEnabled && strings.TrimSpace(it.GoogleAPIKey) != "" {
			otherTools = append(otherTools, tools.NewGoogleSearchTool(permissions))
		}
		if it.BraveSearchEnabled && strings.TrimSpace(it.BraveAPIKey) != "" {
			otherTools = append(otherTools, tools.NewBraveSearchTool(permissions))
		}
		if it.PerplexitySearchEnabled && strings.TrimSpace(it.PerplexityAPIKey) != "" {
			otherTools = append(otherTools, tools.NewPerplexitySearchTool(permissions))
		}
		if it.ExaSearchEnabled && strings.TrimSpace(it.ExaAPIKey) != "" {
			otherTools = append(otherTools, tools.NewExaSearchTool(permissions))
		}
		if it.SourcegraphEnabled {
			otherTools = append(otherTools, tools.NewSourcegraphTool())
		}
		if it.Context7Enabled {
			otherTools = append(otherTools, tools.NewContext7Tools()...)
		}
		if it.BrowserEnabled {
			otherTools = append(otherTools,
				tools.NewBrowserNavigateTool(),
				tools.NewBrowserScreenshotTool(),
				tools.NewBrowserGetContentTool(),
				tools.NewBrowserEvaluateTool(),
				tools.NewBrowserClickTool(),
				tools.NewBrowserFillTool(),
				tools.NewBrowserScrollTool(),
				tools.NewBrowserConsoleLogsTool(),
				tools.NewBrowserNetworkTool(),
				tools.NewBrowserPDFTool(),
			)
		}
	}
	base := []tools.BaseTool{
		tools.NewBashTool(permissions),
		tools.NewEditTool(lspProvider, permissions, history),
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewViewTool(lspProvider),
		tools.NewImageCropTool(),
		tools.NewCacheReadTool(),
		tools.NewCacheStatsTool(),
		tools.NewSavingsStatsTool(),
		tools.NewPandoSetupTool(NewSetupBridge(sessions), lspProvider),
		tools.NewPatchTool(lspProvider, permissions, history),
		tools.NewWriteTool(lspProvider, permissions, history),
		tools.NewTodoWriteTool(),
	}
	base = append(base, maybeAskUserQuestionTool(userInput)...)
	base = append(base, otherTools...)
	result := appendLuaTools(base)
	return ApplyToolDiscovery(result)
}

func CoderAgentToolsWithMesnada(
	mesnadaOrchestrator *orchestrator.Orchestrator,
	remembrances *rag.RemembrancesService,
	gateway *mcpgateway.Gateway,
	permissions permission.Service,
	history history.Service,
	lspProvider tools.LSPProvider,
	userInput userinput.Service,
	sessions session.Service,
) []tools.BaseTool {
	ctx := context.Background()

	var baseTools []tools.BaseTool
	if gateway != nil {
		// Use gateway-aware MCP tools (catalog + call proxy + favorites).
		gatewayTools := GetMcpToolsWithGateway(ctx, permissions, gateway)
		baseTools = append(
			[]tools.BaseTool{
				tools.NewBashTool(permissions),
				tools.NewEditTool(lspProvider, permissions, history),
				tools.NewGlobTool(),
				tools.NewGrepTool(),
				tools.NewLsTool(),
				tools.NewViewTool(lspProvider),
				tools.NewCacheReadTool(),
				tools.NewCacheStatsTool(),
				tools.NewPandoSetupTool(NewSetupBridge(sessions), lspProvider),
				tools.NewPatchTool(lspProvider, permissions, history),
				tools.NewWriteTool(lspProvider, permissions, history),
				tools.NewTodoWriteTool(),
			},
			maybeAskUserQuestionTool(userInput)...,
		)
		baseTools = append(baseTools, gatewayTools...)
		if lspProvider != nil {
			baseTools = append(baseTools, tools.NewDiagnosticsTool(lspProvider))
		}
	} else {
		// CoderAgentTools already includes MCP server tools and internal tools
		// (Google/Brave/Perplexity/Context7/Browser), so we skip adding them again below.
		baseTools = CoderAgentTools(
			permissions,
			history,
			lspProvider,
			userInput,
			sessions,
		)
	}
	// Only add internal tools when using the gateway path; CoderAgentTools already
	// adds them when gateway is nil.
	if gateway != nil {
		cfg := config.Get()
		if cfg != nil {
			it := cfg.InternalTools
			if it.FetchEnabled {
				baseTools = append(baseTools, tools.NewFetchTool(permissions))
			}
			if it.GoogleSearchEnabled && strings.TrimSpace(it.GoogleAPIKey) != "" {
				baseTools = append(baseTools, tools.NewGoogleSearchTool(permissions))
			}
			if it.BraveSearchEnabled && strings.TrimSpace(it.BraveAPIKey) != "" {
				baseTools = append(baseTools, tools.NewBraveSearchTool(permissions))
			}
			if it.PerplexitySearchEnabled && strings.TrimSpace(it.PerplexityAPIKey) != "" {
				baseTools = append(baseTools, tools.NewPerplexitySearchTool(permissions))
			}
			if it.ExaSearchEnabled && strings.TrimSpace(it.ExaAPIKey) != "" {
				baseTools = append(baseTools, tools.NewExaSearchTool(permissions))
			}
			if it.SourcegraphEnabled {
				baseTools = append(baseTools, tools.NewSourcegraphTool())
			}
			if it.Context7Enabled {
				baseTools = append(baseTools, tools.NewContext7Tools()...)
			}
			if it.BrowserEnabled {
				baseTools = append(baseTools,
					tools.NewBrowserNavigateTool(),
					tools.NewBrowserScreenshotTool(),
					tools.NewBrowserGetContentTool(),
					tools.NewBrowserEvaluateTool(),
					tools.NewBrowserClickTool(),
					tools.NewBrowserFillTool(),
					tools.NewBrowserScrollTool(),
					tools.NewBrowserConsoleLogsTool(),
					tools.NewBrowserNetworkTool(),
					tools.NewBrowserPDFTool(),
				)
			}
		}
	}
	if mesnadaOrchestrator != nil {
		baseTools = append(baseTools,
			tools.NewMesnadaSpawnTool(mesnadaOrchestrator),
			tools.NewMesnadaGetTaskTool(mesnadaOrchestrator),
			tools.NewMesnadaListTasksTool(mesnadaOrchestrator),
			tools.NewMesnadaWaitTaskTool(mesnadaOrchestrator),
			tools.NewMesnadaCancelTaskTool(mesnadaOrchestrator),
			tools.NewMesnadaGetOutputTool(mesnadaOrchestrator),
			tools.NewMesnadaNoteTool(mesnadaOrchestrator),
		)
		// The non-blocking mesnada_await tool relies on idle-loop resurrection to
		// wake the parent. Only register it when both delegation and the
		// resurrection mechanism are enabled; otherwise the model could end its
		// turn with no wake event (deadlock). When unregistered, the model falls
		// back to the blocking mesnada_wait_task tool.
		if cfg := config.Get(); cfg != nil {
			del := cfg.Mesnada.Delegation
			if del.Enabled && del.ResurrectIdleLoop {
				baseTools = append(baseTools, tools.NewMesnadaAwaitTool(mesnadaOrchestrator))
			}
			// The swarm helper builds a verify-gated fan-out/merge workgraph; the
			// gate + conclusion forwarding only work when delegation is enabled.
			if del.Enabled {
				baseTools = append(baseTools, tools.NewMesnadaSwarmTool(mesnadaOrchestrator))
			}
		}
	}
	if remembrances != nil {
		baseTools = append(baseTools,
			tools.NewKBAddDocumentTool(remembrances.KB),
			tools.NewKBImportPathTool(remembrances.KB),
			tools.NewKBSearchDocumentsTool(remembrances.KB),
			tools.NewKBGetDocumentTool(remembrances.KB),
			tools.NewKBDeleteDocumentTool(remembrances.KB),
			tools.NewKBMarkOutdatedTool(remembrances.KB),
			tools.NewSaveEventTool(remembrances.Events),
			tools.NewSearchEventsTool(remembrances.Events),
			tools.NewHybridSearchRemembrancesTool(remembrances),
			tools.NewCodeIndexProjectTool(remembrances.Code),
			tools.NewCodeIndexStatusTool(remembrances.Code),
			tools.NewCodeHybridSearchTool(remembrances.Code),
			tools.NewCodeFindSymbolTool(remembrances.Code),
			tools.NewCodeFindReferencesTool(remembrances.Code),
			tools.NewCodeGetSymbolsOverviewTool(remembrances.Code),
			tools.NewCodeGetProjectStatsTool(remembrances.Code),
			tools.NewCodeDeleteProjectTool(remembrances.Code),
			tools.NewCodeReindexFileTool(remembrances.Code),
			tools.NewCodeListProjectsTool(remembrances.Code),
			tools.NewCodeSearchPatternTool(remembrances.Code),
			tools.NewCodeImpactAnalysisTool(remembrances.Code),
			tools.NewCodeRelatedFilesTool(remembrances.Code),
		)
		// The wiki graph navigator only exists when the graph does: with
		// KBWikiLinks off it would answer empty on every call.
		if remembrances.KB != nil && remembrances.KB.WikiLinksEnabled() {
			baseTools = append(baseTools, tools.NewKBRelatedDocumentsTool(remembrances.KB))
		}
		if cfg := config.Get(); cfg != nil && cfg.Remembrances.MemoryEnabled {
			baseTools = append(baseTools,
				tools.NewRememberTool(remembrances.KB, cfg.Remembrances.MemoryDefaultTTLDays),
				tools.NewRecallTool(remembrances.KB, cfg.Remembrances.MemoryDefaultTTLDays),
				tools.NewForgetTool(remembrances.KB),
			)
		}
	}
	result := appendLuaTools(baseTools)
	return ApplyToolDiscovery(result)
}

// ContextEnricherAgentTools returns the read-only retrieval tool set used by the
// context-enrichment agent loop: knowledge base, memories, past events and the code
// index, plus plain file inspection. No tool in this set can modify state, so the loop
// can never touch the workspace while it gathers context for the main agent.
func ContextEnricherAgentTools(remembrances *rag.RemembrancesService, lspProvider tools.LSPProvider) []tools.BaseTool {
	base := []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewViewTool(lspProvider),
	}
	if remembrances != nil {
		base = append(base,
			tools.NewKBSearchDocumentsTool(remembrances.KB),
			tools.NewKBGetDocumentTool(remembrances.KB),
			tools.NewSearchEventsTool(remembrances.Events),
			tools.NewHybridSearchRemembrancesTool(remembrances),
			tools.NewCodeHybridSearchTool(remembrances.Code),
			tools.NewCodeFindSymbolTool(remembrances.Code),
			tools.NewCodeFindReferencesTool(remembrances.Code),
			tools.NewCodeGetSymbolsOverviewTool(remembrances.Code),
			tools.NewCodeSearchPatternTool(remembrances.Code),
			tools.NewCodeRelatedFilesTool(remembrances.Code),
			tools.NewCodeListProjectsTool(remembrances.Code),
		)
		if remembrances.KB != nil && remembrances.KB.WikiLinksEnabled() {
			base = append(base, tools.NewKBRelatedDocumentsTool(remembrances.KB))
		}
		if cfg := config.Get(); cfg != nil && cfg.Remembrances.MemoryEnabled {
			base = append(base, tools.NewRecallTool(remembrances.KB, cfg.Remembrances.MemoryDefaultTTLDays))
		}
	}
	return base
}

func TaskAgentTools(lspProvider tools.LSPProvider) []tools.BaseTool {
	base := []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewViewTool(lspProvider),
		tools.NewCacheReadTool(),
	}
	if cfg := config.Get(); cfg != nil && cfg.InternalTools.SourcegraphEnabled {
		base = append(base, tools.NewSourcegraphTool())
	}
	return appendLuaTools(base)
}
