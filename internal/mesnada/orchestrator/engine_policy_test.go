package orchestrator

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/mesnada/engines"
	"github.com/digiogithub/pando/internal/mesnada/store"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func disableEngines(t *testing.T, names ...string) {
	t.Helper()
	prev := config.Get()
	c := &config.Config{}
	c.Mesnada.Orchestrator.DisabledEngines = names
	config.SetForTests(c)
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestSpawnRejectsDisabledEngine(t *testing.T) {
	disableEngines(t, "claude")
	o := newTestOrchestrator(t)
	o.dynamicMCPDir = t.TempDir()
	blockDispatch(t, o)

	_, err := o.Spawn(context.Background(), models.SpawnRequest{Prompt: "x", Engine: models.EngineClaude, Background: true})
	var de *engines.DisabledError
	if !errors.As(err, &de) {
		t.Fatalf("Spawn(claude) = %v, want *engines.DisabledError", err)
	}
	if !strings.Contains(err.Error(), "use engine=pando") {
		t.Fatalf("error should tell the model to use pando: %q", err)
	}

	// A native engine is never refused.
	if _, err := o.Spawn(context.Background(), models.SpawnRequest{Prompt: "x", Engine: models.EnginePando, Background: true}); err != nil {
		t.Fatalf("Spawn(pando): %v", err)
	}
}

func TestSpawnDisabledDefaultEngineFallsBackToPando(t *testing.T) {
	disableEngines(t, "copilot")
	o := newTestOrchestrator(t)
	o.dynamicMCPDir = t.TempDir()
	o.defaultEngine = models.EngineCopilot
	blockDispatch(t, o)

	task, err := o.Spawn(context.Background(), models.SpawnRequest{Prompt: "x", Background: true})
	if err != nil {
		t.Fatalf("Spawn with disabled default: %v", err)
	}
	if task.Engine != models.EnginePando {
		t.Fatalf("engine = %q, want fallback to pando", task.Engine)
	}
}

func TestRelaunchRejectsDisabledEngine(t *testing.T) {
	disableEngines(t, "gemini")
	o := newTestOrchestrator(t)
	o.dynamicMCPDir = t.TempDir()
	prev := &models.Task{ID: "t1", Engine: models.EngineGemini, Status: models.TaskStatusFailed, CreatedAt: time.Now()}
	if err := o.store.Save(prev); err != nil {
		t.Fatal(err)
	}
	_, err := o.Relaunch(context.Background(), "t1", RelaunchOptions{Force: true})
	var de *engines.DisabledError
	if !errors.As(err, &de) {
		t.Fatalf("Relaunch(gemini task) = %v, want *engines.DisabledError", err)
	}
}

func TestCreateSwarmRejectsDisabledRoleBeforeSpawning(t *testing.T) {
	disableEngines(t, "opencode")
	o := newTestOrchestrator(t)
	o.dynamicMCPDir = t.TempDir()
	blockDispatch(t, o)

	before, _ := o.store.List(store.ListFilter{})
	_, err := o.CreateSwarm(context.Background(), SwarmSpec{
		Goal:            "g",
		ParentSessionID: "sess",
		Workers:         []SwarmWorkerSpec{{Prompt: "a"}, {Prompt: "b"}},
		Synthesizer:     SwarmRoleSpec{Engine: "opencode"},
	})
	var de *engines.DisabledError
	if !errors.As(err, &de) {
		t.Fatalf("CreateSwarm = %v, want *engines.DisabledError", err)
	}
	after, _ := o.store.List(store.ListFilter{})
	if len(after) != len(before) {
		t.Fatalf("a rejected swarm must not leave tasks behind (%d -> %d)", len(before), len(after))
	}
}

// The per-task MCP config hands the subagent's level to the "pando" MCP server
// it launches, so spawns made by an external CLI count one level deeper.
func TestSpawnDynamicMCPConfigCarriesDepth(t *testing.T) {
	disableEngines(t)
	o := newTestOrchestrator(t)
	o.dynamicMCPDir = t.TempDir()
	blockDispatch(t, o)

	task, err := o.Spawn(context.Background(), models.SpawnRequest{Prompt: "x", Engine: models.EngineClaude, Background: true, Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(task.MCPConfig)
	if err != nil {
		t.Fatalf("read dynamic MCP config: %v", err)
	}
	if !strings.Contains(string(data), `"PANDO_DELEGATION_DEPTH": "2"`) {
		t.Fatalf("dynamic MCP config lacks the task depth:\n%s", data)
	}
}
