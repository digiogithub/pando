---
created_at: 2026-07-23T13:52:44.58300294Z
updated_at: 2026-07-23T13:52:44.58300294Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestration
    - blackboard
    - coordination
    - prototype
---
# Mesnada swarm blackboard + conclusion forwarding (P1+P2 prototype)

Prototype of the two highest-value coordination improvements from
[[hermes-kanban-swarm-vs-pando-delegation]]: **P1** a sibling **blackboard** (shared
coordination state between delegated subagents) and **P2** **conclusion forwarding**
(feed a dependency's structured `Conclusion` into its dependents' prompt). Built 2026-07-23.
Default-off: the whole swarm-context injection is gated on `Mesnada.Delegation.Enabled`, so
zero behavior change when delegation is off.

## What changed

### P1 — sibling blackboard
- **New** `internal/mesnada/orchestrator/blackboard.go`: `Blackboard` type — durable,
  process-shared, keyed by swarm id. Append-only `BlackboardEntry{Key,Value(json.RawMessage),
  Author,TaskID,CreatedAt}`; `Latest()` merges last-write-wins per key (sorted, deterministic);
  `List()` returns the full log. Persisted atomically to `blackboard.json` beside the task
  store. Values are `json.Compact`-canonicalized on `Post` and the file is saved with
  `json.Marshal` (not `MarshalIndent`) so stored values survive a save/reload round-trip
  byte-for-byte (Indent would re-flow embedded RawMessage). Non-JSON values are wrapped as a
  JSON string so the file never corrupts. Mirrors Hermes' low-tech blackboard (JSON comments on
  a root card) with no second scheduler.
- **Swarm id resolution** (`Orchestrator.swarmKeyForTask`): `ParentSessionID` (siblings of one
  parent agent session share it) → `ParentTaskID` → `CorrelationID` → "".
- **New tool** `mesnada_note` (`internal/llm/tools/mesnada.go`, `MesnadaNoteTool`):
  `action=post|list`, `swarm_id`, `key`, `value` (any JSON), `author`. Reachable by subagents
  because the dynamic subagent MCP config already exposes pando as an MCP server. Registered in
  `internal/llm/agent/tools.go` and allow-listed in `internal/llm/tools/builtin_names.go`.

### P2 — conclusion forwarding
- **`Orchestrator.getDependencyConclusions(deps)`**: renders each completed dependency's
  `Conclusion` (status, summary, artifacts, memory_refs, follow_up) as a
  `===DEPENDENCY CONCLUSIONS===` block. Pando already captured this per task but only ever
  injected raw log tails (`getDependencyLogs`); this forwards the enriched result.

### Injection point
- **`Orchestrator.buildSwarmContext(task)`** combines the P2 conclusion block + a P1 blackboard
  pointer (swarm id, `mesnada_note` usage, current merged facts). Injected in **`startTask`**
  (not `Spawn`) — dependencies are guaranteed complete at start time, whereas the pre-existing
  dep-log path reads at spawn when deps may be unfinished. Guarded by the `===SWARM CONTEXT===`
  marker so retries/relaunch never double-append; the augmented prompt is persisted so both the
  cold spawner and the warm-ACP path (which read `task.Prompt`) see it.

## Files touched
- New: `internal/mesnada/orchestrator/blackboard.go`,
  `internal/mesnada/orchestrator/blackboard_test.go`,
  `internal/mesnada/orchestrator/swarm_context_test.go`
- Modified: `internal/mesnada/orchestrator/orchestrator.go` (blackboard field + `NewBlackboard`
  wiring in `New`; `swarmKeyForTask`, `PostNote`, `ListNotes`, `getDependencyConclusions`,
  `buildSwarmContext`, `swarmContextMarker`; injection in `startTask`),
  `internal/llm/tools/mesnada.go` (`mesnadaNoteToolName`, `MesnadaNoteTool` Info/Run,
  `encoding/json` import), `internal/llm/agent/tools.go` (register),
  `internal/llm/tools/builtin_names.go` (allow-list).

## Verification
- `go build ./...` clean.
- `go test ./internal/mesnada/orchestrator/ ./internal/llm/tools/ ./internal/llm/agent ./internal/api` pass.
- New tests: blackboard post/last-write-wins/isolation, persist-across-reopen, post validation,
  swarm-key precedence, dependency-conclusion forwarding, combined swarm context, empty-context
  when no correlation + no deps.
- `go vet` clean for touched packages (a pre-existing unrelated warning remains in
  `internal/mesnada/agent/spawner_template.go`, not part of this change).

## Not done (follow-ups from the analysis)
- P3 verifier-gate + `mesnada_swarm` topology helper (needs `TaskStatusBlocked`/`Review` + gate
  predicate in `canStart`).
- No config flag yet beyond reusing `Delegation.Enabled`; a dedicated `Mesnada.Swarm` toggle
  could gate P1 independently of the conclusion protocol.
- Blackboard has no GC/size cap; entries live for the store's lifetime.
