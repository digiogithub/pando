package tools

import (
	"context"

	delegationdepth "github.com/digiogithub/pando/internal/mesnada/depth"
	"github.com/digiogithub/pando/internal/mesnada/engines"
)

// delegationSpawnTools are the tools that start (or exist only to wait on)
// further subagents. They are hidden from an agent that already sits at the
// delegation nesting cap, so the model does not waste calls on spawns that would
// be refused anyway.
var delegationSpawnTools = map[string]bool{
	mesnadaSpawnToolName: true,
	mesnadaSwarmToolName: true,
	mesnadaAwaitToolName: true,
}

// IsDelegationSpawnTool reports whether name is one of the spawn/swarm/await
// tools governed by the nesting cap.
func IsDelegationSpawnTool(name string) bool {
	return delegationSpawnTools[name]
}

// SessionCanDelegate reports whether the agent running sessionID may still
// spawn subagents under the live cap (mesnada.delegation.maxDepth, or 1 when
// allowNestedSubagents is false). The session's own level comes from the warm
// ACP / IPC per-session record, else PANDO_DELEGATION_DEPTH.
func SessionCanDelegate(sessionID string) bool {
	return engines.CanDelegate(delegationdepth.Resolve(context.Background(), sessionID))
}

// WithoutDelegationSpawnTools returns list minus the spawn/swarm/await tools.
// The input slice is never modified.
func WithoutDelegationSpawnTools(list []BaseTool) []BaseTool {
	out := make([]BaseTool, 0, len(list))
	for _, t := range list {
		if t != nil && IsDelegationSpawnTool(t.Info().Name) {
			continue
		}
		out = append(out, t)
	}
	return out
}
