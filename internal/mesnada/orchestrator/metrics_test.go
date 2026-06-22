package orchestrator

import (
	"errors"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// TestDelegationMetricsSnapshotHitRate verifies the derived warm-reuse hit rate
// and that an empty metrics set reports a zero rate (no divide-by-zero).
func TestDelegationMetricsSnapshotHitRate(t *testing.T) {
	var m DelegationMetrics
	if got := m.Snapshot().WarmHitRate; got != 0 {
		t.Fatalf("empty hit rate = %v, want 0", got)
	}

	m.recordWarmAttempt()
	m.recordWarmAttempt()
	m.recordWarmAttempt()
	m.recordWarmAttempt()
	m.recordWarmHit()
	m.recordWarmHit()
	m.recordWarmHit()

	snap := m.Snapshot()
	if snap.WarmAttempts != 4 || snap.WarmHits != 3 {
		t.Fatalf("attempts/hits = %d/%d, want 4/3", snap.WarmAttempts, snap.WarmHits)
	}
	if snap.WarmHitRate != 0.75 {
		t.Fatalf("hit rate = %v, want 0.75", snap.WarmHitRate)
	}
}

// TestDelegationMetricsNilSnapshot guards the nil-receiver path used by the
// orchestrator accessor before metrics is initialised.
func TestDelegationMetricsNilSnapshot(t *testing.T) {
	var m *DelegationMetrics
	if snap := m.Snapshot(); snap != (DelegationMetricsSnapshot{}) {
		t.Fatalf("nil snapshot = %+v, want zero value", snap)
	}
}

// TestTryStartWarmRecordsHit verifies a successful warm run increments the
// warm-attempt and warm-hit counters (and nothing else).
func TestTryStartWarmRecordsHit(t *testing.T) {
	resolver := &fakeWarmResolver{result: &WarmRunResult{ChildSessionID: "c1", Output: "ok", StopReason: "end_turn"}}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true, SynthesizeFallback: true}, resolver)
	task := warmTask()
	_ = o.store.Save(task)

	o.tryStartWarm(task)

	snap := o.DelegationMetrics()
	if snap.WarmAttempts != 1 || snap.WarmHits != 1 {
		t.Fatalf("attempts/hits = %d/%d, want 1/1", snap.WarmAttempts, snap.WarmHits)
	}
	if snap.WarmFailures != 0 || snap.ColdFallbacks != 0 || snap.CapRejections != 0 {
		t.Fatalf("unexpected non-hit counters: %+v", snap)
	}
	if snap.WarmHitRate != 1 {
		t.Fatalf("hit rate = %v, want 1", snap.WarmHitRate)
	}
}

// TestTryStartWarmRecordsFailure verifies a genuine warm-run failure increments
// the warm-failure counter (not a cold fallback).
func TestTryStartWarmRecordsFailure(t *testing.T) {
	resolver := &fakeWarmResolver{err: errors.New("child died")}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)
	task := warmTask()
	_ = o.store.Save(task)

	o.tryStartWarm(task)

	snap := o.DelegationMetrics()
	if snap.WarmAttempts != 1 || snap.WarmFailures != 1 {
		t.Fatalf("attempts/failures = %d/%d, want 1/1", snap.WarmAttempts, snap.WarmFailures)
	}
	if snap.WarmHits != 0 || snap.ColdFallbacks != 0 {
		t.Fatalf("unexpected counters on failure: %+v", snap)
	}
}

// TestTryStartWarmRecordsColdFallback verifies a generic ErrNoWarmTarget counts a
// cold fallback but NOT a cap rejection.
func TestTryStartWarmRecordsColdFallback(t *testing.T) {
	resolver := &fakeWarmResolver{err: ErrNoWarmTarget}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)
	task := warmTask()
	_ = o.store.Save(task)

	o.tryStartWarm(task)

	snap := o.DelegationMetrics()
	if snap.WarmAttempts != 1 || snap.ColdFallbacks != 1 {
		t.Fatalf("attempts/cold = %d/%d, want 1/1", snap.WarmAttempts, snap.ColdFallbacks)
	}
	if snap.CapRejections != 0 {
		t.Fatalf("cap rejections = %d, want 0 for a generic cold fallback", snap.CapRejections)
	}
}

// TestTryStartWarmRecordsCapRejection verifies the cap-specific sentinel counts
// both a cold fallback AND a cap rejection (the cap subset of fallbacks).
func TestTryStartWarmRecordsCapRejection(t *testing.T) {
	resolver := &fakeWarmResolver{err: ErrWarmCapReached}
	o := newWarmOrch(t, DelegationConfig{Enabled: true, ReuseWarmInstances: true}, resolver)
	task := warmTask()
	_ = o.store.Save(task)

	// ErrWarmCapReached must still be treated as a cold fallback (not handled).
	if handled := o.tryStartWarm(task); handled {
		t.Fatal("tryStartWarm returned true for cap-reached, want false (cold fallback)")
	}
	if task.Status != models.TaskStatusPending {
		t.Errorf("status = %q, want pending (untouched)", task.Status)
	}

	snap := o.DelegationMetrics()
	if snap.ColdFallbacks != 1 || snap.CapRejections != 1 {
		t.Fatalf("cold/cap = %d/%d, want 1/1", snap.ColdFallbacks, snap.CapRejections)
	}
}

// TestRecordResurrectionAndInjection verifies the supervisor-facing record
// methods increment their counters.
func TestRecordResurrectionAndInjection(t *testing.T) {
	o := newWarmOrch(t, DelegationConfig{Enabled: true}, nil)
	o.RecordResurrection()
	o.RecordResurrection()
	o.RecordLiveInjection()

	snap := o.DelegationMetrics()
	if snap.Resurrections != 2 || snap.LiveInjections != 1 {
		t.Fatalf("resurrections/injections = %d/%d, want 2/1", snap.Resurrections, snap.LiveInjections)
	}
}
