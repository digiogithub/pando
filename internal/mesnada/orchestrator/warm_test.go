package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeWarmResolver is a programmable WarmTargetResolver for routing tests.
type fakeWarmResolver struct {
	called  int
	gotID   string
	gotPath string
	gotText string
	result  *WarmRunResult
	err     error
}

func (f *fakeWarmResolver) RunWarm(_ context.Context, projectID, projectPath, promptText string) (*WarmRunResult, error) {
	f.called++
	f.gotID, f.gotPath, f.gotText = projectID, projectPath, promptText
	return f.result, f.err
}

func newWarmOrch(t *testing.T, del DelegationConfig, resolver WarmTargetResolver) *Orchestrator {
	t.Helper()
	o, err := New(Config{
		StorePath:   filepath.Join(t.TempDir(), "tasks.json"),
		MaxParallel: 2,
		Delegation:  del,
		// PersonaPath empty: persona manager loads built-ins only.
		WarmTargetResolver: resolver,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = o.Shutdown() })
	return o
}

func warmTask() *models.Task {
	return &models.Task{
		ID:        "task-warm",
		Prompt:    "do the work",
		Status:    models.TaskStatusPending,
		ProjectID: "proj-1",
	}
}

// TestTryStartWarmSuccess: a resolved warm target → terminal completed task with
// engine=warm-acp, captured output and child session id, resolver called once.
func TestTryStartWarmSuccess(t *testing.T) {
	resolver := &fakeWarmResolver{result: &WarmRunResult{
		ChildSessionID: "child-1",
		Output:         "...\n<pando:conclusion>\nstatus: success\n</pando:conclusion>\n",
		StopReason:     "end_turn",
	}}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true, SynthesizeFallback: true}, resolver)

	task := warmTask()
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if handled := o.tryStartWarm(task); !handled {
		t.Fatal("tryStartWarm returned false, want true (warm handled)")
	}
	if resolver.called != 1 {
		t.Fatalf("resolver called %d times, want 1", resolver.called)
	}
	if task.Engine != models.EngineWarmACP {
		t.Errorf("engine = %q, want warm-acp", task.Engine)
	}
	if task.Status != models.TaskStatusCompleted {
		t.Errorf("status = %q, want completed", task.Status)
	}
	if task.ACPSessionID != "child-1" {
		t.Errorf("ACPSessionID = %q, want child-1", task.ACPSessionID)
	}
	if task.Conclusion == nil {
		t.Error("expected a captured conclusion on the completed warm task")
	}

	// Persisted state reflects completion.
	stored, err := o.store.Get(task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != models.TaskStatusCompleted {
		t.Errorf("stored status = %q, want completed", stored.Status)
	}
}

// TestTryStartWarmRunFailureIsTerminal: a genuine warm-run error → terminal
// failed task (the supervisor can still re-enter the parent loop).
func TestTryStartWarmRunFailureIsTerminal(t *testing.T) {
	resolver := &fakeWarmResolver{err: errors.New("child died mid-run")}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true, SynthesizeFallback: true}, resolver)

	task := warmTask()
	_ = o.store.Save(task)

	if handled := o.tryStartWarm(task); !handled {
		t.Fatal("tryStartWarm returned false, want true (warm failure is still handled)")
	}
	if task.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed", task.Status)
	}
	if task.Error == "" {
		t.Error("expected an error message on the failed warm task")
	}
}

// TestTryStartWarmNoTargetFallsBack: ErrNoWarmTarget → cold path, task untouched.
func TestTryStartWarmNoTargetFallsBack(t *testing.T) {
	resolver := &fakeWarmResolver{err: ErrNoWarmTarget}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)

	task := warmTask()
	_ = o.store.Save(task)

	if handled := o.tryStartWarm(task); handled {
		t.Fatal("tryStartWarm returned true, want false (cold fallback)")
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %q, want pending (untouched)", task.Status)
	}
	if task.Engine == models.EngineWarmACP {
		t.Error("engine must not be mutated when falling back to cold path")
	}
}

// TestTryStartWarmGating: warm routing is skipped (returns false, resolver not
// called) when disabled, when the master flag is off, or when no project is set.
func TestTryStartWarmGating(t *testing.T) {
	cases := []struct {
		name string
		del  DelegationConfig
		task *models.Task
	}{
		{"delegation disabled", DelegationConfig{Enabled: false, ReuseWarmInstances: true}, warmTask()},
		{"reuse flag off", DelegationConfig{Enabled: true, ReuseWarmInstances: false}, warmTask()},
		{"no project", DelegationConfig{Enabled: true, ReuseWarmInstances: true}, &models.Task{ID: "t", Status: models.TaskStatusPending}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeWarmResolver{result: &WarmRunResult{}}
			o := newWarmOrch(t, tc.del, resolver)
			_ = o.store.Save(tc.task)
			if handled := o.tryStartWarm(tc.task); handled {
				t.Fatal("tryStartWarm returned true, want false (gated)")
			}
			if resolver.called != 0 {
				t.Errorf("resolver called %d times, want 0", resolver.called)
			}
		})
	}
}

// TestTryStartWarmNilResolver: no resolver wired → cold path even with flags on.
func TestTryStartWarmNilResolver(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, nil)
	task := warmTask()
	_ = o.store.Save(task)
	if handled := o.tryStartWarm(task); handled {
		t.Fatal("tryStartWarm returned true with nil resolver, want false")
	}
}
