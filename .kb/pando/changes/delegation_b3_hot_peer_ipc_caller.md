---
created_at: 2026-06-23T07:53:57.893840188Z
updated_at: 2026-06-23T07:53:57.893840188Z
tags:
    - changes
    - delegation
    - b3
    - ipc
---
# B3 Hot-Peer IPC Delegation — Caller Side

**Date:** 2026-06-23
**Package:** `internal/project` only

## What was changed

Implemented the caller side of hot-peer IPC delegation (B3): the ability to delegate a subagent prompt to an external (editor-launched) peer instance over the IPC ZeroMQ bus, gated behind a new `allowExternal bool` parameter. Default behaviour is unchanged (allowExternal=false).

## Files touched

- `internal/project/errors.go` — two new sentinel errors added
- `internal/project/delegate_external.go` — NEW file with `DelegateExternal` method
- `internal/project/delegation.go` — `WarmDelegate` signature extended + external routing branch
- `internal/project/delegation_internal_test.go` — new tests + updated all WarmDelegate call sites

## New sentinels (errors.go)

```go
var ErrExternalDelegationRefused = errors.New("external instance does not accept delegations")
var ErrExternalUnreachable      = errors.New("external instance is unreachable over IPC")
```

## New method (delegate_external.go)

```go
func (m *Manager) DelegateExternal(ctx context.Context, projectID, projectPath, promptText, correlationID string) (*DelegateResult, error)
```

Steps:
1. Resolves project path (via service.Get if projectPath=="").
2. Reads on-disk IPC lock (`ipc.ReadLockForPath`) and checks `pidIsAlive`.
3. Opens an `ipc.NewClient`, calls `instance.ping` to verify `AcceptsDelegations` and `DelegationProtocol >= 1`.
4. Spawns a ctx-cancel watcher goroutine before the blocking `delegation.run` call; on ctx.Done() fires `delegation.cancel` over a separate short-lived client (avoids reusing the ctx-tied socket).
5. Unmarshals `DelegationRunResult` → `DelegateResult`.

## Modified signature (delegation.go)

```go
func (m *Manager) WarmDelegate(
    ctx context.Context,
    projectID, projectPath, promptText string,
    autoStart bool,
    maxConcurrent, queueDepth int,
    allowExternal bool,          // NEW — B3 opt-in
) (*DelegateResult, error)
```

When `EnsureInstance` returns `ErrExternalInstance` and `allowExternal=true`, a correlation id is generated (`ext-<id>-<nanoseconds>`) and `DelegateExternal` is called. No manager slot is acquired (external peers manage their own concurrency). When `allowExternal=false` (default), `ErrExternalInstance` is returned unchanged.

## Tests added

- `TestExternalDelegationSentinelsDistinct` — verifies sentinels are non-nil and distinct
- `TestDelegateExternalNoLockFile` — ErrExternalUnreachable when no lock file
- `TestDelegateExternalDeadPid` — ErrExternalUnreachable when PID is dead (pid=0)
- `TestWarmDelegateExternalFalsePassthrough` — ErrExternalInstance still returned when allowExternal=false
- `fakeExtService` / `writeFakeLockFile` helpers for external-instance simulation without a live ZMQ peer

All existing WarmDelegate call sites in the package updated for the new param.

## Verification

- `gofmt -l internal/project/` → empty (clean)
- `go vet ./internal/project/...` → no output
- `go test ./internal/project/...` → ok (0.712s)
- `go test -race ./internal/project/...` → ok (5.456s)

## Notes

- The app-layer caller (`internal/app/app.go:868`) is intentionally broken until the parent threads the new `allowExternal` arg — per task spec.
- TODO(B3): thread the orchestrator CorrelationID through WarmDelegate for true idempotency (currently generated locally as `ext-<id>-<nanos>`).
