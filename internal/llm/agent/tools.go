package agent

import (
	"context"

	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
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
) []tools.BaseTool {
	ctx := context.Background()
	otherTools := GetMcpTools(ctx, permissions)
	if len(lspClients) > 0 {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
	}
	return append(
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
}

func CoderAgentToolsWithMesnada(
	mesnadaOrchestrator *orchestrator.Orchestrator,
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	skillManager *skills.SkillManager,
) []tools.BaseTool {
	baseTools := CoderAgentTools(
		permissions,
		sessions,
		messages,
		history,
		lspClients,
		skillManager,
	)
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
