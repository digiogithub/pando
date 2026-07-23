package orchestrator

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestConclusionPassesGate(t *testing.T) {
	cases := []struct {
		name string
		task *models.Task
		want bool
	}{
		{"nil task", nil, false},
		{"no conclusion", &models.Task{}, false},
		{"success", &models.Task{Conclusion: &models.Conclusion{Status: "success"}}, true},
		{"partial", &models.Task{Conclusion: &models.Conclusion{Status: "partial"}}, false},
		{"blocked", &models.Task{Conclusion: &models.Conclusion{Status: "blocked"}}, false},
		{"failed", &models.Task{Conclusion: &models.Conclusion{Status: "failed"}}, false},
	}
	for _, tc := range cases {
		if got := conclusionPasses(tc.task); got != tc.want {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, got)
		}
	}
}

func TestCanStartGateSemantics(t *testing.T) {
	o := newTestOrchestrator(t)

	// Verifier dependency, completed but NOT passing (blocked conclusion).
	verifier := &models.Task{
		ID:         "ver",
		Status:     models.TaskStatusCompleted,
		Conclusion: &models.Conclusion{Status: "blocked", Summary: "missing tests"},
	}
	_ = o.store.Save(verifier)

	synth := &models.Task{
		ID:           "syn",
		Status:       models.TaskStatusPending,
		Dependencies: []string{"ver"},
		GateDeps:     []string{"ver"},
	}

	if o.canStart(synth) {
		t.Fatal("synthesizer must NOT start while gate dep is blocked")
	}

	// Verifier now passes.
	verifier.Conclusion = &models.Conclusion{Status: "success"}
	_ = o.store.Save(verifier)
	if !o.canStart(synth) {
		t.Fatal("synthesizer must start once gate dep passes")
	}

	// A completed dep that is NOT gated is unaffected by its conclusion.
	plain := &models.Task{
		ID:           "syn2",
		Status:       models.TaskStatusPending,
		Dependencies: []string{"ver2"},
	}
	_ = o.store.Save(&models.Task{ID: "ver2", Status: models.TaskStatusCompleted, Conclusion: &models.Conclusion{Status: "blocked"}})
	if !o.canStart(plain) {
		t.Fatal("non-gate dependency should start on completion regardless of conclusion")
	}
}

func TestGateBlocks(t *testing.T) {
	task := &models.Task{ID: "syn", GateDeps: []string{"ver"}}
	blockedVer := &models.Task{ID: "ver", Conclusion: &models.Conclusion{Status: "blocked"}}
	passVer := &models.Task{ID: "ver", Conclusion: &models.Conclusion{Status: "success"}}
	other := &models.Task{ID: "worker"}

	if !gateBlocks(task, blockedVer) {
		t.Error("failed gate dep should block")
	}
	if gateBlocks(task, passVer) {
		t.Error("passing gate dep should not block")
	}
	if gateBlocks(task, other) {
		t.Error("non-gate task should not block")
	}
}

func TestCreateSwarmWiring(t *testing.T) {
	o := newTestOrchestrator(t)
	// Prevent real spawning: workers have no deps and would auto-start. Use a
	// non-existent engine path? Simpler: the store-backed orchestrator's manager is
	// nil, so startTask would panic in a goroutine. Guard by NOT auto-starting:
	// CreateSwarm sets Background=true and workers have no deps → Spawn calls
	// startTask. To keep this a pure wiring test we assert on the created graph via
	// the store immediately; the goroutine may fail spawning but the rows exist.
	o.manager = nil

	created, err := o.CreateSwarm(context.Background(), SwarmSpec{
		Goal:            "build a parser",
		ParentSessionID: "sess1",
		Workers: []SwarmWorkerSpec{
			{Prompt: "write lexer"},
			{Prompt: "write grammar"},
		},
	})
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	if len(created.WorkerIDs) != 2 {
		t.Fatalf("want 2 workers, got %d", len(created.WorkerIDs))
	}

	ver, err := o.store.Get(created.VerifierID)
	if err != nil {
		t.Fatalf("get verifier: %v", err)
	}
	if len(ver.Dependencies) != 2 {
		t.Fatalf("verifier must depend on both workers, got %v", ver.Dependencies)
	}

	syn, err := o.store.Get(created.SynthesizerID)
	if err != nil {
		t.Fatalf("get synthesizer: %v", err)
	}
	if len(syn.Dependencies) != 1 || syn.Dependencies[0] != created.VerifierID {
		t.Fatalf("synthesizer must depend on verifier, got %v", syn.Dependencies)
	}
	if len(syn.GateDeps) != 1 || syn.GateDeps[0] != created.VerifierID {
		t.Fatalf("synthesizer must gate on verifier, got %v", syn.GateDeps)
	}
	// All cards share the swarm id.
	if ver.ParentSessionID != "sess1" || syn.ParentSessionID != "sess1" {
		t.Fatal("swarm cards must share ParentSessionID")
	}
}

func TestCreateSwarmValidation(t *testing.T) {
	o := newTestOrchestrator(t)
	o.manager = nil
	cases := []SwarmSpec{
		{Goal: "", ParentSessionID: "s", Workers: []SwarmWorkerSpec{{Prompt: "x"}}},
		{Goal: "g", ParentSessionID: "s"}, // no workers
		{Goal: "g", ParentSessionID: "", Workers: []SwarmWorkerSpec{{Prompt: "x"}}},
		{Goal: "g", ParentSessionID: "s", Workers: []SwarmWorkerSpec{{Prompt: ""}}},
	}
	for i, spec := range cases {
		if _, err := o.CreateSwarm(context.Background(), spec); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestCreateSwarmIdempotent(t *testing.T) {
	o := newTestOrchestrator(t)
	o.manager = nil
	spec := SwarmSpec{
		Goal:            "goal",
		ParentSessionID: "sessX",
		IdempotencyKey:  "k1",
		Workers:         []SwarmWorkerSpec{{Prompt: "a"}},
	}
	first, err := o.CreateSwarm(context.Background(), spec)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := o.CreateSwarm(context.Background(), spec)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.SynthesizerID != second.SynthesizerID || first.VerifierID != second.VerifierID {
		t.Fatal("idempotent CreateSwarm must return the same topology")
	}
}
