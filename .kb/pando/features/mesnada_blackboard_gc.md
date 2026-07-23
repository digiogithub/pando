---
created_at: 2026-07-24T05:01:11.195416292Z
updated_at: 2026-07-24T05:01:11.195416292Z
tags:
    - feature
    - mesnada
    - delegation
    - blackboard
    - gc
    - orchestration
---
# Mesnada swarm blackboard garbage collection

Bounds the sibling-coordination [[mesnada_swarm_blackboard_conclusion_forwarding]]
blackboard so it no longer grows unbounded. This closes the "Blackboard GC / size
cap" item tracked in that feature and in
[[mesnada_swarm_verifier_gate_plan]]. Built 2026-07-24, after
[[mesnada_durable_event_log_and_claim_dispatch]] (P5+P7). ON by default with
documented limits.

## Problem

`internal/mesnada/orchestrator/blackboard.go` kept `entries map[string][]BlackboardEntry`
(swarmID -> append-only log) with no bound on two axes:

1. **Per-swarm log length** — every `Post` appended forever; superseded facts were
   never shed even though `Latest` only reads each key's winner.
2. **Number of swarms** — every swarm that ever ran kept its log for the store's
   whole lifetime; a finished swarm was never removed.

Worse, `saveLocked` re-marshals the **entire** map on every `Post`, so unbounded
growth also made each write progressively more expensive (O(total history)).

## What changed

### `blackboard.go`
- New GC defaults: `DefaultBlackboardMaxEntriesPerSwarm = 200`,
  `DefaultBlackboardTTL = 7*24h`.
- `Blackboard` gains `maxEntriesPerSwarm int` and `ttl time.Duration`.
- New `BlackboardOption` + `WithBlackboardLimits(maxEntriesPerSwarm, ttl)`
  (non-positive arg leaves that axis's default; existing `NewBlackboard(path)`
  callers keep compiling via variadic opts).
- `NewBlackboard` runs `pruneExpiredLocked` on open (in memory only — does not
  rewrite the file just by being read; the next `Post` persists the shrunk map).
- `compactSwarmLocked(swarmID)`: past the cap, keeps the newest `maxEntriesPerSwarm`
  entries **plus every key's winning (last) entry even when it falls outside the
  window**, in insertion order. So GC only sheds superseded history — the merged
  `Latest` view is never altered. Retained count = min-bounded by cap, may exceed
  it by the number of orphan winners (bounded by distinct keys).
- `pruneExpiredLocked(now)`: deletes any swarm whose newest entry is older than
  `ttl` (and any empty log). This is the "purge terminated swarms" axis — a
  finished swarm stops posting and ages out on its own, so the blackboard never
  needs to know task lifecycle state (dependency-free, unlike a terminal-state
  hook).
- `Post` now calls `compactSwarmLocked(swarmID)` + `pruneExpiredLocked(now)` before
  `saveLocked`.

### Wiring
- `DelegationConfig` (orchestrator) gains `BlackboardMaxEntriesPerSwarm int`,
  `BlackboardTTL time.Duration`. `New` passes them via `WithBlackboardLimits` to
  both the on-disk board and the in-memory error fallback.
- `MesnadaDelegationConfig` (config) gains `BlackboardMaxEntriesPerSwarm int`
  (`blackboardMaxEntriesPerSwarm`) and `BlackboardTTL string` (`blackboardTtl`).
- Consts `defaultDelegationBlackboardMaxEntries = 200`,
  `defaultDelegationBlackboardTTL = "168h"`; viper `SetDefault` for both;
  `normalizeMesnadaDelegationDefaults` restores them when zero/empty (same
  viper-nested-default-shadowing rule as the event log — a shadowed 0 would
  otherwise silently disable GC).
- `app.go` maps both config fields into `DelegationConfig` (TTL via
  `parseDelegationDuration`).
- `init.go` TOML template: `BlackboardMaxEntriesPerSwarm = 200` +
  `BlackboardTtl = '168h'` under `[Mesnada.Delegation]` with a comment.

### Surfaces
- No TUI/WebUI/REST controls: these are internal GC tuning knobs, exposed only via
  the TOML template + config normalize (source of truth). Consistent with keeping
  the settings UI to user-facing toggles; can be surfaced later if requested.

## Tests
`blackboard_test.go`:
- `TestBlackboardCompactionBoundsLogButKeepsWinners`: cap=3, an early one-shot
  "owner" winner ages out of the newest window and still survives; List==4 (3
  newest + 1 orphan winner); Latest unchanged; bound persists across reopen.
- `TestBlackboardTTLPurgesStaleSwarms`: a stale swarm is purged by the global sweep
  a fresh `Post` triggers; the fresh swarm survives.
- `TestBlackboardPruneExpiredOnOpen`: an expired swarm written to the file is
  pruned on reopen; a live swarm survives.
- Both option helpers assert the non-positive-arg-keeps-default behaviour.
`mesnada_delegation_test.go`: extended
`TestMesnadaEventLogAndDispatcherDefaultsUnderShadowing` with blackboard-default
assertions.

## Verification
`go build ./...`, `gofmt`, full `go test ./...` clean;
`go test -race -count=2 ./internal/mesnada/orchestrator/` green;
`go test ./internal/config -run Mesnada` green.

## Not done (deliberate)
- Terminal-state-driven purge (drop a swarm's board the moment its synthesizer
  reaches a terminal state): TTL covers it dependency-free; a lifecycle hook can be
  added later if a week is too long to hold a finished board.
- Backlog now: only `Blocked/Review` lane and `auto_decompose` remain from
  [[hermes-kanban-swarm-vs-pando-delegation]] — both assessed as not worth doing
  now (see chat 2026-07-24).
