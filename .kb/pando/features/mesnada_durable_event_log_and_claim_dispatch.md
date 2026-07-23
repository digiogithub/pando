---
created_at: 2026-07-23T22:11:11.457271408Z
updated_at: 2026-07-23T22:11:11.457271408Z
tags:
    - feature
    - mesnada
    - delegation
    - orchestration
    - events
    - dispatch
    - hermes
---
# P5 durable event log + P7 claim-lease dispatch & concurrency caps

Implementation of the last two ranked improvements from
[[hermes-kanban-swarm-vs-pando-delegation]], on top of P1+P2
([[mesnada_swarm_blackboard]]), P3 ([[mesnada_swarm_verifier_gate]]) and P6+P4
([[mesnada_conclusion_gate_and_circuit_breaker]]). Date: 2026-07-24.

## Motivation

**P5** — Pando's delegation feedback loop (Case A live injection, Case B
resurrection) was driven exclusively by an in-memory completion bus whose sends
are non-blocking (`broadcastCompletion` drops on a full buffer), and whose
subscriber state (`seen` map, pending batches) is lost on restart. A conclusion
could therefore vanish with no trace: a busy supervisor dropped it, or the
process died before consuming it. Hermes solves this with an append-only
`task_events` table plus a cursor-advanced notifier.

**P7** — Two independent problems:

1. `Mesnada.Orchestrator.MaxParallel` was parsed, stored, surfaced in the TUI
   and WebUI settings … and **never enforced anywhere**. `grep maxParallel`
   showed exactly one production use: the assignment in `New`. The advertised
   cap was a lie.
2. A genuine double-start race: two dependencies of the same task completing at
   the same instant both run `processDependentTasks`, both list pending tasks,
   both find the dependent startable, and both `go o.startTask(task)`.

## P5 — durable delegation event log

### `internal/mesnada/events` (new, dependency-free)

- `Event{Seq, Kind, TaskID, ParentSessionID, CorrelationID, Detail, CreatedAt}`.
  Deliberately minimal: identifiers plus a short human-readable note. Consumers
  re-read the task from the store, so a replayed event can never resurrect a
  stale snapshot of it.
- `Kind`: terminal (`completed`, `failed`, `cancelled`) plus informational
  (`gate_failed`, `breaker_tripped`, `respawn_refused`, `reclaimed`).
  `Kind.IsTerminal()` is what the supervisor acts on.
- `Log` — JSONL append-only file plus a sibling `*.cursors.json`:
  - `Append` assigns the next `Seq`, stamps `CreatedAt`, writes one line.
  - `Unseen(subID)` returns everything past the acked watermark, oldest first.
    An **unknown** subscription starts at 0, so a consumer that lost its cursor
    file replays what is retained rather than silently skipping it.
  - `Ack(subID, seq)` moves the watermark forward only (a stale ack cannot
    rewind) and persists atomically.
  - Compaction past `2*maxEntries` trims to the newest `maxEntries` and **clamps
    registered cursors forward** to the retained floor: bounded, explicit loss is
    preferred to a log growing without limit because one consumer stopped acking.
  - A **nil `*Log` is a valid no-op log** — every method is nil-safe, so the
    orchestrator holds `nil` when the feature is disabled instead of branching at
    each call site.
  - A malformed line is skipped on load; the event log must never be the reason
    the orchestrator cannot start.

### Orchestrator wiring

- Opened in `New` beside the task store and blackboard (`events.jsonl`); an open
  failure logs and leaves `eventLog` nil.
- `onTaskComplete` appends the terminal event **before** `broadcastCompletion`:
  the log is the record, the broadcast is only a best-effort wakeup.
- `Cancel` records a `cancelled` event (and releases the claim) but deliberately
  does **not** broadcast — a cancelled task carries no conclusion, so there is
  nothing for a parent loop to act on.
- Informational events appended at their existing log sites: gate failure in
  `processDependentTasks`, breaker trip in `recordBreakerOutcome`, refusal in
  `guardRespawn`, reclaim in `reclaimExpiredClaims`.
- Public API: `UnseenEvents(subID)`, `AckEvent(subID, seq)`, `RecentEvents(n)`,
  `EventLogEnabled()`.

### Supervisor consumption (`internal/app/delegation_supervisor.go`)

New `eventSource` interface (`EventLogEnabled`/`UnseenEvents`/`AckEvent`/
`GetTask`), satisfied by `*Orchestrator`, stubbed in tests.

When the log is enabled, the completion channel becomes a **mere wakeup**: the
dispatch loop calls `drainEvents()`, which handles unacked events in order and
acks each one. This is what makes a dropped or duplicated broadcast harmless.
Plus:

- an initial drain at `Start` (**replay** of anything a previous run left
  unacked, before live traffic);
- a 30s `eventDrainInterval` ticker as a safety net for a dropped wakeup with no
  follow-up completion;
- `eventReplayMaxAge = 1h`: an older replayed event is acked *without* acting —
  re-entering a parent loop over a long-stale result is noise, not recovery;
- an ack failure **stops** the drain so the unacked tail is retried rather than
  skipped.

Acking means "the supervisor has taken responsibility for this signal", not "the
parent has consumed it" (a batched resurrection still lives in memory until it
flushes). The guarantee is at-least-once delivery; the existing
CorrelationID-based `seen` dedupe covers redelivery.

When the log is disabled the previous channel-driven path is preserved exactly.

## P7 — claim-lease dispatch + concurrency caps

### Task model + store CAS

`Task` gains `ClaimLock`, `ClaimedAt`, `ClaimExpires` plus `ClaimActive(now)` /
`ClearClaim()`. `store.Store` gains:

- `ClaimForDispatch(id, owner, expires) (bool, error)` — under the store lock:
  the task must exist, be **pending**, and hold no live claim. An **expired**
  lease is stolen (that is how a task stranded by a dispatcher crash becomes
  dispatchable again).
- `ReleaseClaim(id)` — cleanup; a missing task is not an error.

**Key subtlety found by the tests**: the check must refuse a live claim
*regardless of owner*. An owner-scoped check (`ClaimActive && ClaimLock != owner`)
looked right but is useless here — the racing dispatchers inside one process
share the orchestrator's instance id, so both would have won.

### `internal/mesnada/orchestrator/dispatch.go` (new)

- `admitAndClaim(task)` — the whole decision (caps, then the claim CAS) with no
  side effect beyond taking the claim. `tryDispatch` = `admitAndClaim` + `go
  startTask`. Separating them keeps "may this run?" auditable and testable
  without a goroutine mutating the task under the assertions.
- `admit`/`fits` — `MaxParallel` (orchestrator-wide) and `MaxPerEngine`.
  `inFlightCounts` counts running tasks **plus** pending tasks holding a live
  claim, so a burst of simultaneous readiness cannot overshoot.
- `dispatchNow(task, background)` — explicit operator/agent actions (foreground
  `Spawn`, `Relaunch`, `Retry`-of-pending) still take the claim but **bypass the
  caps**: someone asked for this task by name and silently queueing it would be
  surprising.
- `dispatchLoop`/`dispatchTick` — every `DispatchInterval` (10s): reclaim expired
  claims, then drain. Without the tick, a task deferred by a cap would wait for
  an unrelated completion to be reconsidered; the tick is what turns the cap into
  a queue instead of a dead end.
- `drainDispatchable() int` — priority-desc then oldest-first, and it samples
  capacity **once** per pass, tracking the tally locally as it hands tasks out
  (see "race" below).
- `reclaimExpiredClaims()` — only **pending** tasks: once a task is running its
  lifecycle belongs to the spawner, not the claim.

### Other behaviour fixed along the way

`startTask`'s spawn-failure and nil-manager branches used to set the task failed
and just `Save` it — no broadcast, no event, claim still held. Both now go
through `onTaskComplete`, so a task that dies at spawn signals like any other
terminal outcome.

## Configuration

| Key | Default | Notes |
|---|---|---|
| `Mesnada.Delegation.EventLogDisabled` | `false` (log ON) | inverted, see below |
| `Mesnada.Delegation.EventLogMaxEntries` | `5000` | compaction past 2× |
| `Mesnada.Orchestrator.MaxParallel` | `5` | **now actually enforced** |
| `Mesnada.Orchestrator.MaxPerEngine` | `0` | 0 = genuinely no per-engine cap |
| `Mesnada.Orchestrator.ClaimTTL` | `2m` | |
| `Mesnada.Orchestrator.DispatchInterval` | `10s` | |

`EventLogDisabled` is inverted for the same reason as the P6/P4 flags: viper's
nested-default shadowing turns a `true` default into a silent `false`, so the
zero value must be the safe/active behaviour.

New `normalizeMesnadaOrchestratorDefaults()` restores `MaxParallel`, `ClaimTTL`
and `DispatchInterval` when they unmarshal as zero. This matters *now*: while
`MaxParallel` was unenforced a shadowed 0 was harmless, but as a real cap it
would silently mean "unlimited" and the number the UI shows would be a lie. As
with the delegation caps, an explicit 0 reads as "use the default" — configure a
large value for effectively unlimited. `MaxPerEngine` is **not** normalized,
because 0 genuinely means "no cap".

Surfaces: TOML template (`init.go`), REST (`GET/PUT /api/v1/settings` — positive
flags on the wire, plus a new `config.UpdateMesnadaOrchestrator`), TUI settings
(2 delegation fields + 4 orchestrator fields), WebUI `GeneralSettings.tsx` +
types + store defaults + all 7 locales.

New metrics: `dispatches_deferred`, `claims_rejected`, `claims_reclaimed`
(nil-safe recorders, same landmine as P6/P4).

## Behaviour change to be aware of

`MaxParallel = 5` is now enforced. A fan-out wider than 5 queues instead of
spawning everything at once, and drains as slots free. This is the documented
intent of a setting users already believed was active, and it protects provider
quotas — but it *is* a visible change for anyone relying on the previously
unlimited behaviour. Raise `MaxParallel` to restore it.

## Verification

- `go build ./...`, `gofmt`, `npx tsc --noEmit` clean; all 7 locale JSONs valid
  and key-aligned (72 keys each).
- `go test ./...` — no failures.
- `go test -race` green (repeated 6×) on `./internal/mesnada/...`,
  `./internal/app`, `./internal/api`, `./internal/config`, `./pkg/mesnada/...`.
- New tests: `events_test.go` (seq, cursor semantics, reopen/replay, compaction
  clamping, corrupt line, nil log), `dispatch_test.go` (claim exclusivity incl.
  the same-owner case, expired-lease steal, cap deferral global + per-engine,
  in-flight counting, reclaim, drain ordering, terminal events, ack, cancel),
  `delegation_supervisor_events_test.go` (ordered drain + ack, unacked-only
  replay, non-terminal skip, stale-replay skip, unknown task, ack-failure stop,
  redelivery dedupe, disabled fallback), config shadowing tests.

### Data race found (pre-existing, NOT introduced — reported, not fixed)

`-race` surfaced a **pre-existing** data race in `FileStore`'s shared-pointer
model: `Get`/`List` hand out the live `*models.Task`, `matchesFilter`/`canStart`
read `task.Status` under the store lock, while spawners and `startTask` write
`task.Status` with no lock at all. Reproduced at HEAD (commit `6f118531`, in a
clean worktree) with `go test -race -count=5 ./internal/mesnada/orchestrator/`:
fails ~2 of 3 runs, in `TestCreateSwarm*`. Inside `CreateSwarm` it is a genuine
production race — a worker is dispatched into a goroutine, and the very next
`Spawn` (verifier) reads that worker's `Status` in `canStart`.

Two instances my own code added were fixed:
- `drainDispatchable` re-read occupancy per task, inspecting a task the same loop
  had just launched → capacity is now sampled once per pass and tracked locally;
- a P4 test read `ConsecutiveFailures` while the background start mutated it →
  it now asserts `guardRespawn` directly.

The swarm topology tests now saturate the cap (`blockDispatch`) so no goroutine
runs at all — they assert on the created graph, not on execution.

**Recommended follow-up**: make task mutation safe — either copy-on-read/write in
`FileStore` (requires auditing that every mutation is followed by `Save`) or a
task-level mutex with accessors. It is a change of its own, with a wide blast
radius across the spawners.

## Deliberately not ported from Hermes

- **PID / heartbeat reaper for running tasks.** Hermes reclaims running cards
  whose worker PID is dead. In Pando the in-process wait goroutine already
  detects child exit, and `recoverStaleTasks` covers restarts; a second reaper
  would race the wait goroutine into double completions (double breaker
  increment, double broadcast) for little gain.
- **`enforce_max_runtime`.** Killing a running child on a timer is destructive
  and deserves its own decision.
- **Multi-dispatcher / cross-IPC dispatch.** The claim CAS is the substrate for
  it, but Pando still has exactly one in-process dispatcher per store.

Still open from the analysis: a real `TaskStatusBlocked`/`Review` lane,
blackboard GC, `auto_decompose`.
