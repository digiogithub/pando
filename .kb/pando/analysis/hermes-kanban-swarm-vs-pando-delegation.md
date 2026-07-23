---
created_at: 2026-07-23T13:32:45.300756033Z
updated_at: 2026-07-23T13:32:45.300756033Z
tags:
    - analysis
    - mesnada
    - delegation
    - orchestration
    - hermes
    - kanban
    - coordination
---
# Hermes Kanban Swarm vs Pando mesnada delegation — coordination analysis

Comparison of Hermes Agent's **Kanban Swarm** ("Kanban multi-agent collaboration") against
Pando's current subagent/delegation stack, with concrete improvement proposals for Pando —
focused on **task-coordination** concepts. Related: [[delegated_conclusions_resurrection]],
[[delegation_future_improvements]], [[inter_instance_communication_plan]].

Source (read on 2026-07-23):
- `hermes-agent/hermes_cli/kanban_db.py` (~9.7k LOC durable kernel)
- `hermes-agent/hermes_cli/kanban_swarm.py` (swarm topology helper)
- `hermes-agent/gateway/kanban_watchers.py` (dispatcher/notifier loops)
- `hermes-agent/tools/kanban_tools.py` (worker-facing tools)

Pando side:
- `pkg/mesnada/models/task.go`, `internal/mesnada/orchestrator/orchestrator.go`,
  `internal/mesnada/agent/spawner*.go`, `internal/llm/tools/mesnada*.go`

---

## 1. How Hermes Kanban Swarm works

### 1.1 Durable kanban kernel (the substrate)
Everything is one per-board **SQLite** database (`tasks`, `task_links`, `task_runs`,
`task_comments`, `task_events`, `kanban_notify_subs`). No in-memory scheduler; state is
durable and inspectable. Multiple boards coexist; a dashboard, notifier, slash command and
dispatcher all read/write the same rows.

**Task state machine** (`VALID_STATUSES`):
`triage → todo → scheduled → ready → running → blocked → review → done → archived`
- `triage`: raw idea, not yet a workgraph.
- `todo`: has undone parents (waiting on dependencies).
- `ready`: all parents `done`/`archived`, dispatchable, no claim yet.
- `running`: claimed by a worker (holds a lease).
- `blocked`: worker- or breaker-initiated stop; needs `unblock`.
- `review`: awaiting a verifier/human gate (separate claim lane).
- `done`/`archived`: terminal.

### 1.2 Dependency graph + readiness
`task_links(parent_id, child_id)` is a DAG with **cycle detection** (`_would_cycle`).
`recompute_ready()` promotes `todo`/`blocked → ready` **only when every parent is
`done`/`archived`**. The promotion is the single source of readiness truth and runs each
dispatcher tick. `claim_task` re-checks the invariant atomically and demotes back to `todo`
if a racy writer promoted a task with undone parents.

### 1.3 Claim-based atomic dispatch (leases)
Dispatch is **pull + lease**, not push:
- `claim_task`: `UPDATE ... SET status='running', claim_lock=?, claim_expires=? WHERE
  status='ready' AND claim_lock IS NULL` — optimistic CAS; `rowcount != 1` means someone
  else won. This makes **multiple concurrent dispatchers safe** (`_dispatch_tick_lock` is a
  board-scoped non-blocking file lock so two dispatchers on the same DB serialize; different
  boards run in parallel).
- `heartbeat_claim` extends the lease; `release_stale_claims` reclaims expired leases;
  `detect_crashed_workers` reclaims running tasks whose host-local **PID is dead**;
  `enforce_max_runtime` times out over-budget workers. All reset the task to `ready`.

### 1.4 Dispatcher tick (`dispatch_once` / `_dispatch_once_locked`)
Under the board lock, one tick does, in order:
1. `reap_worker_zombies()` + `release_stale_claims` (TTL) + `detect_stale_running`
   (heartbeat) + `detect_crashed_workers` (PID) + `enforce_max_runtime`.
2. `recompute_ready` (promote todo→ready).
3. For each `ready`, unclaimed task ordered by `priority DESC, created_at ASC`: check
   `check_respawn_guard`, then `claim_task` + `spawn_fn`, recording `worker_pid`.

**Concurrency caps**: `max_spawn` is a *live* cap (counts `status='running'` + this tick's
spawns, not a per-tick budget), plus `max_in_progress` (board-wide) and
`max_in_progress_per_profile` (per-assignee — stops one fan-out melting a single model's
quota while others idle).

### 1.5 Circuit breaker + respawn guard (anti-thrash)
- `consecutive_failures` per task; after `failure_limit` (per-task `max_retries` override)
  the task is **auto-blocked** with the last error as reason. `recompute_ready` refuses to
  auto-recover a task at the failure limit (breaker accumulates across recovery cycles).
- **Sticky block**: a worker-initiated `kanban_block` stays blocked until an explicit
  `kanban_unblock` (won't silently auto-recover).
- `check_respawn_guard` defers (not fails) a respawn this tick for: `rate_limit_cooldown`
  (last run hit a quota wall), `blocker_auth` (error matches quota/auth regex),
  `recent_success` (a completed run exists inside a window — wait for review), `active_pr`
  (a recent comment has a PR URL — avoid duplicate PRs).

### 1.6 Signals = append-only event log + notifier
`task_events` is an append-only per-task log (`completed`, `blocked`, `gave_up`, `crashed`,
`timed_out`, `promoted`, `claim_rejected`, `unblocked`, `status`, `heartbeat`). The
**notifier watcher** polls subscriptions and delivers terminal events to users via
`claim_unseen_events_for_sub` — a **cursor-based, atomically-advanced** dedup so a crash →
reclaim → re-crash still notifies each time, and delivery is idempotent. Subscriptions are
dropped only on truly-final status (`done`/`archived`) or after N consecutive send failures.

### 1.7 Blackboard = shared coordination state (the swarm's core idea)
`kanban_swarm.py` deliberately **adds no second scheduler**. It writes a small graph into the
existing kernel:
```
planning root (completed immediately — stays the shared blackboard + audit anchor)
  ├─ parallel specialist workers (ready)
  └─ verifier (todo until all workers done)
       └─ synthesizer (todo until verifier passes)
```
The **blackboard** is low-tech: structured JSON comments on the root task
(`[swarm:blackboard] {"key":..,"value":..}`). `latest_blackboard` merges them
**last-write-wins per key**, recording the winning author (`_authors`) for traceability.
Because it lives in `task_comments`/`task_events`, the dashboard, notifier, dispatcher and
slash command keep working with zero new services. Workers are told (in their injected body)
to read sibling/parent handoffs and put machine-readable facts in completion metadata,
cross-worker notes on the root.

**Verifier gate**: the verifier completes only with metadata `{"gate":"pass"}` when evidence
is sufficient, else **blocks with the exact missing work**. The synthesizer's parent is the
verifier, so it cannot start until the gate passes.

### 1.8 Worker context handoff (`build_worker_context`)
A spawned worker receives a bounded, structured prompt: task body → attachments →
**prior attempts on this task** (runs with summary/error/metadata) → **structured handoff
results of every done parent** (prefers `run.summary`/`run.metadata`, falls back to
`task.result`) → cross-task role history → comment thread. Every field is byte-capped so
pathological boards can't blow the context.

### 1.9 Integrity gates
- `_verify_created_cards` / `_scan_prose_for_phantom_ids` raise `HallucinatedCardsError` when
  a completing agent references task IDs that don't exist — an **anti-hallucination completion
  gate**.
- `ArtifactPreservationError` guards scratch-workspace artifacts on completion.
- Idempotency keys on `create_task` (a repeated swarm creation recovers the existing topology
  from the root blackboard instead of duplicating the graph).
- `auto_decompose` (triage → workgraph via aux LLM, capped per tick) and `specify` turn a raw
  goal into a ready graph before fan-out.

---

## 2. How Pando does it today

**Task model** (`pkg/mesnada/models/task.go`) — 6 flat states:
`pending → running → {completed | failed | cancelled | paused}`. `Task.Dependencies []string`
is a DAG by ID. Rich per-task `Conclusion` (status success/partial/failed/blocked, summary,
artifacts, memory_refs, follow_up, confidence, synthesized). Correlation ID (idempotency),
parent session/task linkage, `Depth` (anti-fork-bomb cap).

**Orchestration** (`internal/mesnada/orchestrator/orchestrator.go`) — **push, in-process**:
- No dispatcher tick and no lease. `onTaskComplete` → `processDependentTasks` lists `pending`
  tasks, and for each whose deps are all `completed` (`canStart`), calls `go o.startTask`.
  Readiness is evaluated event-driven on each completion, not by a polling scheduler.
- `startTask` tries a **warm per-project ACP instance** (`tryStartWarm`), else cold-spawns a
  CLI via a per-engine spawner (copilot/claude/gemini/opencode/mistral/pando/warm-acp).
- `recoverStaleTasks` on startup + external-reattach recovery for delegations whose process
  was lost.

**Delegation feedback** ([[delegated_conclusions_resurrection]]): subagent emits a thin
`<pando:conclusion>` sentinel; orchestrator enriches it with owned launch metadata. Global
`SubscribeCompletions` bus drives **Case A** (inject conclusion into a live parent loop) and
**Case B** (idle **resurrection** of a parent that ended its turn via `mesnada_await`).
`getDependencyLogs` injects the **last N log lines** of dependency tasks into a dependent's
prompt.

**Metrics**: warm hits/failures, cold fallbacks, cap rejections, resurrections, live
injections, external-reattach recovered/failed.

---

## 3. Side-by-side (coordination lens)

| Concept | Hermes Kanban Swarm | Pando mesnada |
|---|---|---|
| State model | 9 states incl. `blocked`/`review`/`triage`/`scheduled` | 6 flat states, no block/review/gate |
| Readiness | polling `recompute_ready`, re-checked atomically on claim | event-driven `processDependentTasks` on each completion |
| Dispatch | pull + **claim lease** (CAS), multi-dispatcher safe | push, single in-process owner, no lease |
| Crash recovery | TTL/heartbeat/PID/max-runtime reclaim → back to `ready` | startup `recoverStaleTasks` + external reattach; no live heartbeat/PID reclaim loop |
| Anti-thrash | circuit breaker + `check_respawn_guard` (rate-limit/auth/recent-success/active-PR) | task just → `failed`; no retry backoff/guard |
| Shared state | **blackboard** = merged JSON comments on root, last-write-wins + authors | none — deps get log-tail only, no sibling channel |
| Handoff to dependents | structured parent `run.summary`/`metadata` | last-N **log lines** (Conclusion NOT fed forward) |
| Signals | durable append-only `task_events` + cursor-dedup notifier | in-memory channels, non-blocking drop-on-full |
| Verify/synthesize | first-class swarm topology + verifier **gate** | plain DAG, no gate/verifier primitive |
| Concurrency caps | live `max_spawn` + `max_in_progress` + **per-profile** | `Priority` field exists; no global scheduler enforcing caps |
| Integrity gate | phantom-task-id hallucination gate on completion | conclusion parsed/soft-validated, no existence gate |
| Planning | `auto_decompose` / `specify` (goal → graph) | parent enumerates deps manually |

Where Pando is **already ahead**: richer per-task `Conclusion` object (confidence, memory
refs, follow-up), warm-instance reuse, live-injection + resurrection (Case A/B) — Hermes has
no "resurrect a parent turn" equivalent; it relies on the durable board + notifier instead.

---

## 4. Proposed improvements for Pando (ranked, coordination-first)

### P1 — Sibling **blackboard** for parallel delegations (highest value, low cost)
Adopt Hermes' low-tech idea directly. When a parent fans out N sibling tasks, give them a
shared **root/coordination key** (reuse `CorrelationID` or a new `SwarmID`). Add a
`mesnada_note` tool that appends structured `{key,value,author}` facts to the root task and a
merged `latest_blackboard` reader, so siblings exchange machine-readable facts (chosen
interfaces, file ownership, decisions) without a new service. Store in the existing task store
(a `Notes []BlackboardEntry` slice or a comments table). This is the single biggest missing
coordination primitive: Pando siblings currently cannot talk to each other at all.

### P2 — Feed **structured conclusions forward** to dependents (not just log tails)
Pando already captures a rich `Conclusion` per task but only injects raw log tails into
dependents (`getDependencyLogs`). Wire each dependency's `Conclusion` (summary + artifacts +
follow_up + memory_refs) into the dependent's spawn prompt, mirroring Hermes'
`build_worker_context` parent-handoff block. Nearly free — the data already exists.

### P3 — **Verifier gate + swarm topology helper** (`mesnada_swarm`)
A helper that, from a goal + worker specs, builds the DAG: parallel workers → verifier →
synthesizer, on top of the existing `Dependencies`. Represent the gate with a lightweight
`blocked`/gate concept: the verifier's conclusion `status` (`success` vs `blocked`) decides
whether the synthesizer's dep is considered satisfied. Add `TaskStatusBlocked` +
`TaskStatusReview` and teach `canStart` about a gate predicate. Turns ad-hoc fan-out into a
repeatable fan-out/verify/merge pattern.

### P4 — **Circuit breaker + respawn guard** for delegated retries
Add `ConsecutiveFailures` + `MaxRetries` to `Task`; after the limit, park the task
(`blocked`) instead of silently `failed`, and defer respawns on rate-limit/auth errors and
recent success. Prevents thrash when a delegated task is unfixable or quota-walled. Pairs with
Pando's existing metrics.

### P5 — Durable **event log** for delegation signals
Pando's completion bus is in-memory with drop-on-full sends. A small append-only
`task_events` table + cursor-based delivery would make Case A/B injection and any future
notifier **crash-safe and idempotent** (a resurrection after a Pando restart could replay
unseen terminal events). Aligns with the existing IPC/ZMQ event plane
([[inter_instance_communication_plan]]).

### P6 — **Anti-hallucination conclusion gate** (cheap correctness win)
Validate a completing subagent's `Conclusion.Artifacts`/`MemoryRefs` actually exist (files on
disk, memory keys resolvable) before accepting `status=success`; downgrade to `partial` +
warning otherwise. Mirrors `HallucinatedCardsError`. Directly hardens the delegation feedback
loop Pando already depends on.

### P7 (optional, larger) — **claim-lease dispatch + per-profile caps**
Only worth it if Pando moves delegation to a durable/multi-dispatcher model (e.g. shared
across IPC peers). Introduce a `ready` state + claim CAS + heartbeat/PID reclaim, plus
`max_in_progress_per_engine`. Given Pando's current single-in-process orchestrator + warm
instances + IPC bus, P1–P6 deliver most of the coordination value without this rewrite.

**Recommended first slice**: P1 (blackboard) + P2 (conclusion forwarding) + P3 (swarm/verifier
helper) — together they give Pando true multi-agent *collaboration* (siblings coordinate,
outputs verified and merged) reusing existing infrastructure, no durable-scheduler rewrite.

---

## 5. Verification
Analysis only — no code changed. Findings cross-checked by reading the cited Hermes source
(state constants, `dispatch_once`, `claim_task`, `recompute_ready`, `check_respawn_guard`,
`build_worker_context`, `kanban_swarm.create_swarm`/`latest_blackboard`) and the Pando
orchestrator (`onTaskComplete`, `processDependentTasks`, `canStart`, `startTask`) plus
`models/task.go`. hermes-agent code index was still `in_progress`, so Hermes reads were done
directly on disk at `/www/MCP/Pando/hermes-agent`.
