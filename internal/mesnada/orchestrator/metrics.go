package orchestrator

import "sync/atomic"

// DelegationMetrics holds process-lifetime counters describing how delegated
// tasks were routed and how the parent loop re-entered after their completion
// (item E1). All counters are monotonic and lock-free (atomic), so recording a
// metric from the hot delegation path never contends with a reader taking a
// Snapshot. The zero value is ready to use; the orchestrator owns one instance
// for its whole lifetime.
//
// The counters are intentionally orchestrator-global rather than per-project:
// they answer "is warm reuse paying off and is resurrection firing as expected"
// across the whole instance, which is the question the panel telemetry surfaces.
type DelegationMetrics struct {
	// warmAttempts counts delegated tasks that were eligible for warm routing and
	// for which a warm run was attempted (tryStartWarm called RunWarm).
	warmAttempts atomic.Int64
	// warmHits counts warm attempts that were served by a warm instance and
	// completed successfully (no cold spawn).
	warmHits atomic.Int64
	// externalHits counts the subset of warmHits served by an EXTERNAL
	// (editor-launched) peer over the IPC bus (hot-peer delegation, B3) rather than
	// a manager-spawned warm child. External run failures fold into warmFailures and
	// refused/unreachable peers fold into coldFallbacks, like any other warm miss.
	externalHits atomic.Int64
	// warmFailures counts warm attempts that reached a warm instance but failed the
	// run (terminal-failed task, still no cold spawn).
	warmFailures atomic.Int64
	// coldFallbacks counts warm attempts that found no warm target and fell back to
	// the cold subprocess path (includes capRejections).
	coldFallbacks atomic.Int64
	// capRejections counts the subset of coldFallbacks caused specifically by the
	// per-instance concurrency cap being reached (and the warm queue, if any, full).
	capRejections atomic.Int64
	// resurrections counts idle parent loops that were resurrected (Case B) after
	// delegated siblings completed.
	resurrections atomic.Int64
	// liveInjections counts conclusions injected into a still-running parent loop
	// (Case A), including resume-race fallback injections.
	liveInjections atomic.Int64
	// externalReattachRecovered counts interrupted external delegations whose result
	// was recovered from the surviving peer after a parent restart (A2) instead of
	// being marked failed.
	externalReattachRecovered atomic.Int64
	// externalReattachFailed counts interrupted external delegations that could NOT
	// be recovered on restart (peer gone / no record / still running past the
	// recovery window) and were therefore marked failed.
	externalReattachFailed atomic.Int64
}

// DelegationMetricsSnapshot is an immutable, JSON-friendly view of
// DelegationMetrics taken at one instant, plus the derived warm-reuse hit rate.
type DelegationMetricsSnapshot struct {
	WarmAttempts   int64 `json:"warm_attempts"`
	WarmHits       int64 `json:"warm_hits"`
	ExternalHits   int64 `json:"external_hits"`
	WarmFailures   int64 `json:"warm_failures"`
	ColdFallbacks  int64 `json:"cold_fallbacks"`
	CapRejections  int64 `json:"cap_rejections"`
	Resurrections  int64 `json:"resurrections"`
	LiveInjections int64 `json:"live_injections"`
	// ExternalReattachRecovered / ExternalReattachFailed count cross-restart
	// external delegation recovery outcomes (A2).
	ExternalReattachRecovered int64 `json:"external_reattach_recovered"`
	ExternalReattachFailed    int64 `json:"external_reattach_failed"`
	// WarmHitRate is warmHits / warmAttempts in [0,1]; 0 when there were no
	// attempts. It is the headline "is warm reuse paying off" number.
	WarmHitRate float64 `json:"warm_hit_rate"`
}

func (m *DelegationMetrics) recordWarmAttempt()   { m.warmAttempts.Add(1) }
func (m *DelegationMetrics) recordWarmHit()       { m.warmHits.Add(1) }
func (m *DelegationMetrics) recordExternalHit()   { m.externalHits.Add(1) }
func (m *DelegationMetrics) recordWarmFailure()   { m.warmFailures.Add(1) }
func (m *DelegationMetrics) recordColdFallback()  { m.coldFallbacks.Add(1) }
func (m *DelegationMetrics) recordCapRejection()  { m.capRejections.Add(1) }
func (m *DelegationMetrics) recordResurrection()  { m.resurrections.Add(1) }
func (m *DelegationMetrics) recordLiveInjection() { m.liveInjections.Add(1) }

func (m *DelegationMetrics) recordExternalReattachRecovered() { m.externalReattachRecovered.Add(1) }
func (m *DelegationMetrics) recordExternalReattachFailed()    { m.externalReattachFailed.Add(1) }

// Snapshot returns a consistent-enough point-in-time copy of the counters with
// the derived hit rate. Counters are read independently (no global lock), so a
// snapshot taken during concurrent updates may mix values from adjacent instants;
// this is acceptable for telemetry and never blocks the delegation path.
func (m *DelegationMetrics) Snapshot() DelegationMetricsSnapshot {
	if m == nil {
		return DelegationMetricsSnapshot{}
	}
	attempts := m.warmAttempts.Load()
	hits := m.warmHits.Load()
	snap := DelegationMetricsSnapshot{
		WarmAttempts:              attempts,
		WarmHits:                  hits,
		ExternalHits:              m.externalHits.Load(),
		WarmFailures:              m.warmFailures.Load(),
		ColdFallbacks:             m.coldFallbacks.Load(),
		CapRejections:             m.capRejections.Load(),
		Resurrections:             m.resurrections.Load(),
		LiveInjections:            m.liveInjections.Load(),
		ExternalReattachRecovered: m.externalReattachRecovered.Load(),
		ExternalReattachFailed:    m.externalReattachFailed.Load(),
	}
	if attempts > 0 {
		snap.WarmHitRate = float64(hits) / float64(attempts)
	}
	return snap
}
