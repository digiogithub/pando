---
created_at: 2026-06-22T12:18:15.260974032Z
updated_at: 2026-06-22T12:18:35.325263133Z
tags:
    - change
    - mesnada
    - delegation
    - metrics
    - observability
    - orchestrator
    - telemetry
    - i18n
---
# Change: Delegation Metrics & Panel Telemetry (E1)

Date: 2026-06-22. Backlog item E1 from pando/plans/delegation_future_improvements.md. Read-only telemetry (no config knob); default behaviour unchanged — counters stay at zero until delegation does something. See feature pando/features/delegated_conclusions_resurrection.md.

## Counters (monotonic, process-lifetime)
warm_attempts, warm_hits, warm_failures, cold_fallbacks (includes cap_rejections), cap_rejections (subset of cold_fallbacks caused by the per-instance concurrency cap + full warm queue), resurrections (Case B), live_injections (Case A, incl. resume-race fallback), warm_hit_rate (derived warm_hits/warm_attempts in [0,1]; 0 when no attempts).

## Design
- internal/mesnada/orchestrator/metrics.go (NEW): DelegationMetrics with lock-free atomic.Int64 counters + DelegationMetricsSnapshot (JSON + derived hit rate) + Snapshot() (nil-safe) + unexported record helpers.
- Orchestrator owns metrics *DelegationMetrics (init in New). Public DelegationMetrics() accessor; public RecordResurrection()/RecordLiveInjection() for the supervisor (lives in internal/app, only holds the concrete orchestrator).
- Cap attribution: new sentinel orchestrator.ErrWarmCapReached WRAPS ErrNoWarmTarget (existing errors.Is(err, ErrNoWarmTarget) cold-path checks keep working). app makeWarmTargetResolver maps project.ErrWarmCapReached → orchestrator.ErrWarmCapReached; other reasons → generic ErrNoWarmTarget. tryStartWarm: ErrNoWarmTarget → recordColdFallback (+ recordCapRejection when also Is ErrWarmCapReached); success → recordWarmHit; genuine error → recordWarmFailure; every eligible attempt → recordWarmAttempt.
- Supervisor (internal/app/delegation_supervisor.go): RecordLiveInjection() on successful injectLive and on resume-race fallback injection in flushWith; RecordResurrection() on successful Resume.

## Files
metrics.go (NEW) + metrics_test.go (NEW); orchestrator.go (field+init+3 methods); warm.go (sentinel + recording in tryStartWarm); app/app.go (adapter cap mapping); app/delegation_supervisor.go (record calls); api/handlers_orchestrator.go + routes.go (GET /api/v1/orchestrator/delegation/metrics); tui/page/orchestrator.go (one-line metrics summary in dashboard header, only when activity; paneHeights + mouse hit-test account for the extra line); web-ui types/index.ts (DelegationMetrics), stores/orchestratorStore.ts (delegationMetrics + fetchDelegationMetrics, polled with tasks), components/orchestrator/OrchestratorView.tsx (DelegationMetricsBar strip, i18n via t('orchestrator.delegationMetrics.*')); README.md warm bullet extended.

## Follow-ups (review feedback) — pando/fixes/delegation_e1_followups_races_i18n.md
- WebUI strings NOT hardcoded: DelegationMetricsBar uses useTranslation() + t('orchestrator.delegationMetrics.*'); new namespace (warmHitRate/warmHits/warmFailures/coldFallbacks/capRejections/resurrections/liveInjections/title) in all 7 locales.
- Fixed the -race failures (previously called pre-existing flakes): FileStore.Close now wg.Wait()s its background saver → no TempDir teardown race (TestTryStartWarmGating); ACP a.conn read via new getConn() under sessionsMu (sendAvailableCommandsUpdate); terminal outputBuf → self-locking lockedBuffer (exec writer vs TerminalOutput reader); test syncBuffer for the async-logger test.

## Verification
go build ./... + gofmt -l clean. metrics_test.go: snapshot hit-rate + nil-safe; tryStartWarm records hit/failure/cold/cap (cap still cold-falls-back, task untouched); record resurrection/injection. Suites green incl. go test -race ./internal/mesnada/... ./internal/project (acp, store, orchestrator, project all race-clean now); WebUI tsc clean; all 7 locale JSONs valid.