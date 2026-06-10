package agent

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/rag"
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
	lspClients map[string]*lsp.Client,
) []tools.BaseTool {
	ctx := context.Background()
	otherTools := GetMcpTools(ctx, permissions)
	if len(lspClients) > 0 {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
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
	base := append(
		[]tools.BaseTool{
			tools.NewBashTool(permissions),
			tools.NewEditTool(lspClients, permissions, history),
			tools.NewGlobTool(),
			tools.NewGrepTool(),
			tools.NewLsTool(),
			tools.NewViewTool(lspClients),
			tools.NewCacheReadTool(),
			tools.NewCacheStatsTool(),
			tools.NewPatchTool(lspClients, permissions, history),
			tools.NewWriteTool(lspClients, permissions, history),
			tools.NewTodoWriteTool(),
		}, otherTools...,
	)
	result := appendLuaTools(base)
	return ApplyToolDiscovery(result)
}

func CoderAgentToolsWithMesnada(
	mesnadaOrchestrator *orchestrator.Orchestrator,
	remembrances *rag.RemembrancesService,
	gateway *mcpgateway.Gateway,
	permissions permission.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
) []tools.BaseTool {
	ctx := context.Background()

	var baseTools []tools.BaseTool
	if gateway != nil {
		// Use gateway-aware MCP tools (catalog + call proxy + favorites).
		gatewayTools := GetMcpToolsWithGateway(ctx, permissions, gateway)
		baseTools = append(
			[]tools.BaseTool{
				tools.NewBashTool(permissions),
				tools.NewEditTool(lspClients, permissions, history),
				tools.NewGlobTool(),
				tools.NewGrepTool(),
				tools.NewLsTool(),
				tools.NewViewTool(lspClients),
				tools.NewCacheReadTool(),
				tools.NewCacheStatsTool(),
				tools.NewPatchTool(lspClients, permissions, history),
				tools.NewWriteTool(lspClients, permissions, history),
				tools.NewTodoWriteTool(),
			},
			gatewayTools...,
		)
		if len(lspClients) > 0 {
			baseTools = append(baseTools, tools.NewDiagnosticsTool(lspClients))
		}
	} else {
		// CoderAgentTools already includes MCP server tools and internal tools
		// (Google/Brave/Perplexity/Context7/Browser), so we skip adding them again below.
		baseTools = CoderAgentTools(
			permissions,
			history,
			lspClients,
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
		)
	}
	if remembrances != nil {
		baseTools = append(baseTools,
			tools.NewKBAddDocumentTool(remembrances.KB),
			tools.NewKBImportPathTool(remembrances.KB),
			tools.NewKBSearchDocumentsTool(remembrances.KB),
			tools.NewKBGetDocumentTool(remembrances.KB),
			tools.NewKBDeleteDocumentTool(remembrances.KB),
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
		)
	}
	result := appendLuaTools(baseTools)
	return ApplyToolDiscovery(result)
}

func TaskAgentTools(lspClients map[string]*lsp.Client) []tools.BaseTool {
	base := []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewViewTool(lspClients),
		tools.NewCacheReadTool(),
	}
	if cfg := config.Get(); cfg != nil && cfg.InternalTools.SourcegraphEnabled {
		base = append(base, tools.NewSourcegraphTool())
	}
	return appendLuaTools(base)
}
