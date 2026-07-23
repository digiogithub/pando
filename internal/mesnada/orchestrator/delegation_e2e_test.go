package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/store"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// newDelegationOrch builds a real orchestrator backed by a temp FileStore with the
// delegation conclusion protocol enabled. It exercises the genuine
// onTaskComplete -> captureConclusion -> Enrich -> broadcastCompletion pipeline
// (no agent subprocess), which is the heart of the delegation feature.
func newDelegationOrch(t *testing.T, synthesize bool, resolver func(string) (string, string)) *Orchestrator {
	t.Helper()
	fs, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return &Orchestrator{
		store:           fs,
		subscribers:     make(map[string][]chan *models.Task),
		delegation:      DelegationConfig{Enabled: true, SynthesizeFallback: synthesize},
		projectResolver: resolver,
		metrics:         &DelegationMetrics{},
	}
}

func intPtr(v int) *int { return &v }

// TestDelegationE2E_ConclusionBlockCapturedAndBroadcast drives a completed task
// carrying a real <pando:conclusion> block all the way through onTaskComplete and
// asserts (a) the block was parsed, (b) software-owned metadata was filled, and
// (c) the SAME enriched task is delivered on the completion subscription (proving
// capture happens BEFORE broadcast).
func TestDelegationE2E_ConclusionBlockCapturedAndBroadcast(t *testing.T) {
	o := newDelegationOrch(t, false, func(path string) (string, string) {
		return "proj-123", "my-project"
	})
	ch, unsubscribe := o.SubscribeCompletions()
	defer unsubscribe()

	// Real work dir holding the artifact the block cites, so the conclusion is
	// honest and the anti-hallucination gate leaves status=success alone.
	workDir := t.TempDir()
	artifact := filepath.Join(workDir, "internal", "foo", "bar.go")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("package foo"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	exit := 0
	task := &models.Task{
		ID:              "t-e2e-1",
		Engine:          models.EnginePando,
		Model:           "claude-sonnet-4-6",
		WorkDir:         workDir,
		ProjectPath:     "/tmp/work",
		ParentSessionID: "sess-parent",
		CorrelationID:   "corr-e2e-1",
		Status:          models.TaskStatusCompleted,
		ExitCode:        &exit,
		Output: strings.Join([]string{
			"... some agent output ...",
			"<pando:conclusion>",
			"status: success",
			"summary: Implemented the feature and added tests.",
			"artifacts:",
			"  - internal/foo/bar.go",
			"memory_refs:",
			"  - pando/changes/foo.md",
			"follow_up: none",
			"confidence: 0.9",
			"</pando:conclusion>",
		}, "\n"),
	}

	o.onTaskComplete(task)

	// The enriched conclusion must be persisted on the task and re-fetchable.
	stored, err := o.store.Get("t-e2e-1")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.Conclusion == nil {
		t.Fatal("expected conclusion to be captured and persisted")
	}
	if stored.Conclusion.Status != "success" {
		t.Errorf("status = %q, want success", stored.Conclusion.Status)
	}
	if !strings.Contains(stored.Conclusion.Summary, "Implemented the feature") {
		t.Errorf("summary not parsed from block: %q", stored.Conclusion.Summary)
	}
	if stored.Conclusion.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", stored.Conclusion.Confidence)
	}
	if stored.Conclusion.Synthesized {
		t.Error("expected Synthesized=false when a real block was emitted")
	}
	if stored.Conclusion.CapturedAt.IsZero() {
		t.Error("expected CapturedAt to be set (software-owned metadata)")
	}
	// Project metadata is software-owned: resolved from the resolver, not the model.
	if stored.ProjectID != "proj-123" || stored.ProjectName != "my-project" {
		t.Errorf("project metadata = %q/%q, want proj-123/my-project", stored.ProjectID, stored.ProjectName)
	}

	// The broadcast must deliver the SAME task pointer carrying the enriched
	// conclusion (capture-before-broadcast ordering).
	select {
	case got := <-ch:
		if got == nil || got.ID != "t-e2e-1" {
			t.Fatalf("unexpected broadcast task: %#v", got)
		}
		if got.Conclusion == nil || got.Conclusion.Status != "success" {
			t.Fatal("broadcast task did not carry the enriched conclusion")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion broadcast")
	}
}

// TestDelegationE2E_SynthesizeFallbackForMissingBlock verifies that a completed
// task that emitted NO sentinel block gets a synthesized conclusion when the
// fallback is enabled.
func TestDelegationE2E_SynthesizeFallbackForMissingBlock(t *testing.T) {
	o := newDelegationOrch(t, true, nil) // synthesize on, no resolver

	exit := 0
	task := &models.Task{
		ID:              "t-e2e-2",
		Engine:          models.EnginePando,
		ProjectPath:     "/tmp/alpha",
		ParentSessionID: "sess-parent",
		CorrelationID:   "corr-e2e-2",
		Status:          models.TaskStatusCompleted,
		ExitCode:        &exit,
		OutputTail:      "final line of work with no conclusion block",
	}

	o.onTaskComplete(task)

	stored, err := o.store.Get("t-e2e-2")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.Conclusion == nil {
		t.Fatal("expected a synthesized conclusion when fallback is enabled")
	}
	if !stored.Conclusion.Synthesized {
		t.Error("expected Synthesized=true for the fallback path")
	}
	if stored.Conclusion.Status != "success" {
		t.Errorf("status = %q, want success (completed, exit 0)", stored.Conclusion.Status)
	}
	if !strings.Contains(stored.Conclusion.Summary, "final line of work") {
		t.Errorf("summary not derived from output tail: %q", stored.Conclusion.Summary)
	}
	// No resolver: project name derived from the canonical path base.
	if stored.ProjectName != "alpha" {
		t.Errorf("project name = %q, want alpha (derived from path)", stored.ProjectName)
	}
}

// TestDelegationE2E_FailedTaskSynthesizesFailure verifies a failed task with the
// fallback enabled yields a "failed" conclusion summarizing the error.
func TestDelegationE2E_FailedTaskSynthesizesFailure(t *testing.T) {
	o := newDelegationOrch(t, true, nil)

	task := &models.Task{
		ID:              "t-e2e-3",
		Engine:          models.EnginePando,
		ParentSessionID: "sess-parent",
		Status:          models.TaskStatusFailed,
		Error:           "boom: compilation failed",
		ExitCode:        intPtr(1),
	}

	o.onTaskComplete(task)

	stored, err := o.store.Get("t-e2e-3")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.Conclusion == nil {
		t.Fatal("expected a synthesized failure conclusion")
	}
	if stored.Conclusion.Status != "failed" {
		t.Errorf("status = %q, want failed", stored.Conclusion.Status)
	}
	if !strings.Contains(stored.Conclusion.Summary, "boom: compilation failed") {
		t.Errorf("failure summary missing error: %q", stored.Conclusion.Summary)
	}
}

// TestDelegationE2E_NoBlockNoSynthesizeYieldsNoConclusion verifies that with the
// fallback disabled a missing block leaves the task without a conclusion (but the
// completion is still broadcast so the supervisor's gate, not the orchestrator,
// decides to skip it).
func TestDelegationE2E_NoBlockNoSynthesizeYieldsNoConclusion(t *testing.T) {
	o := newDelegationOrch(t, false, nil) // synthesize OFF
	ch, unsubscribe := o.SubscribeCompletions()
	defer unsubscribe()

	task := &models.Task{
		ID:              "t-e2e-4",
		Engine:          models.EnginePando,
		ParentSessionID: "sess-parent",
		Status:          models.TaskStatusCompleted,
		OutputTail:      "no block here",
	}

	o.onTaskComplete(task)

	if task.Conclusion != nil {
		t.Error("expected no conclusion when fallback is off and no block emitted")
	}
	// Completion is still broadcast.
	select {
	case got := <-ch:
		if got == nil || got.ID != "t-e2e-4" {
			t.Fatalf("unexpected broadcast task: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion broadcast")
	}
}

// TestDelegationE2E_DisabledCapturesNothing verifies the default-OFF behavior: with
// delegation disabled, onTaskComplete never captures a conclusion even if a block
// is present (byte-for-byte legacy behavior).
func TestDelegationE2E_DisabledCapturesNothing(t *testing.T) {
	fs, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	o := &Orchestrator{
		store:       fs,
		subscribers: make(map[string][]chan *models.Task),
		delegation:  DelegationConfig{Enabled: false}, // OFF
	}

	task := &models.Task{
		ID:              "t-e2e-5",
		Engine:          models.EnginePando,
		ParentSessionID: "sess-parent",
		Status:          models.TaskStatusCompleted,
		Output:          "<pando:conclusion>\nstatus: success\nsummary: should be ignored\n</pando:conclusion>",
	}

	o.onTaskComplete(task)

	if task.Conclusion != nil {
		t.Error("expected NO conclusion capture when delegation is disabled")
	}
}
