---
created_at: 2026-08-21T07:26:40.491596285Z
updated_at: 2026-08-21T07:26:40.491596285Z
tags:
    - fix
    - mesnada
    - delegation
    - warm-acp
    - orchestrator
    - ipc
---
# Fix: subagents stuck "pending" with engine warm-acp + self-delegation over IPC

Date: 2026-08-21

## Symptom

Delegated subagents (`mesnada_spawn_agent`, engine `pando`) stayed at status `pending`
in the orchestrator panel and showed engine `warm-acp`. Previously they cold-spawned a
`pando` CLI subprocess and behaved normally.

## What changed upstream

- `870c6f49` (2026-07-23) flipped `.pando.toml` to `AllowExternalWarmTargets = true`
  and `AcceptDelegations = true`.
- `fce7e3a2` (2026-08-08) added `models.IsWarmEligibleEngine` restricting warm routing
  to `""` / `pando` / `warm-acp` — precisely the engine these subagents use, so every
  spawn in a registered project now takes the warm path.

## Root causes

1. **Warm run never marked the task running.** `tryStartWarm`
   (`internal/mesnada/orchestrator/warm.go`) only wrote the `Running` + `warm-acp`
   breadcrumb when `externalRecoverer != nil`. Otherwise the task stayed `pending`
   for the whole (multi-minute) blocking `RunWarm`. Cascade:
   - `reclaimExpiredClaims` / `drainDispatchable` (`dispatch.go`) only consider
     pending tasks; after `DefaultClaimTTL` (2m) the claim lapsed, the task returned
     to the pool and a SECOND run of the same prompt started — every tick.
   - `canStart` checks dependencies only, never status.
   - `inFlightCounts` stopped counting it → `MaxConcurrent` breached.
   - `FileStore` hands out shared `*Task` pointers, so both runs mutated one object.
2. **Self-delegation over IPC.** `Manager.Runtime` (`internal/project/manager.go`)
   has no `os.Getpid()` check, so the current instance's own on-disk IPC lock read as
   a live *external* peer. `EnsureInstance` returned `ErrExternalInstance`, and with
   `AllowExternalWarmTargets=true` `WarmDelegate` called `DelegateExternal` against
   our own RPC port. `agentDelegationRunner.RunDelegation` then ran the "subagent" as
   a hidden session inside the parent process — no PID, no log file, no separate loop,
   recursion possible.

## Changes

- `internal/mesnada/orchestrator/warm.go`: the in-flight marking (`Engine=warm-acp`,
  `Status=Running`, `StartedAt`) is now unconditional, written before `RunWarm` and
  reverted unconditionally on the `ErrNoWarmTarget` cold fallback. Removed the
  `breadcrumb` flag.
- `internal/project/errors.go`: new `ErrSelfInstance`.
- `internal/project/delegation.go`: new `Manager.servedBySelf(path)` (compares the IPC
  lock PID with `os.Getpid()`); `EnsureInstance` returns `ErrSelfInstance` before both
  the external check and the autoStart branch, so no duplicate child is spawned for a
  directory this process already owns. `Runtime` semantics deliberately unchanged (the
  Projects panel still refuses to stop untracked instances).
- `internal/app/app.go`: `isWarmColdFallback` treats `ErrSelfInstance` as a cold-path
  fallback, not a run failure.
- `internal/mesnada/orchestrator/dispatch.go` (belt-and-braces): `reclaimExpiredClaims`
  and `drainDispatchable` skip pending tasks with `StartedAt != nil`; `inFlightCounts`
  counts them as occupying a slot.

## Tests

- New: `TestTryStartWarmMarksRunningDuringRun`,
  `TestTryStartWarmColdFallbackRevertsWithoutRecoverer` (warm_test.go);
  `TestReclaimSkipsInFlightPendingTask`, `TestDrainSkipsInFlightPendingTask`,
  `TestInFlightCountsCountUnclaimedStartedPending` (dispatch_test.go);
  `TestWarmDelegateSelfInstanceRefused` (delegation_internal_test.go).
- Updated: `TestWarmDelegateExternalFalsePassthrough` used `os.Getpid()` to fake an
  external peer — now uses `os.Getppid()` so it exercises the external path, not the
  new self guard.
- Verified: `go build ./...`, `go vet`, `go test ./internal/mesnada/orchestrator/
  ./internal/project/ ./internal/llm/agent ./internal/api` — all pass.

## Workaround without code

`ReuseWarmInstances = false` in `.pando.toml` forces the cold `pando` path.

Related: [[feature_event_log_claim_dispatch]], [[plan_delegation_future_improvements]],
[[plan_delegated_conclusion_resurrection]], [[cli_engines_skip_warm_acp]]
