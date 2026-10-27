package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	delegationdepth "github.com/digiogithub/pando/internal/mesnada/depth"
	"github.com/digiogithub/pando/internal/mesnada/orchestrator"
)

func setMesnadaPolicy(t *testing.T, mutate func(c *config.Config)) {
	t.Helper()
	prev := config.Get()
	c := &config.Config{}
	mutate(c)
	config.SetForTests(c)
	t.Cleanup(func() { config.SetForTests(prev) })
}

func newPolicyTestOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	dir := t.TempDir()
	o, err := orchestrator.New(orchestrator.Config{
		StorePath: filepath.Join(dir, "tasks.json"),
		LogDir:    filepath.Join(dir, "logs"),
	})
	if err != nil {
		t.Fatalf("orchestrator.New: %v", err)
	}
	t.Cleanup(func() { _ = o.Shutdown() })
	return o
}

func engineEnum(t *testing.T, info ToolInfo) []string {
	t.Helper()
	param, ok := info.Parameters["engine"].(map[string]any)
	if !ok {
		t.Fatal("engine parameter missing")
	}
	enum, _ := param["enum"].([]string)
	return enum
}

// The spawn tool's engine enum is rebuilt from live config on every Info()
// call, so a disabled engine disappears from (and reappears in) the schema
// without a restart.
func TestSpawnToolEngineEnumExcludesDisabled(t *testing.T) {
	setMesnadaPolicy(t, func(c *config.Config) {
		c.Mesnada.Orchestrator.DisabledEngines = []string{"claude", "copilot", "pando"}
	})
	tool := NewMesnadaSpawnTool(newPolicyTestOrchestrator(t)).(*MesnadaSpawnTool)

	enum := engineEnum(t, tool.Info())
	joined := "," + strings.Join(enum, ",") + ","
	if strings.Contains(joined, ",claude,") || strings.Contains(joined, ",copilot,") {
		t.Fatalf("disabled engines still offered: %v", enum)
	}
	if !strings.Contains(joined, ",pando,") || !strings.Contains(joined, ",gemini,") {
		t.Fatalf("enabled engines missing: %v", enum)
	}
	desc := tool.Info().Parameters["engine"].(map[string]any)["description"].(string)
	if !strings.Contains(desc, "Disabled in Mesnada settings") {
		t.Fatalf("description should name the disabled engines: %q", desc)
	}

	config.Get().Mesnada.Orchestrator.DisabledEngines = nil
	if enum := engineEnum(t, tool.Info()); !strings.Contains(strings.Join(enum, ","), "claude") {
		t.Fatalf("re-enabled engine not offered again: %v", enum)
	}
}

func runSpawn(t *testing.T, tool BaseTool, ctx context.Context, input map[string]any) ToolResponse {
	t.Helper()
	raw, _ := json.Marshal(input)
	resp, err := tool.Run(ctx, ToolCall{ID: "c1", Input: string(raw)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return resp
}

func TestSpawnToolRefusesPastMaxDepth(t *testing.T) {
	setMesnadaPolicy(t, func(c *config.Config) { c.Mesnada.Delegation.MaxDepth = 1 })
	// This agent is itself a level-1 subagent (cold CLI path: env var).
	t.Setenv(delegationdepth.EnvVar, "1")

	resp := runSpawn(t, &MesnadaSpawnTool{}, context.Background(), map[string]any{"prompt": "do it"})
	if !resp.IsError || !strings.Contains(resp.Content, "delegation nesting limit reached (level 2 > max 1)") {
		t.Fatalf("spawn from level 1 with MaxDepth=1 = %+v, want nesting refusal", resp)
	}
}

func TestSpawnToolRefusesWhenNestedSubagentsDisabled(t *testing.T) {
	off := false
	setMesnadaPolicy(t, func(c *config.Config) {
		c.Mesnada.Delegation.MaxDepth = 5
		c.Mesnada.Delegation.AllowNestedSubagents = &off
	})
	// Warm path: the level comes from the per-session record, not the env.
	t.Setenv(delegationdepth.EnvVar, "")
	delegationdepth.SetSession("warm-child", 1)
	t.Cleanup(func() { delegationdepth.ClearSession("warm-child") })
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "warm-child")

	resp := runSpawn(t, &MesnadaSpawnTool{}, ctx, map[string]any{"prompt": "do it"})
	if !resp.IsError || !strings.Contains(resp.Content, "nested subagents are disabled in Mesnada settings") {
		t.Fatalf("spawn from a warm subagent = %+v, want nested-disabled refusal", resp)
	}

	swarm := runSpawn(t, &MesnadaSwarmTool{}, ctx, map[string]any{"goal": "g", "workers": []map[string]any{{"prompt": "a"}}})
	if !swarm.IsError || !strings.Contains(swarm.Content, "nested subagents are disabled") {
		t.Fatalf("swarm from a warm subagent = %+v, want nested-disabled refusal", swarm)
	}
}

func TestSpawnCorrelationResolvesSessionAndContextDepth(t *testing.T) {
	t.Setenv(delegationdepth.EnvVar, "1")
	dir := t.TempDir()

	delegationdepth.SetSession("s-warm", 2)
	t.Cleanup(func() { delegationdepth.ClearSession("s-warm") })
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s-warm")
	if _, _, _, d := spawnCorrelation(ctx, dir); d != 3 {
		t.Fatalf("warm session depth: got %d, want 3", d)
	}
	// MCP over HTTP: the header lands in the context and wins.
	if _, _, _, d := spawnCorrelation(delegationdepth.WithContext(ctx, 0), dir); d != 1 {
		t.Fatalf("context depth: got %d, want 1", d)
	}
}

func TestDelegationToolHiding(t *testing.T) {
	setMesnadaPolicy(t, func(c *config.Config) { c.Mesnada.Delegation.MaxDepth = 1 })
	t.Setenv(delegationdepth.EnvVar, "")

	if !SessionCanDelegate("main") {
		t.Fatal("the main session (level 0) must keep the spawn tools")
	}
	delegationdepth.SetSession("child", 1)
	t.Cleanup(func() { delegationdepth.ClearSession("child") })
	if SessionCanDelegate("child") {
		t.Fatal("a level-1 session must lose the spawn tools with MaxDepth=1")
	}

	list := []BaseTool{&MesnadaSpawnTool{}, &MesnadaSwarmTool{}, &MesnadaAwaitTool{}, &MesnadaGetTaskTool{}}
	kept := WithoutDelegationSpawnTools(list)
	if len(kept) != 1 || kept[0].Info().Name != mesnadaGetTaskToolName {
		names := []string{}
		for _, k := range kept {
			names = append(names, k.Info().Name)
		}
		t.Fatalf("WithoutDelegationSpawnTools kept %v, want only %s", names, mesnadaGetTaskToolName)
	}
	if len(list) != 4 {
		t.Fatal("input slice must not be modified")
	}
}
