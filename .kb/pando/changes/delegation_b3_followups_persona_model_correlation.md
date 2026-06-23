---
created_at: 2026-06-23T13:42:27.771654228Z
updated_at: 2026-06-23T13:42:27.771654228Z
tags:
    - change
    - mesnada
    - delegation
    - b3
    - external
    - ipc
    - persona
    - model
    - correlation-id
    - session-overrides
---
# Change: B3 hot-peer delegation follow-ups — apply persona/model on target + thread real CorrelationID

Date: 2026-06-23. Closes the two TODOs the B3 hot-peer IPC delegation epic
(`pando/changes/delegation_b3_hot_peer_ipc.md`) shipped with. Chosen by the user
("Close B3 TODOs") as the next remaining-backlog item. Default behaviour unchanged.

## What changed

### 1. Apply the accepted-but-ignored persona/model override on the delegation TARGET run
Before, `NewAgentDelegationRunner.RunDelegation` accepted `params.Persona` /
`params.Model` (from the `delegation.run` IPC RPC) but ignored them — a delegated
external run always used the target's own default model and persona. Now the
runner threads them through the **request-scoped `SessionLLMOverrides`** mechanism
(the same one the ACP server uses, added in Phase 7.1), so the override applies to
that delegated session ONLY and never mutates the target's global agent state or
its other concurrent sessions.

- `internal/ipc/bridge/delegation_runner.go`:
  - New helper `delegationSessionOverrides(params) (agent.SessionLLMOverrides, bool)`:
    maps `params.Model` → `SessionLLMOverrides.Model` (`models.ModelID`), and a
    non-empty `params.Persona` → `Persona` + `PersonaScoped=true` (so it overrides
    the target's global active persona for this session). Returns `ok=false` when
    neither is set (target keeps its defaults). Whitespace-only values trimmed to empty.
  - In `RunDelegation`, replaced the `TODO(B3-followup)` block: when `ok`, calls
    `agent.SetSessionLLMOverrides(sess.ID, ov)` before `agentSvc.Run`, and a
    `defer agent.SetSessionLLMOverrides(sess.ID, agent.SessionLLMOverrides{})`
    clears it on return (an empty override is a delete) so the kept ephemeral
    session does not silently reuse the override on any later turn.
  - No validation of the model is needed: `createAgentProvider` only honors a
    supported model, so an unknown model is gracefully ignored downstream.
  - New imports: `strings`, `internal/llm/models`.

### 2. Thread the orchestrator's real CorrelationID through to external delegation
Before, `WarmDelegate`'s external branch synthesized `ext-<id>-<nanos>` as the
correlation/idempotency key, so a `delegation.cancel` could not be matched to the
orchestrator's task identity. Now the task's `CorrelationID` flows end-to-end;
the synthesized id remains only as a fallback when the caller supplies none (e.g.
a direct `WarmDelegate` call without a task).

- `internal/mesnada/orchestrator/warm.go`: `WarmTargetResolver.RunWarm` gained a
  trailing `correlationID string` param; `tryStartWarm` passes `task.CorrelationID`.
- `internal/app/app.go`: `warmTargetResolverFunc` type + method + the
  `makeWarmTargetResolver` closure gained the `correlationID` param and pass it to
  `mgr.WarmDelegate(...)`.
- `internal/project/delegation.go`: `WarmDelegate` gained a trailing
  `correlationID string` param; the external branch uses it (`cid := correlationID;
  if cid == "" { cid = fmt.Sprintf("ext-%s-%d", id, time.Now().UnixNano()) }`)
  when calling `DelegateExternal`. The manager-spawned warm path ignores it
  (`delegateOn` does not need a correlation key).

## Files touched
- `internal/ipc/bridge/delegation_runner.go` (apply persona/model + helper)
- `internal/project/delegation.go` (`WarmDelegate` signature + external correlation use)
- `internal/mesnada/orchestrator/warm.go` (`RunWarm` signature + `tryStartWarm` threads `task.CorrelationID`)
- `internal/app/app.go` (`warmTargetResolverFunc` + closure threading)
- Tests: `internal/ipc/bridge/delegation_runner_internal_test.go`
  (`TestDelegationSessionOverrides` table — none/model-only/persona-only/both/blank;
  `TestAgentDelegationRunner_RunWithOverrides` smoke), `internal/mesnada/orchestrator/warm_test.go`
  (`fakeWarmResolver` captures `gotCorr`; new `TestTryStartWarmThreadsCorrelationID`),
  `internal/project/{manager_warm_test,delegation_internal_test}.go` (all `WarmDelegate`
  call sites updated with the new trailing `""` correlationID arg).

## Why
Closes the two loose ends documented at the end of the B3 epic so external
(editor-launched) warm delegation honors the requested per-task model/persona and
is cancel-addressable by the orchestrator's own idempotency key, rather than a
locally-synthesized one. Both are pure plumbing on top of existing mechanisms
(7.1 SessionLLMOverrides; B3 IPC transport).

## Verification
- `gofmt -l` clean on all touched files.
- `go build ./internal/... ./cmd/...` clean.
- `go vet` clean on bridge/project/orchestrator/app.
- `go test -race ./internal/ipc/bridge/... ./internal/mesnada/orchestrator/... ./internal/project/...` all ok.
- `go test ./internal/llm/agent ./internal/api ./internal/app/...` all ok.

## Still open (not in this change)
- E2 (per-session model/persona surfaced in the Projects panel).
- A2 cross-restart re-attach for external peers atop the B3 transport.
- Default-off opt-in flags unchanged (`AllowExternalWarmTargets` / `AcceptDelegations`):
  this change only affects the path already gated behind them.
