---
created_at: 2026-08-16T20:11:37.630881112Z
updated_at: 2026-08-16T20:11:37.630881112Z
tags:
    - fix
    - mesnada
    - warm-acp
    - cli-engines
    - delegation
---
# Fix: CLI engines must not route through warm-acp

## What changed

`tryStartWarm()` hijacked every project-scoped task onto the warm Pando ACP
instance, including explicit CLI engines (`claude`, `copilot`, `gemini`, …).
Those tasks then failed with:

```
ipc: RPC error -32000: delegation.run: delegated run produced no assistant response
```

Warm reuse is an optimization for **Pando itself** (empty engine / `engine=pando`).
CLI engines spawn their own subprocess and must stay on the cold path.

## Files / symbols

- `pkg/mesnada/models/task.go` — new `IsWarmEligibleEngine(e Engine) bool`
  (`""`, `pando`, `warm-acp` only)
- `internal/mesnada/orchestrator/warm.go` — `tryStartWarm` now gates on
  `models.IsWarmEligibleEngine(o.effectiveEngine(task))`
- `internal/mesnada/orchestrator/warm_test.go` — gating cases for claude /
  copilot / gemini / acp-claude; plus `TestTryStartWarmPandoEngineStillRoutes`
- `pkg/mesnada/models/task_delegation_test.go` — `TestIsWarmEligibleEngine`

## Safety

- Default / empty / `pando` still take the warm path (existing Phase 7.3
  behavior unchanged).
- Third-party ACP engines (`acp`, `acp-claude`, …) also stay cold: they are
  not Pando warm instances.
- Returning `false` from `tryStartWarm` leaves the task untouched and falls
  through to `manager.Spawn` (the original CLI subprocess).
- Metrics: gated tasks never increment warm-attempt / fallback counters
  (same as "no project" / reuse-off).

## Verification

```
go test ./pkg/mesnada/models ./internal/mesnada/orchestrator -count=1
```

All packages passed.

Related: GitHub issue #7, [[pando/features/delegated_conclusions_resurrection.md]].
