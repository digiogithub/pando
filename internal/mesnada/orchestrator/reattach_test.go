package orchestrator

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// fakeRecoverer is a programmable ExternalDelegationRecoverer for A2 tests.
type fakeRecoverer struct {
	res   *WarmRunResult
	state ExternalRecoveryState
	err   error
	calls int
}

func (f *fakeRecoverer) RecoverExternalDelegation(_ context.Context, _, _, _ string) (*WarmRunResult, ExternalRecoveryState, error) {
	f.calls++
	return f.res, f.state, f.err
}

func staleDelegationTask() *models.Task {
	return &models.Task{
		ID:            "task-reattach",
		Prompt:        "do work",
		Status:        models.TaskStatusRunning,
		Engine:        models.EngineWarmACP,
		ProjectID:     "proj-1",
		ProjectPath:   "/p",
		CorrelationID: "corr-r",
	}
}

// TestIsRecoverableExternalDelegation covers the discriminator: only warm-acp
// tasks with a correlation id and a project reference qualify.
func TestIsRecoverableExternalDelegation(t *testing.T) {
	if !isRecoverableExternalDelegation(staleDelegationTask()) {
		t.Error("warm-acp + corr + path should be recoverable")
	}
	cold := staleDelegationTask()
	cold.Engine = models.Engine("claude")
	if isRecoverableExternalDelegation(cold) {
		t.Error("cold-engine task must not be recoverable")
	}
	noCorr := staleDelegationTask()
	noCorr.CorrelationID = ""
	if isRecoverableExternalDelegation(noCorr) {
		t.Error("missing correlation id must not be recoverable")
	}
	noProj := staleDelegationTask()
	noProj.ProjectID, noProj.ProjectPath = "", ""
	if isRecoverableExternalDelegation(noProj) {
		t.Error("missing project reference must not be recoverable")
	}
}

// TestRecoverExternalDelegationCompleted: a peer that returns a completed result
// drives the task to completed (via onTaskComplete) and bumps the recovered metric.
func TestRecoverExternalDelegationCompleted(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true, SynthesizeFallback: true}, nil)
	o.externalRecoverer = &fakeRecoverer{
		res:   &WarmRunResult{ChildSessionID: "c-1", Output: "recovered\n<pando:conclusion>\nstatus: success\n</pando:conclusion>\n"},
		state: RecoveryCompleted,
	}

	task := staleDelegationTask()
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	o.recoverExternalDelegation(task)

	stored, err := o.store.Get(task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != models.TaskStatusCompleted {
		t.Errorf("status = %q, want completed", stored.Status)
	}
	if stored.ACPSessionID != "c-1" {
		t.Errorf("ACPSessionID = %q, want c-1", stored.ACPSessionID)
	}
	if stored.Conclusion == nil {
		t.Error("expected a captured conclusion on the recovered task")
	}
	if got := o.metrics.Snapshot().ExternalReattachRecovered; got != 1 {
		t.Errorf("recovered metric = %d, want 1", got)
	}
}

// TestRecoverExternalDelegationUnknown: an unrecoverable peer (no record) marks the
// task failed and bumps the failed metric — today's behaviour, just instrumented.
func TestRecoverExternalDelegationUnknown(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, nil)
	o.externalRecoverer = &fakeRecoverer{state: RecoveryUnknown}

	task := staleDelegationTask()
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	o.recoverExternalDelegation(task)

	stored, _ := o.store.Get(task.ID)
	if stored.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed", stored.Status)
	}
	if got := o.metrics.Snapshot().ExternalReattachFailed; got != 1 {
		t.Errorf("failed metric = %d, want 1", got)
	}
}

// TestRecoverStaleTasksColdUnaffected: a stale cold task is marked failed and the
// recoverer is never consulted (only warm-acp delegations are recovery candidates).
func TestRecoverStaleTasksColdUnaffected(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, nil)
	rec := &fakeRecoverer{state: RecoveryCompleted}
	o.externalRecoverer = rec

	cold := &models.Task{ID: "cold-1", Status: models.TaskStatusRunning, Engine: models.Engine("claude")}
	if err := o.store.Save(cold); err != nil {
		t.Fatalf("Save: %v", err)
	}

	o.recoverStaleTasks()

	stored, _ := o.store.Get("cold-1")
	if stored.Status != models.TaskStatusFailed {
		t.Errorf("cold task status = %q, want failed", stored.Status)
	}
	if rec.calls != 0 {
		t.Errorf("recoverer called %d times for a cold task, want 0", rec.calls)
	}
}

// TestRecoverStaleTasksNoRecovererMarksFailed: with no recoverer wired (the
// default) an interrupted warm-acp delegation is marked failed exactly as before.
func TestRecoverStaleTasksNoRecovererMarksFailed(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, nil)
	o.externalRecoverer = nil // default

	task := staleDelegationTask()
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	o.recoverStaleTasks()

	stored, _ := o.store.Get(task.ID)
	if stored.Status != models.TaskStatusFailed {
		t.Errorf("status = %q, want failed (no recoverer)", stored.Status)
	}
}

// TestTryStartWarmBreadcrumbRevertedOnColdFallback: with recovery enabled, a cold
// fallback reverts the in-flight breadcrumb so the cold path sees the original
// (pending, no-engine) task.
func TestTryStartWarmBreadcrumbRevertedOnColdFallback(t *testing.T) {
	resolver := &fakeWarmResolver{err: ErrNoWarmTarget}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)
	o.externalRecoverer = &fakeRecoverer{}

	task := warmTask() // pending, empty engine
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if handled := o.tryStartWarm(task); handled {
		t.Fatal("tryStartWarm returned true, want false (cold fallback)")
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %q, want pending (breadcrumb reverted)", task.Status)
	}
	if task.Engine == models.EngineWarmACP {
		t.Error("engine still warm-acp; breadcrumb not reverted on cold fallback")
	}
	if task.StartedAt != nil {
		t.Error("StartedAt set; breadcrumb not reverted on cold fallback")
	}
}

// TestTryStartWarmBreadcrumbSuccess: with recovery enabled and a warm hit, the
// breadcrumb does not disrupt normal completion.
func TestTryStartWarmBreadcrumbSuccess(t *testing.T) {
	resolver := &fakeWarmResolver{result: &WarmRunResult{ChildSessionID: "c-1", Output: "ok", StopReason: "end_turn"}}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)
	o.externalRecoverer = &fakeRecoverer{}

	task := warmTask()
	if err := o.store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if handled := o.tryStartWarm(task); !handled {
		t.Fatal("tryStartWarm returned false, want true (warm hit)")
	}
	if task.Status != models.TaskStatusCompleted {
		t.Errorf("status = %q, want completed", task.Status)
	}
}
