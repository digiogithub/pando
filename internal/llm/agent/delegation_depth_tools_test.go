package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/mesnada/depth"
)

// A session already at the delegation nesting cap does not see the
// spawn/swarm/await tools; the main session keeps them. Evaluated per request,
// so it follows the live config.
func TestToolsForSessionHidesSpawnToolsAtNestingCap(t *testing.T) {
	prev := config.Get()
	c := &config.Config{}
	c.Mesnada.Delegation.MaxDepth = 1
	config.SetForTests(c)
	t.Cleanup(func() { config.SetForTests(prev) })
	t.Setenv(depth.EnvVar, "")

	a := &agent{tools: []tools.BaseTool{
		namedTool{"bash"}, namedTool{"mesnada_spawn_agent"}, namedTool{"mesnada_swarm"}, namedTool{"mesnada_await"}, namedTool{"mesnada_get_task"},
	}}
	names := func(sessionID string) map[string]bool {
		out := map[string]bool{}
		for _, tl := range a.toolsForSession(sessionID) {
			out[tl.Info().Name] = true
		}
		return out
	}

	if got := names("main"); !got["mesnada_spawn_agent"] || !got["mesnada_swarm"] || !got["mesnada_await"] {
		t.Fatalf("main session lost spawn tools: %v", got)
	}

	depth.SetSession("sub", 1)
	t.Cleanup(func() { depth.ClearSession("sub") })
	got := names("sub")
	if got["mesnada_spawn_agent"] || got["mesnada_swarm"] || got["mesnada_await"] {
		t.Fatalf("subagent at the cap still sees spawn tools: %v", got)
	}
	if !got["bash"] || !got["mesnada_get_task"] {
		t.Fatalf("other tools must stay: %v", got)
	}

	// Raising the cap live gives the tools back.
	config.Get().Mesnada.Delegation.MaxDepth = 2
	if got := names("sub"); !got["mesnada_spawn_agent"] {
		t.Fatalf("raised cap should restore spawn tools: %v", got)
	}
}
