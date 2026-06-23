---
created_at: 2026-06-23T14:33:04.87053618Z
updated_at: 2026-06-23T14:33:04.87053618Z
tags:
    - change
    - mesnada
    - delegation
    - a2
    - external
    - ipc
    - recovery
    - reattach
    - restart
    - orchestrator
    - metrics
---
# Change: A2 — re-attach external delegations after a parent restart

Date: 2026-06-23. Implements the last open delegation backlog item
(`pando/plans/delegation_future_improvements.md` A2). Plan:
`/home/sevir/.claude/plans/expressive-watching-narwhal.md`. Default-OFF: gated on
the existing caller opt-in `AllowExternalWarmTargets`; when off, behaviour is
byte-identical to before (interrupted tasks marked failed).

## Problem
On a parent restart, `recoverStaleTasks` marked every still-`running` task failed.
Correct for manager-spawned warm children (stdio subprocesses that die with the
parent), but external (editor-launched) peers SURVIVE the parent and have usually
already finished the delegation by the time it restarts (seconds). Two gaps blocked
recovery: (1) the synchronous warm path persisted no in-flight breadcrumb; (2) the
B3 `delegation.run` is blocking and the peer dropped its CorrelationID→session
mapping on return, so there was no way to ask "what happened to correlation X".

## Approach: recover completed results
The peer retains recent completed results and answers a new `delegation.status`
query by correlation id; on restart the parent queries the surviving peer and
recovers a completed result through the normal conclusion → supervisor pipeline
(resurrect/inject), instead of failing it.

### Phase 1 — Protocol (`internal/ipc/protocol/rpc.go`)
`MethodDelegationStatus = "delegation.status"`; `DelegationStatusParams{CorrelationID}`;
`DelegationStatusResult{State, SessionID, Output, StopReason}` with state consts
`DelegationStateRunning/Completed/Unknown`. Bumped `DelegationProtocolVersion` 1→2
(status RPC is the v2 capability; re-attach requires peer `DelegationProtocol >= 2`,
fails closed for v1).

### Phase 2 — Bridge target (`internal/ipc/bridge/`)
`agentDelegationRunner` gained a bounded result cache (`results map[corrID]entry`,
`delegationResultCacheMax=64` / `delegationResultTTL=30m`, oldest+expired eviction,
test-overridable `nowFn`). `RunDelegation` stores the result on success BEFORE the
inflight-delete defer (no running→unknown gap). New `DelegationStatus(corrID)`:
inflight→running, cache→completed+payload, else→unknown. `DelegationRunner` interface
+1 method; consent-gated `delegation.status` handler in `handlers.go`.

### Phase 3 — Project caller (`internal/project/delegate_external.go`)
`Manager.RecoverExternalDelegation(ctx, projectID, projectPath, correlationID) (*DelegateResult, state string, error)`
reuses the `DelegateExternal` plumbing (`ipc.ReadLockForPath`+`pidIsAlive`→`ErrExternalUnreachable`;
`instance.ping` requiring `AcceptsDelegations && DelegationProtocol>=2` else
`ErrExternalDelegationRefused`) then calls `delegation.status`. Read-only; never re-runs.

### Phase 4 — Orchestrator (`internal/mesnada/orchestrator/`)
- `warm.go`: `ExternalRecoveryState` enum (Unknown/Completed/Running) +
  `ExternalDelegationRecoverer` interface (injected, avoids import cycle).
- `tryStartWarm`: when recovery enabled, persists a running warm-acp breadcrumb
  before the blocking `RunWarm`; reverts it on cold fallback (cold path unchanged).
- `recoverStaleTasks`: stale running tasks that are `isRecoverableExternalDelegation`
  (engine=warm-acp + CorrelationID + project ref) AND a recoverer is wired are
  recovered in a background goroutine (`recoverExternalDelegation`): completed →
  `completeRecoveredDelegation` → `onTaskComplete` (conclusion+broadcast); running →
  `pollExternalRecovery` (bounded, 2m/5s) then recover/fail; unknown/unreachable →
  `markStaleTaskFailed` (extracted helper, pre-A2 behaviour). Non-delegation tasks
  keep the exact mark-failed path.
- `metrics.go`: lock-free `externalReattachRecovered`/`externalReattachFailed`
  counters + snapshot fields (E1 pattern). No config knob.

### Phase 5 — App wiring (`internal/app/app.go`)
`externalDelegationRecovererFunc` + `makeExternalDelegationRecoverer(mgr)` adapter:
maps `protocol.DelegationState*` → `ExternalRecoveryState`, `*project.DelegateResult`
→ `*WarmRunResult`, errors → RecoveryUnknown (fail). Wired into
`mesnadaCfg.ExternalDelegationRecoverer` ONLY when `AllowExternalWarmTargets` (reuses
the existing two-sided opt-in; no new config field).

### Phase 6 — UI / i18n
WebUI `DelegationMetrics` type + `DelegationMetricsBar` show `reattachRecovered` /
`reattachFailed` (only when > 0); TUI `delegationMetricsText` appends
` · reattach R/N`; 7-locale i18n under `orchestrator.delegationMetrics`.

## Safety / idempotency
Recovery is the only consumer after a restart (original caller is dead → no
double-completion); keyed by CorrelationID. Never re-issues `delegation.run` (only
reads status); a running peer is polled, never restarted. Peer never SIGTERM'd;
status is a read-only localhost+token IPC query. `DelegationProtocol>=2` fails closed.
A manager-spawned warm child that died has no surviving peer/lock → unreachable →
mark failed (unchanged). A project's editor peer queried for a correlation it never
ran → unknown → mark failed (no false recovery).

## Files
- `internal/ipc/protocol/rpc.go`, `internal/ipc/bridge/{delegation_runner.go,handlers.go}`
- `internal/project/delegate_external.go`
- `internal/mesnada/orchestrator/{warm.go,orchestrator.go,metrics.go}`
- `internal/app/app.go`
- web-ui `{types/index.ts, components/orchestrator/OrchestratorView.tsx, i18n/locales/*.json}`, `internal/tui/page/orchestrator.go`
- Tests: bridge `delegation_runner_internal_test.go` (status lifecycle/running/TTL/cap) + `delegation_test.go` (status handler accept/refuse); project `delegation_internal_test.go` (recover blank/no-lock/dead-pid); orchestrator `reattach_test.go` (discriminator, completed-recovers-via-onTaskComplete+metric, unknown-fails+metric, cold-untouched, nil-recoverer-marks-failed, breadcrumb revert-on-cold-fallback / success).

## Verification
gofmt clean; `go build ./cmd/... ./internal/...` clean; `go vet` clean;
`go test -race ./internal/ipc/... ./internal/project/... ./internal/mesnada/orchestrator/...` ok;
`go test ./internal/app/... ./internal/api ./internal/tui/page ./internal/llm/agent` ok;
web-ui `npx tsc --noEmit` exit 0; 7 locale JSONs valid.

## Out of scope (future)
Full async re-attach to in-flight peer runs / live stream re-subscription (heavier
model). Manager-spawned warm-child re-attach (architecturally impossible — dies with
parent). A `running`-state poll uses local constants, not the supervisor's
ResurrectionTimeout (orchestrator DelegationConfig does not carry it).
