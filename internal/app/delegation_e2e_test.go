package app

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// enrichedTask builds a completed task carrying a real <pando:conclusion> block
// and runs it through the PRODUCTION conclusion.Enrich path (the same enrichment
// the orchestrator applies in onTaskComplete). The returned task is exactly what a
// completion subscriber (the supervisor) receives, so feeding it to the supervisor
// is a genuine cross-package end-to-end of the conclusion -> re-entry contract.
func enrichedTask(t *testing.T, id, parent, corr string) *models.Task {
	t.Helper()
	exit := 0
	task := &models.Task{
		ID:              id,
		Engine:          models.EnginePando,
		Model:           "claude-sonnet-4-6",
		WorkDir:         "/tmp/proj",
		ProjectPath:     "/tmp/proj",
		ParentSessionID: parent,
		CorrelationID:   corr,
		Status:          models.TaskStatusCompleted,
		ExitCode:        &exit,
		Output: strings.Join([]string{
			"<pando:conclusion>",
			"status: success",
			"summary: Did the delegated work and wrote a test.",
			"confidence: 0.8",
			"</pando:conclusion>",
		}, "\n"),
	}
	c := conclusion.Enrich(task, conclusion.DelegationOptions{SynthesizeFallback: true}, nil)
	if c == nil {
		t.Fatal("Enrich returned nil for a task with a conclusion block")
	}
	task.Conclusion = c
	return task
}

// TestDelegationE2E_CaseA_LiveInjection wires the real supervisor decision logic to
// a production-enriched task: a busy parent must receive the conclusion injected as
// the FormatForParent rendering (pointers, not dumps).
func TestDelegationE2E_CaseA_LiveInjection(t *testing.T) {
	inj := newFakeInjector()
	inj.busy["sess-A"] = true
	s := newSup(inj, true, true) // Enabled + InjectIntoLiveLoop

	task := enrichedTask(t, "task-A", "sess-A", "corr-A")
	if !s.handleCompletion(task) {
		t.Fatal("expected live injection for a busy parent")
	}
	if inj.count() != 1 {
		t.Fatalf("expected exactly 1 injection, got %d", inj.count())
	}
	// The injected content is the production FormatForParent rendering of the task
	// (using the same resolver the supervisor uses).
	want := conclusion.FormatForParent(task, supervisorResolveOptions(task))
	if len(inj.injectArgs) != 1 || inj.injectArgs[0] != want {
		t.Fatalf("injected content mismatch.\n got: %q\nwant: %q", inj.injectArgs, want)
	}
	if !strings.Contains(want, "status=success") || !strings.Contains(want, "Did the delegated work") {
		t.Fatalf("formatted conclusion missing expected fields: %q", want)
	}
}

// TestDelegationE2E_CaseB_IdleResurrection wires the real supervisor to a
// production-enriched task with an idle parent and no outstanding siblings: the
// parent loop must be resurrected once, with the conclusion in the resume content.
func TestDelegationE2E_CaseB_IdleResurrection(t *testing.T) {
	inj := newFakeInjector() // sess-B idle
	lister := &fakeLister{}  // no outstanding siblings
	s := newResurrectSup(inj, lister, resurrectOpts())

	task := enrichedTask(t, "task-B", "sess-B", "corr-B")
	if !s.handleCompletion(task) {
		t.Fatal("expected resurrection for an idle parent")
	}
	if inj.resumeCount() != 1 {
		t.Fatalf("expected exactly 1 resume, got %d", inj.resumeCount())
	}
	content := inj.lastResumeContent()
	if !strings.Contains(content, "resuming because") {
		t.Errorf("resume content missing resurrection framing: %q", content)
	}
	if !strings.Contains(content, "Did the delegated work") {
		t.Errorf("resume content missing the delegated conclusion summary: %q", content)
	}
}

// TestDelegationE2E_CapsRespected confirms the caps are enforced on the real
// production-enriched task: depth cap and resurrection budget both block re-entry.
func TestDelegationE2E_CapsRespected(t *testing.T) {
	t.Run("depth cap", func(t *testing.T) {
		inj := newFakeInjector()
		opts := resurrectOpts()
		opts.MaxDepth = 2
		s := newResurrectSup(inj, &fakeLister{}, opts)

		task := enrichedTask(t, "task-C", "sess-C", "corr-C")
		task.Depth = 2 // >= MaxDepth
		if s.handleCompletion(task) {
			t.Fatal("expected depth cap to block resurrection")
		}
		if inj.resumeCount() != 0 {
			t.Fatalf("expected 0 resumes, got %d", inj.resumeCount())
		}
	})

	t.Run("budget cap", func(t *testing.T) {
		inj := newFakeInjector()
		inj.resCount = 4 // already at MaxResurrections
		s := newResurrectSup(inj, &fakeLister{}, resurrectOpts())

		task := enrichedTask(t, "task-D", "sess-D", "corr-D")
		if s.handleCompletion(task) {
			t.Fatal("expected budget cap to block resurrection")
		}
		if inj.resumeCount() != 0 {
			t.Fatalf("expected 0 resumes when budget exhausted, got %d", inj.resumeCount())
		}
	})
}
