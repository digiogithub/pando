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
	gotCorr string
	result  *WarmRunResult
	err     error
}

func (f *fakeWarmResolver) RunWarm(_ context.Context, projectID, projectPath, promptText, correlationID string) (*WarmRunResult, error) {
	f.called++
	f.gotID, f.gotPath, f.gotText, f.gotCorr = projectID, projectPath, promptText, correlationID
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

// TestTryStartWarmThreadsCorrelationID: the task's idempotency key reaches the
// resolver so external (IPC) delegation can target a cancel at the right run.
func TestTryStartWarmThreadsCorrelationID(t *testing.T) {
	resolver := &fakeWarmResolver{result: &WarmRunResult{
		ChildSessionID: "child-1",
		Output:         "ok",
		StopReason:     "end_turn",
	}}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)

	task := warmTask()
	task.CorrelationID = "corr-abc-123"
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if handled := o.tryStartWarm(task); !handled {
		t.Fatal("tryStartWarm returned false, want true (warm handled)")
	}
	if resolver.gotCorr != "corr-abc-123" {
		t.Errorf("resolver got correlation id %q, want %q", resolver.gotCorr, "corr-abc-123")
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

// fakeRefResolver is a programmable ProjectRefResolver for accessor tests.
type fakeRefResolver struct {
	ref  ProjectRef
	ok   bool
	list []ProjectRef
}

func (f *fakeRefResolver) ResolveProjectRef(_ context.Context, _ string) (ProjectRef, bool) {
	return f.ref, f.ok
}
func (f *fakeRefResolver) ListProjectRefs(_ context.Context) []ProjectRef { return f.list }

// TestProjectRefAccessorsNil: with no resolver wired, the accessors degrade to
// "unsupported / empty" rather than panicking. Constructs the struct directly to
// avoid New()'s background store writers (which race t.TempDir cleanup).
func TestProjectRefAccessorsNil(t *testing.T) {
	o := &Orchestrator{}
	if o.ProjectRefsSupported() {
		t.Error("ProjectRefsSupported = true, want false with no resolver")
	}
	if _, ok := o.ResolveProjectRef(context.Background(), "x"); ok {
		t.Error("ResolveProjectRef ok = true, want false with no resolver")
	}
	if refs := o.ListProjectRefs(context.Background()); refs != nil {
		t.Errorf("ListProjectRefs = %v, want nil with no resolver", refs)
	}
}

// TestProjectRefAccessorsWired: the accessors delegate to the injected resolver.
func TestProjectRefAccessorsWired(t *testing.T) {
	rr := &fakeRefResolver{
		ref:  ProjectRef{ID: "p1", Name: "Alpha", Path: "/a"},
		ok:   true,
		list: []ProjectRef{{ID: "p1", Name: "Alpha", Path: "/a"}},
	}
	o := &Orchestrator{projectRefResolver: rr}

	if !o.ProjectRefsSupported() {
		t.Fatal("ProjectRefsSupported = false, want true")
	}
	ref, ok := o.ResolveProjectRef(context.Background(), "Alpha")
	if !ok || ref.ID != "p1" {
		t.Fatalf("ResolveProjectRef = (%+v, %v), want p1/true", ref, ok)
	}
	if refs := o.ListProjectRefs(context.Background()); len(refs) != 1 || refs[0].ID != "p1" {
		t.Fatalf("ListProjectRefs = %+v, want one p1", refs)
	}
}
