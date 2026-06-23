---
created_at: 2026-06-23T08:07:31.369965656Z
updated_at: 2026-06-23T08:07:31.369965656Z
tags:
    - change
    - mesnada
    - delegation
    - b3
    - b2
    - ipc
    - zeromq
    - hot-peer
    - external-instance
    - orchestrator
    - bridge
    - epic
---
# Change: B3 — Hot-peer IPC delegation (B2 external-as-warm folded in)

Implemented 2026-06-23. Status: DONE, verified. One coherent epic (user decision:
B2's "external editor-launched instances as warm targets" cannot delegate without
B3's synchronous IPC transport, so they were built together). Plan:
`pando/plans/delegation_b3_hot_peer_ipc_epic_plan.md`. Backlog item B2 + B3 in
`pando/plans/delegation_future_improvements.md`.

## What & why

The warm delegation path only reaches a MANAGER-SPAWNED child over its stdio ACP
connection. An EXTERNAL instance (e.g. launched by an editor's ACP integration)
has no stdio pipe to this process — it is reachable only over the IPC ZeroMQ bus.
This change adds a synchronous `delegation.run` JSON-RPC over the existing
ROUTER/DEALER layer so a delegated task whose project is served by such a peer can
run inside it (capturing the `<pando:conclusion>` synchronously) instead of being
refused (`ErrExternalInstance`) and cold-spawning a CLI. Two-sided opt-in,
default-OFF: with both flags off, behaviour is byte-identical to before.

## Architecture

Caller (parent, has orchestrator) → `Manager.DelegateExternal` → IPC client
`Call("delegation.run")` → target peer's consent-gated bridge handler →
`DelegationRunner` runs a fresh ephemeral session on the target's local agent →
returns full assistant text synchronously → caller maps to `DelegateResult` →
existing `conclusion.Enrich` pipeline. Cancellation: caller's ctx-cancel watcher
fires `delegation.cancel` keyed on the caller-owned CorrelationID (the synchronous
run only returns the session id AFTER completion).

## Files & symbols

### Phase 1 — protocol (parent)
- `internal/ipc/protocol/rpc.go`: `MethodDelegationRun`/`MethodDelegationCancel`;
  `DelegationProtocolVersion = 1`; `DelegationRunParams{Prompt,Cwd,Persona,Model,
  CorrelationID}`, `DelegationRunResult{SessionID,Output,StopReason}`,
  `DelegationCancelParams{CorrelationID}`; `PingResult` gained
  `AcceptsDelegations bool` + `DelegationProtocol int` (capability advertised in
  the same round-trip that confirms liveness; old peers omit → fail closed).

### Phase 1.5 — config (parent)
- `internal/config/config.go`: `MesnadaDelegationConfig.AllowExternalWarmTargets`
  (caller opt-in) + `.AcceptDelegations` (target opt-in), both `bool` default
  false; viper `SetDefault(false)` + env `PANDO_DELEGATION_ALLOW_EXTERNAL_WARM_TARGETS`
  / `PANDO_DELEGATION_ACCEPT_DELEGATIONS`. Bools, so unaffected by the A1
  nested-default shadowing (which only hit int/string caps). `internal/config/init.go`
  template updated.

### Phase 2 — target side (subagent A, package internal/ipc/bridge)
- `handlers.go`: new `DelegationRunner` interface (`RunDelegation(ctx, params)
  (result, error)` + `CancelDelegation(correlationID)`); new
  `RegisterHandlersWithDelegation(bus, instanceID, svc, msgSvc, startedAt, runner,
  interrupter, delRunner, acceptDelegations)`; `RegisterHandlersWithAgent` now
  delegates to it with `(nil,false)` so existing exports/behaviour are unchanged.
  Ping advertises `accepts := acceptDelegations && delRunner != nil`; `delegation.run`
  returns a typed refusal when not accepting; `delegation.cancel` calls
  `CancelDelegation`.
- `delegation_runner.go` (NEW): `NewAgentDelegationRunner(sessions, agentSvc)
  DelegationRunner` — creates a session, registers correlationID→sessionID
  (mutex-guarded inflight map, deferred delete), runs `agentSvc.Run`, drains
  `<-done`, returns `DelegationRunResult{SessionID, Output: msg.Content().String(),
  StopReason:"end_turn"}`; `CancelDelegation` looks up the session and calls
  `agentSvc.Cancel`. (Persona/Model params accepted but not yet applied —
  TODO(B3-followup).)
- Tests: `delegation_test.go` (handler wiring, accept on/off, ping capability),
  `delegation_runner_internal_test.go` (runner + cancel map).

### Phase 3 — caller side (subagent B, package internal/project)
- `errors.go`: `ErrExternalDelegationRefused`, `ErrExternalUnreachable`.
- `delegate_external.go` (NEW): `Manager.DelegateExternal(ctx, projectID,
  projectPath, promptText, correlationID) (*DelegateResult, error)` — resolve path,
  `ipc.ReadLockForPath` + `pidIsAlive`, `ipc.NewClient`, capability check via
  `instance.ping`, ctx-cancel watcher (separate ephemeral client for
  `delegation.cancel`), blocking `delegation.run`, map result with `External:true`.
- `delegation.go`: `WarmDelegate` signature gained a trailing `allowExternal bool`;
  on `EnsureInstance`→`ErrExternalInstance` with `allowExternal` it routes to
  `DelegateExternal` (no manager slot acquired); `DelegateResult` gained `External bool`.
- Tests: `delegation_internal_test.go` (sentinels, unreachable, allowExternal=false
  passthrough).

### Phase 4 — orchestrator + app glue (parent)
- `internal/mesnada/orchestrator/metrics.go`: `externalHits` atomic +
  `recordExternalHit()` + snapshot field `external_hits`.
- `internal/mesnada/orchestrator/warm.go`: `WarmRunResult.External`; `tryStartWarm`
  records `recordExternalHit()` on a warm hit when `res.External`.
- `internal/app/app.go`: `makeWarmTargetResolver` threads
  `cfg...AllowExternalWarmTargets` into `WarmDelegate` and copies `res.External` →
  `WarmRunResult.External`; `isWarmColdFallback` now also treats
  `ErrExternalUnreachable`/`ErrExternalDelegationRefused` as cold-fallback.
- `cmd/bridge_delegation.go` (NEW): `registerBridgeHandlers(bus, instanceID,
  pandoApp)` — builds the `DelegationRunner` (when `AcceptDelegations` &&
  CoderAgent!=nil) and calls `RegisterHandlersWithDelegation`. All 7 cmd call sites
  (serve/app/desktop + tui/acp/failover in root.go) switched to it; default path
  (accept off) is behaviourally identical to the old `RegisterHandlers`.

### Phase 5 — UI / i18n (subagent C)
- `internal/api/handlers_settings.go`: `delegation_allow_external_warm_targets` +
  `delegation_accept_delegations` in GET/PATCH.
- `internal/tui/page/settings.go`: two toggles + apply cases.
- web-ui: `types/index.ts`, `stores/settingsStore.ts`, `GeneralSettings.tsx`
  toggles; `external_hits` in the DelegationMetricsBar; 7-locale i18n
  (en/es/fr/de/pt/ja/zh) for the two settings + the metric label.

## Safety / policy model
- Two-sided consent (caller `AllowExternalWarmTargets` + target `AcceptDelegations`),
  both default OFF. A peer that hasn't opted in returns a typed refusal → caller
  cold-falls-back. Capability negotiated over `instance.ping`, fails closed.
- Never SIGTERM/stop an editor's instance (existing `ErrExternalInstance` Stop guard
  untouched). On cancel: best-effort `delegation.cancel` (interrupts only the
  delegated session).
- The delegated run uses a fresh EPHEMERAL session — never the user's active one.
- Localhost-only + shared-token IPC (no new network surface).

## Verification
- `gofmt -l` clean; `go build ./cmd/... ./internal/...` (minus pre-existing missing
  bin/pando-desktop + webui/dist embed artifacts) OK.
- `go test -race ./internal/ipc/bridge/... ./internal/project/... ./internal/config/...`;
  `go test ./internal/mesnada/... ./internal/llm/agent ./internal/api`;
  `go test -race ./internal/mesnada/orchestrator/...` — all green.
- web-ui `npx tsc --noEmit` clean; locale JSON valid (Phase 5).

## Follow-ups (not in scope)
- Apply Persona/Model overrides on the target run (currently accepted, not applied).
- A2 cross-restart re-attach for external peers (now feasible atop this transport).
- Thread the orchestrator's real CorrelationID through `WarmDelegate`→`DelegateExternal`
  for true idempotency (currently a locally-generated `ext-<id>-<nanos>`).
