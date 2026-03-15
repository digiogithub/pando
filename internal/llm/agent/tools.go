package agent

import (
	"context"

	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
)

func CoderAgentTools(
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	skillManager *skills.SkillManager,
	luaMgr *luaengine.FilterManager,
) []tools.BaseTool {
	ctx := context.Background()
	otherTools := GetMcpTools(ctx, permissions)
	if len(lspClients) > 0 {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
	}
	baseTools := append(
		[]tools.BaseTool{
			tools.NewBashTool(permissions),
			tools.NewEditTool(lspClients, permissions, history),
			tools.NewFetchTool(permissions),
			tools.NewGlobTool(),
			tools.NewGrepTool(),
			tools.NewLsTool(),
			tools.NewSourcegraphTool(),
			tools.NewViewTool(lspClients),
			tools.NewPatchTool(lspClients, permissions, history),
			tools.NewWriteTool(lspClients, permissions, history),
			NewAgentTool(sessions, messages, lspClients, skillManager),
		}, otherTools...,
	)
	if luaMgr != nil && luaMgr.IsEnabled() {
		luaTools := tools.NewLuaToolsFromManager(luaMgr)
		if len(luaTools) > 0 {
			logging.Info("Registering Lua tools", "count", len(luaTools))
			baseTools = append(baseTools, luaTools...)
		}
	}
	return baseTools
}

func CoderAgentToolsWithMesnada(
	mesnadaOrchestrator *orchestrator.Orchestrator,
	remembrances *rag.RemembrancesService,
	gateway *mcpgateway.Gateway,
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	skillManager *skills.SkillManager,
	luaMgr *luaengine.FilterManager,
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
				tools.NewFetchTool(permissions),
				tools.NewGlobTool(),
				tools.NewGrepTool(),
				tools.NewLsTool(),
				tools.NewSourcegraphTool(),
				tools.NewViewTool(lspClients),
				tools.NewPatchTool(lspClients, permissions, history),
				tools.NewWriteTool(lspClients, permissions, history),
				NewAgentTool(sessions, messages, lspClients, skillManager),
			},
			gatewayTools...,
		)
		if len(lspClients) > 0 {
			baseTools = append(baseTools, tools.NewDiagnosticsTool(lspClients))
		}
		if luaMgr != nil && luaMgr.IsEnabled() {
			luaTools := tools.NewLuaToolsFromManager(luaMgr)
			if len(luaTools) > 0 {
				logging.Info("Registering Lua tools", "count", len(luaTools))
				baseTools = append(baseTools, luaTools...)
			}
		}
	} else {
		baseTools = CoderAgentTools(
			permissions,
			sessions,
			messages,
			history,
			lspClients,
			skillManager,
			luaMgr,
		)
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
			tools.NewKBSearchDocumentsTool(remembrances.KB),
			tools.NewKBGetDocumentTool(remembrances.KB),
			tools.NewKBDeleteDocumentTool(remembrances.KB),
			tools.NewSaveEventTool(remembrances.Events),
			tools.NewSearchEventsTool(remembrances.Events),
			tools.NewCodeIndexProjectTool(remembrances.Code),
			tools.NewCodeIndexStatusTool(remembrances.Code),
			tools.NewCodeHybridSearchTool(remembrances.Code),
			tools.NewCodeFindSymbolTool(remembrances.Code),
			tools.NewCodeGetSymbolsOverviewTool(remembrances.Code),
			tools.NewCodeGetProjectStatsTool(remembrances.Code),
			tools.NewCodeReindexFileTool(remembrances.Code),
			tools.NewCodeListProjectsTool(remembrances.Code),
			tools.NewCodeSearchPatternTool(remembrances.Code),
		)
	}
	return baseTools
}

func TaskAgentTools(lspClients map[string]*lsp.Client) []tools.BaseTool {
	return []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewSourcegraphTool(),
		tools.NewViewTool(lspClients),
	}
}
