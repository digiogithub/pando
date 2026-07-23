package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/mesnada/breaker"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestBreakerOptionsApplyDefaults(t *testing.T) {
	o := newTestOrchestrator(t)

	opts := o.breakerOptions()
	if opts.MaxRetries != breaker.DefaultMaxRetries ||
		opts.RateLimitCooldown != breaker.DefaultRateLimitCooldown ||
		opts.RecentSuccessWindow != breaker.DefaultRecentSuccessWindow {
		t.Fatalf("unset config must fall back to package defaults, got %+v", opts)
	}

	o.delegation = DelegationConfig{MaxTaskRetries: 7, RateLimitCooldown: time.Minute, RecentSuccessWindow: time.Second}
	opts = o.breakerOptions()
	if opts.MaxRetries != 7 || opts.RateLimitCooldown != time.Minute || opts.RecentSuccessWindow != time.Second {
		t.Fatalf("configured values must win, got %+v", opts)
	}
}

func TestRecordBreakerOutcomePersists(t *testing.T) {
	o := newTestOrchestrator(t)
	o.delegation = DelegationConfig{MaxTaskRetries: 2}

	task := &models.Task{ID: "t1", Status: models.TaskStatusFailed, Error: "boom"}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	o.recordBreakerOutcome(task)
	stored, err := o.store.Get("t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1 (persisted)", stored.ConsecutiveFailures)
	}
	if stored.LastFailureKind != string(breaker.FailureKindOther) {
		t.Errorf("LastFailureKind = %q, want other", stored.LastFailureKind)
	}

	// Second failure trips the breaker and bumps the metric.
	o.recordBreakerOutcome(task)
	if snap := o.metrics.Snapshot(); snap.BreakerTrips != 1 {
		t.Errorf("BreakerTrips = %d, want 1", snap.BreakerTrips)
	}

	// A disabled breaker records nothing.
	o.delegation.BreakerDisabled = true
	before := task.ConsecutiveFailures
	o.recordBreakerOutcome(task)
	if task.ConsecutiveFailures != before {
		t.Error("a disabled breaker must not touch the counters")
	}
}

func TestRelaunchRefusedByTrippedBreaker(t *testing.T) {
	o := newTestOrchestrator(t)
	o.manager = nil
	o.delegation = DelegationConfig{MaxTaskRetries: 2}

	task := &models.Task{ID: "t1", Status: models.TaskStatusFailed, ConsecutiveFailures: 2}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, err := o.Relaunch(context.Background(), "t1", RelaunchOptions{Background: true})
	if err == nil {
		t.Fatal("relaunch of a tripped task must be refused")
	}
	if !strings.Contains(err.Error(), string(breaker.ReasonBreakerTripped)) {
		t.Errorf("error must name the reason, got %q", err)
	}
	if snap := o.metrics.Snapshot(); snap.RespawnsRefused != 1 {
		t.Errorf("RespawnsRefused = %d, want 1", snap.RespawnsRefused)
	}

	// The task must be untouched by the refusal.
	stored, _ := o.store.Get("t1")
	if stored.Status != models.TaskStatusFailed {
		t.Errorf("a refused relaunch must not reset the task, status=%s", stored.Status)
	}
}

func TestRelaunchForceBypassesGuard(t *testing.T) {
	o := newTestOrchestrator(t)
	o.manager = nil
	o.delegation = DelegationConfig{MaxTaskRetries: 2}

	task := &models.Task{ID: "t1", Status: models.TaskStatusFailed, ConsecutiveFailures: 5}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The guard is asserted directly rather than through Relaunch: Relaunch would
	// go on to start the task in a goroutine that mutates it, and the counter
	// assertion below would then be reading a field that goroutine is writing.
	// TestRelaunchRefusedByTrippedBreaker covers the Relaunch wiring itself.
	if err := o.guardRespawn(task, "relaunch", true); err != nil {
		t.Fatalf("force must bypass the guard: %v", err)
	}
	// The breaker counter survives the bypass: forcing does not forgive history.
	stored, _ := o.store.Get("t1")
	if stored.ConsecutiveFailures != 5 {
		t.Errorf("force must not reset ConsecutiveFailures, got %d", stored.ConsecutiveFailures)
	}
	if got := o.metrics.Snapshot().RespawnsRefused; got != 0 {
		t.Errorf("a forced bypass must not count as a refusal, got %d", got)
	}
}

func TestRespawnDecisionReportsWithoutRespawning(t *testing.T) {
	o := newTestOrchestrator(t)
	o.delegation = DelegationConfig{MaxTaskRetries: 1}

	failedAt := time.Now()
	task := &models.Task{
		ID:              "t1",
		Status:          models.TaskStatusFailed,
		LastFailureKind: string(breaker.FailureKindAuth),
		LastFailureAt:   &failedAt,
	}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	d, err := o.RespawnDecision("t1")
	if err != nil {
		t.Fatalf("RespawnDecision: %v", err)
	}
	if d.Allow || d.Reason != breaker.ReasonBlockerAuth {
		t.Fatalf("want an auth-blocker denial, got allow=%v reason=%q", d.Allow, d.Reason)
	}
	// Reporting a denial is not a refusal event.
	if snap := o.metrics.Snapshot(); snap.RespawnsRefused != 0 {
		t.Errorf("RespawnDecision must not count as a refusal, got %d", snap.RespawnsRefused)
	}

	if _, err := o.RespawnDecision("missing"); err == nil {
		t.Error("an unknown task id must error")
	}
}

func TestCaptureConclusionAppliesHallucinationGate(t *testing.T) {
	o := newTestOrchestrator(t)
	o.delegation = DelegationConfig{Enabled: true}

	dir := t.TempDir()
	real := filepath.Join(dir, "real.go")
	if err := os.WriteFile(real, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	task := &models.Task{
		ID:      "t1",
		Status:  models.TaskStatusCompleted,
		WorkDir: dir,
		Output: "done\n<pando:conclusion>\n" +
			"status: success\n" +
			"summary: implemented everything\n" +
			"artifacts:\n  - real.go\n  - imaginary.go\n" +
			"</pando:conclusion>\n",
	}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	o.captureConclusion(task)
	if task.Conclusion == nil {
		t.Fatal("conclusion must be captured")
	}
	if task.Conclusion.Status != "partial" {
		t.Fatalf("a hallucinated artifact must downgrade success, got %q", task.Conclusion.Status)
	}
	if len(task.Conclusion.Warnings) == 0 || !strings.Contains(strings.Join(task.Conclusion.Warnings, " "), "imaginary.go") {
		t.Errorf("warnings must name the missing artifact, got %v", task.Conclusion.Warnings)
	}
	if snap := o.metrics.Snapshot(); snap.ConclusionGateDowngrades != 1 {
		t.Errorf("ConclusionGateDowngrades = %d, want 1", snap.ConclusionGateDowngrades)
	}

	// The downgrade closes the swarm verifier gate: only "success" passes.
	if conclusionPasses(task) {
		t.Error("a downgraded conclusion must not pass the verifier gate")
	}
}

func TestCaptureConclusionGateCanBeDisabled(t *testing.T) {
	o := newTestOrchestrator(t)
	o.delegation = DelegationConfig{Enabled: true, ConclusionGateDisabled: true}

	task := &models.Task{
		ID:      "t1",
		Status:  models.TaskStatusCompleted,
		WorkDir: t.TempDir(),
		Output: "<pando:conclusion>\n" +
			"status: success\n" +
			"summary: all good\n" +
			"artifacts:\n  - imaginary.go\n" +
			"</pando:conclusion>\n",
	}
	if err := o.store.Save(task); err != nil {
		t.Fatalf("save: %v", err)
	}

	o.captureConclusion(task)
	if task.Conclusion == nil || task.Conclusion.Status != "success" {
		t.Fatalf("a disabled gate must leave the status alone, got %+v", task.Conclusion)
	}
	if snap := o.metrics.Snapshot(); snap.ConclusionGateDowngrades != 0 {
		t.Errorf("a disabled gate must not record downgrades, got %d", snap.ConclusionGateDowngrades)
	}
}
