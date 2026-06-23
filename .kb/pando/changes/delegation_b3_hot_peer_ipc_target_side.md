---
created_at: 2026-06-23T07:57:30.031266368Z
updated_at: 2026-06-23T07:57:30.031266368Z
tags:
    - changes
    - delegation
    - ipc
    - bridge
---
# B3 Hot-Peer IPC Delegation — Target Side (internal/ipc/bridge)

**Date:** 2026-06-23
**Status:** DONE
**Scope:** `internal/ipc/bridge` only — no other packages touched.

## What was changed

### handlers.go

1. **New interface `DelegationRunner`** (added alongside `MessageRunner`/`SessionInterrupter`):
   ```go
   type DelegationRunner interface {
       RunDelegation(ctx context.Context, params protocol.DelegationRunParams) (protocol.DelegationRunResult, error)
       CancelDelegation(correlationID string)
   }
   ```

2. **New function `RegisterHandlersWithDelegation`**:
   ```go
   func RegisterHandlersWithDelegation(
       bus BusRegistrar, instanceID string,
       svc session.Service, msgSvc message.Service,
       startedAt time.Time,
       runner MessageRunner, interrupter SessionInterrupter,
       delRunner DelegationRunner, acceptDelegations bool,
   )
   ```
   - Registers all existing handlers (moved from `RegisterHandlersWithAgent`).
   - Effective capability: `accepts := acceptDelegations && delRunner != nil`.
   - Ping handler now sets `AcceptsDelegations` and `DelegationProtocol` (= `protocol.DelegationProtocolVersion` = 1) when `accepts`.
   - `delegation.run` handler: returns refusal error when `!accepts`; validates non-empty `Prompt`; calls `delRunner.RunDelegation`; marshals `DelegationRunResult`.
   - `delegation.cancel` handler: unmarshals `DelegationCancelParams`; calls `delRunner.CancelDelegation` when `accepts`; always returns `OKResult{OK:true}`.

3. **`RegisterHandlersWithAgent` refactored** to delegate to `RegisterHandlersWithDelegation(..., nil, false)` — no behavioral change, existing call sites unaffected.

4. **`RegisterHandlers`** unchanged (still calls `RegisterHandlersWithAgent`).

### delegation_runner.go (new file)

- Two narrow internal interfaces to avoid requiring full service implementations in tests:
  - `delegationSessionCreator` — only `Create`.
  - `delegationAgentRunner` — only `Run` and `Cancel`.
- `agentDelegationRunner` struct holds these narrow deps plus `sync.Mutex`-guarded `inflight map[string]string` (correlationID → sessionID).
- **`NewAgentDelegationRunner(sessions session.Service, agentSvc agent.Service) DelegationRunner`** — public constructor, panics on nil deps, delegates to `newAgentDelegationRunnerFromDeps`.
- `newAgentDelegationRunnerFromDeps(...)` — internal constructor used by tests.
- `RunDelegation`: validates prompt → creates session → registers inflight (deferred delete) → calls `agentSvc.Run` → drains channel → validates assistant role → returns `DelegationRunResult{SessionID, Output, StopReason: "end_turn"}`. Sessions are NOT deleted after the run (kept for history/recovery). Includes `TODO(B3-followup)` comment for persona/model override.
- `CancelDelegation(correlationID)`: looks up sessionID under mutex; calls `agentSvc.Cancel(sessionID)` if found.

### delegation_test.go (new file, package bridge_test)

Handler wiring tests via `fakeDelegationRunner` + `capturingBus` (reused from bridge_test.go):
- `TestRegisterHandlersWithDelegation_PingNoAccept` — ping advertises false/0 when off.
- `TestRegisterHandlersWithDelegation_PingAccepts` — ping advertises true/1 when on.
- `TestRegisterHandlersWithDelegation_PingAcceptsFlagButNoRunner` — nil runner forces false.
- `TestDelegationRun_RejectsWhenNotAccepted` — handler returns error when off.
- `TestDelegationRun_RejectsEmptyPrompt` — handler rejects empty prompt.
- `TestDelegationRun_InvokesRunnerAndReturnsResult` — happy path, result marshalled.
- `TestDelegationRun_PropagatesRunnerError` — runner error propagates.
- `TestDelegationCancel_NoOpsWhenNotAccepted` — returns OK without calling runner.
- `TestDelegationCancel_CallsRunner` — calls runner.CancelDelegation with correlation id.
- `TestRegisterHandlersWithAgent_PingAdvertisesNoDelegation` — backward compat check.

### delegation_runner_internal_test.go (new file, package bridge)

Internal tests for `agentDelegationRunner` using `stubSessionCreator`/`stubAgentRunner` (narrow interfaces):
- `TestAgentDelegationRunner_EmptyPromptRejected`
- `TestAgentDelegationRunner_SessionCreateError`
- `TestAgentDelegationRunner_SuccessfulRun`
- `TestAgentDelegationRunner_NonAssistantResponseError`
- `TestAgentDelegationRunner_AgentRunError`
- `TestAgentDelegationRunner_CancelInflightEntry`
- `TestAgentDelegationRunner_CancelUnknownID_NoOp`
- `TestAgentDelegationRunner_InflightRemovedAfterRun`

## Verification

- `gofmt -l internal/ipc/bridge/` → empty (no formatting issues).
- `go vet ./internal/ipc/bridge/...` → clean.
- `go test -count=1 -race ./internal/ipc/bridge/...` → PASS (1.077s).
- `go build ./internal/ipc/bridge/...` → clean.

## What is NOT done (deferred to B3-followup)

- Per-session persona/model override in `RunDelegation` (marked TODO).
- Wiring `RegisterHandlersWithDelegation` into `cmd/` or `app/` — left to the parent integration task.
