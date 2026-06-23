---
created_at: 2026-06-23T07:41:58.382515422Z
updated_at: 2026-06-23T07:41:58.382515422Z
tags:
    - plan
    - epic
    - mesnada
    - delegation
    - b3
    - b2
    - ipc
    - zeromq
    - hot-peer
    - external-instance
    - warm-instance
    - orchestrator
---
# Plan: B3 — Hot-peer IPC delegation epic (B2 external-as-warm folded in)

Created 2026-06-23. Status: PLANNED → IN PROGRESS. Owner: parent + subagents.
Supersedes the separate B2 backlog item: B2 ("external editor-launched instances
as warm delegation targets") is folded in here as one consumer of the new IPC
delegation transport. From the backlog `pando/plans/delegation_future_improvements.md`.

## Why one epic (the finding that merged B2 into B3)

The existing warm path delegates over `inst.conn` — the **stdio** ACP connection of
a *manager-spawned* child (`internal/project/delegation.go` `delegateOn` →
`conn.NewSession` + synchronous `conn.Prompt`, output captured by the per-session
capturing client). An **external** (editor-launched) instance has NO stdio pipe to
this manager; the only channel to it is the **IPC ZeroMQ bus** (ROUTER/DEALER RPC +
PUB/SUB), whose endpoints are published in `<projectDir>/.pando/ipc.lock`
(`LockInfo{PID, PubPort, RPCPort}`, read via `ipc.ReadLockForPath`).

The IPC bus today CANNOT carry a delegation:
- `message.send` (`protocol.MethodMessageSend`) is fire-and-forget — returns after
  launching the agent goroutine, no result. And it is wired nowhere today:
  every call site uses `bridge.RegisterHandlers(...)` (runner/interrupter = nil),
  so `message.send`/`session.interrupt` are currently unhandled.
- There is no `session.create` RPC over IPC (only list/get/activate/interrupt).
- `session.update` PUB carries only `SessionPayload` (id/title/count) — not the
  streamed assistant text, and has no turn-complete boundary correlated to a
  delegated session, so `<pando:conclusion>` cannot be captured over the wire.

Therefore "external-as-warm (B2)" cannot actually delegate without building the
synchronous IPC delegation transport that IS B3's core. User decision (2026-06-23):
**skip the partial B2, build B3 as one coherent epic; external-as-warm is one
consumer.** Default-OFF throughout (caller opt-in AND target opt-in).

## Enabling facts (verified in code)

- The target instance's `agent.Service` (`internal/llm/agent`) emits a clean
  `AgentEventTypeResponse` at turn end (session-scoped), plus `ContentDelta`
  tokens; `internal/ipc/bridge/bridge.go` already subscribes to these. So a
  delegated turn run in-process on the target has a precise completion boundary
  and full assistant text available locally — the handler does NOT need to stream
  over the bus; it runs synchronously and returns the captured text in the RPC
  response.
- IPC client (`internal/ipc/client.go`) already supports synchronous `Call(ctx,
  routerEndpoint, method, params)` (DEALER) and `SubscribeTo(pubEndpoint, topics)`
  (SUB). Localhost-only, token-authed (`~/.config/pando/zmq_token`).
- `ipc.ReadLockForPath` / `ipc.PortsForPath` already resolve a peer's endpoints
  from the on-disk lock without acquiring the flock.
- Orchestrator already has a `WarmTargetResolver` seam + `DelegationMetrics`
  (atomic counters) + `ErrWarmCapReached`/`ErrNoWarmTarget` sentinels; the app
  adapter `makeWarmTargetResolver` calls `Manager.WarmDelegate`.

## Architecture

Add a **synchronous request/response delegation RPC** over the existing IPC
JSON-RPC layer, served by a new bridge handler backed by the target instance's
local agent, and consumed by a new IPC-based delegation transport on the caller
side that the project Manager routes to when a target is an external/peer
instance. Both ends are opt-in.

```
Caller (parent, has orchestrator)        Target peer (editor or other pando)
  Manager.DelegateExternal                 bridge: delegation.run handler
   resolve lock -> RPC endpoint              consent gate (AcceptDelegations)
   capability check (instance.info)          DelegationRunner.RunDelegation:
   ipc.Client.Call("delegation.run") ----->    create ephemeral session
        {prompt,cwd,persona,model,corr}         run agent turn to completion
   <----- {session_id, output, stop } ------    capture assistant text
   map -> DelegateResult                       return (no PUB streaming needed)
   conclusion.Enrich(output) (existing)
   ctx cancel -> Call("session.interrupt")
```

## Safety / policy model (B2's "design note first")

- **Two-sided consent.** Caller must enable `AllowExternalWarmTargets` (route TO
  peers). Target must enable `AcceptDelegations` (accept incoming delegation.run).
  Both default FALSE → today's behaviour byte-for-byte. A peer that has not opted
  in returns a typed "delegation not accepted" error → caller cold-falls-back.
- **No stop-cancel authority over an editor's process.** We never SIGTERM an
  external instance (that stays the launching app's job — existing
  `ErrExternalInstance` guard for Stop is untouched). On parent cancel we issue a
  best-effort `session.interrupt` RPC for the delegated session only.
- **Isolation.** The delegated run uses a fresh EPHEMERAL session on the target;
  it never touches the user's active editor session. (Mirrors the warm path's
  ephemeral-session discipline.)
- **Transport security.** Reuses IPC's localhost-only + shared-token auth. No new
  surface beyond 127.0.0.1.
- **Version/capability negotiation.** `instance.info` advertises
  `AcceptsDelegations` + a `DelegationProtocol` version; the caller checks it
  before attempting `delegation.run`, so a peer too old simply isn't used.
- **Idempotency.** CorrelationID flows in params (as the cold/warm paths already
  do) so a retried delegation is de-duplicated by the existing pipeline.

## Phases

### Phase 1 — Protocol + capability negotiation (shared contract; parent owns, not parallelized)
- `internal/ipc/protocol/rpc.go`: `MethodDelegationRun = "delegation.run"`,
  `MethodDelegationCancel = "delegation.cancel"` (or reuse session.interrupt);
  `DelegationRunParams{Prompt,Cwd,Persona,Model,CorrelationID}`,
  `DelegationRunResult{SessionID,Output,StopReason}`.
- `instance.info` result gains `AcceptsDelegations bool` + `DelegationProtocol int`
  (`MethodInstanceInfo` already reserved). Add `InstanceInfoResult` if missing.
- Constants only; no behaviour. Unit: protocol round-trip marshal.

### Phase 2 — Target side: handler + runner + consent + config (subagent A; owns bridge + app target-wiring + config AcceptDelegations)
- `internal/ipc/bridge/handlers.go`: new `DelegationRunner` interface
  (`RunDelegation(ctx, DelegationRunParams) (DelegationRunResult, error)`, local
  to avoid import cycle) + register `delegation.run` (consent-gated: handler only
  active when accept==true, else typed error) and advertise `AcceptsDelegations`
  via instance.info. New `RegisterHandlersWithDelegation` (or extend
  `RegisterHandlersWithAgent` signature) — keep existing call sites compiling.
- App: implement `DelegationRunner` backed by `agent.Service` (create session via
  `session.Service`, run the turn, capture assistant text by subscribing to agent
  events filtered by session id until `AgentEventTypeResponse`). Wire it where the
  bus/bridge is set up (`cmd/*.go` call sites + `internal/app`).
- Config: `MesnadaDelegationConfig.AcceptDelegations bool` (default false) +
  setDefaults + env `PANDO_DELEGATION_ACCEPT_DELEGATIONS` + normalize.
- Tests: handler with fake runner (accept on/off), runner captures output.

### Phase 3 — Caller side: external transport + routing + config (subagent B; owns project DelegateExternal + config AllowExternalWarmTargets + EnsureInstance/WarmDelegate integration)
- `internal/project`: `Manager.DelegateExternal(ctx, projectID, path, prompt)
  (*DelegateResult, error)` — resolve external endpoint from lock, capability
  check (instance.info), `ipc.Client.Call(delegation.run)`, map result; ctx-cancel
  → `Call(session.interrupt)`. New typed sentinels as needed
  (`ErrExternalDelegationRefused`, `ErrExternalUnreachable`).
- Integrate into the warm decision: when project is served by an external instance
  AND `AllowExternalWarmTargets` AND peer accepts → route to `DelegateExternal`
  instead of returning `ErrExternalInstance`. `EnsureInstance`/`WarmDelegate`
  decision branch.
- Config: `MesnadaDelegationConfig.AllowExternalWarmTargets bool` (default false) +
  setDefaults + env `PANDO_DELEGATION_ALLOW_EXTERNAL_WARM_TARGETS` + normalize.
- Tests: DelegateExternal over a loopback/fake bus; gating; cold-fallback when
  refused/unreachable/flag off.

NOTE on shared file (`internal/config/config.go`): the PARENT adds BOTH new struct
fields + defaults + env + normalize in Phase 1.5 (before fan-out) so subagents A/B
never both edit config.go. Each subagent only READS the field it needs.

### Phase 4 — Orchestrator routing + metrics (parent, after A/B unify)
- Extend `DelegationMetrics`: `external_attempts`, `external_hits`,
  `external_failures` (lock-free atomics, derived rate). Record in the warm
  adapter / DelegateExternal path. `external_refused` counted as cold-fallback.
- Ensure the warm adapter (`makeWarmTargetResolver`) routes external when enabled;
  `isWarmColdFallback` recognises the new sentinels as cold-fallback (not failure).

### Phase 5 — UI / i18n / API (subagent C; mechanical, established pattern)
- Expose `AllowExternalWarmTargets` + `AcceptDelegations` in TUI settings, WebUI
  GeneralSettings + store/types, REST settings handler, and 7-locale i18n —
  mirroring the existing `ReuseWarmInstances`/`AutoStartWarmInstance`/`WarmQueueDepth`
  knobs. Surface new external metrics in the TUI dashboard line + WebUI
  DelegationMetricsBar (read-only).

### Phase 6 — e2e + docs (parent)
- e2e: two in-process instances over a loopback bus — caller delegates to "external"
  target, asserts conclusion round-trips; refused/flag-off → cold-fallback.
- Docs: feature doc `pando/features/delegated_conclusions_resurrection.md` (new
  "Hot-peer IPC delegation" section), README bullet, change doc
  `pando/changes/delegation_b3_hot_peer_ipc.md`. Mark B2+B3 DONE, A2 re-attach
  reconsidered (peers now survivable for the external case) in the backlog.

## Default-off doctrine
Every new knob (`AllowExternalWarmTargets`, `AcceptDelegations`) defaults FALSE.
With both off, behaviour is byte-identical to today: external instances remain
non-warm (`ErrExternalInstance` → cold path), no delegation.run handler is active.

## Subagent fan-out plan (proven D1/D2 pattern)
- Parent: Phase 1 (protocol) + Phase 1.5 (config fields) on main tree first.
- Then parallel worktree subagents (sonnet), DISJOINT file ownership:
  - A = Phase 2 target side (bridge/handlers.go, app runner wiring, bridge tests).
  - B = Phase 3 caller side (internal/project delegate_external.go + errors +
    EnsureInstance integration + project tests).
- Parent unifies (cp from worktrees), wires Phase 4 metrics + the cross-cut glue,
  builds/tests/-race, then Phase 5 (subagent C, UI/i18n) and Phase 6 (parent docs).

## Verification
- `go build ./...` (minus pre-existing missing embed artifacts), `gofmt -l`, `go vet`.
- `go test ./internal/ipc/... ./internal/project ./internal/mesnada/... ./internal/llm/agent ./internal/api` + `-race` on touched pkgs.
- web-ui `npx tsc --noEmit` when Phase 5 lands.

## Open risks
- Synchronous run on the target may be long; the RPC needs a generous/ctx-bounded
  timeout and the caller must map timeout → cold-fallback, not hang.
- Capability negotiation must fail closed (unknown peer version → don't delegate).
- Avoid import cycles: keep `DelegationRunner` a local bridge interface; keep the
  project-side transport free of agent imports (IPC client + protocol only).
