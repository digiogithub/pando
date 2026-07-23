package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/mesnada/events"
	"github.com/digiogithub/pando/internal/mesnada/store"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func newTestOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewFileStore(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bb, err := NewBlackboard(filepath.Join(dir, "blackboard.json"))
	if err != nil {
		t.Fatalf("blackboard: %v", err)
	}
	evLog, err := events.Open(filepath.Join(dir, "events.jsonl"), 0)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	return &Orchestrator{
		store:      st,
		blackboard: bb,
		eventLog:   evLog,
		instanceID: "orch-test",
		metrics:    &DelegationMetrics{},
	}
}

func TestGetDependencyConclusionsForwardsStructuredResult(t *testing.T) {
	o := newTestOrchestrator(t)
	dep := &models.Task{
		ID:     "dep1",
		Status: models.TaskStatusCompleted,
		Conclusion: &models.Conclusion{
			Status:     "success",
			Summary:    "Implemented the parser.",
			Artifacts:  []string{"parser.go"},
			MemoryRefs: []string{"proj.parser"},
			FollowUp:   "Wire it into the CLI.",
		},
	}
	if err := o.store.Save(dep); err != nil {
		t.Fatalf("save dep: %v", err)
	}

	out := o.getDependencyConclusions([]string{"dep1"})
	for _, want := range []string{"DEPENDENCY CONCLUSIONS", "success", "Implemented the parser.", "parser.go", "proj.parser", "Wire it into the CLI."} {
		if !strings.Contains(out, want) {
			t.Fatalf("conclusion block missing %q\n%s", want, out)
		}
	}

	// A dependency with no captured conclusion contributes nothing.
	if o.getDependencyConclusions([]string{"missing"}) != "" {
		t.Fatal("expected empty block for unknown dep")
	}
}

func TestBuildSwarmContextCombinesConclusionAndBlackboard(t *testing.T) {
	o := newTestOrchestrator(t)
	dep := &models.Task{
		ID:         "dep1",
		Status:     models.TaskStatusCompleted,
		Conclusion: &models.Conclusion{Status: "success", Summary: "done"},
	}
	_ = o.store.Save(dep)
	_ = o.blackboard.Post("sessX", BlackboardEntry{Key: "api", Value: json.RawMessage(`"v2"`), Author: "sibling"})

	task := &models.Task{
		ID:              "child",
		ParentSessionID: "sessX",
		Dependencies:    []string{"dep1"},
	}
	block := o.buildSwarmContext(task)
	for _, want := range []string{swarmContextMarker, "DEPENDENCY CONCLUSIONS", "SWARM BLACKBOARD", "sessX", "mesnada_note", "api", "v2"} {
		if !strings.Contains(block, want) {
			t.Fatalf("swarm context missing %q\n%s", want, block)
		}
	}
}

func TestBuildSwarmContextEmptyWhenNoCorrelationNoDeps(t *testing.T) {
	o := newTestOrchestrator(t)
	if got := o.buildSwarmContext(&models.Task{ID: "solo"}); got != "" {
		t.Fatalf("want empty context, got %q", got)
	}
}
