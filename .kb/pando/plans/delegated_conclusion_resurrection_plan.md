---
created_at: 2026-06-21T09:18:03.597128668Z
updated_at: 2026-06-21T09:19:46.672998611Z
tags:
    - plan
    - agent
    - mesnada
    - delegation
    - steering
    - orchestrator
    - architecture
    - tui
    - webui
    - config
    - projects
---
# Plan: Delegated-Task Conclusions + Agent-Loop Resurrection

Created: 2026-06-21. Status: PLAN (not yet implemented).
Author: analysis from user request + codebase review.

## 1. Framing (the organizing insight)

The user's request is NOT two new subsystems. It is **two generalizations of
mechanisms Pando already has**:

1. The "conclusion summary that re-enters the main loop" is exactly the existing
   **interactive agent-loop steering** mechanism, generalized. Pando already has an
   out-of-band inbox that injects messages between tool-call boundaries
   (`steeringQueue` + `Steer` + `drainSteeringInto`). A delegated subagent's
   conclusion is just **a second class of event for the same inbox** — not a new
   channel, a second producer for the queue we already drain.

2. The requirement "if the loop ended but work is still pending, restart it" turns
   the linear agent loop into a **supervisor with continuations**. The orchestrator
   (`internal/mesnada/orchestrator`) is already the data layer for that supervisor:
   it persists tasks, knows their lifecycle, and notifies subscribers on completion
   (`onTaskComplete`). We add correlation (task→parent session) and an
   event-driven "resurrection" that begins a fresh, system-initiated turn.

A second hard requirement from the user: **the subagent only emits a thin
`<pando:conclusion>` wrapper; the software fills in all the meta-information** it
already knows from how the task was launched (task id, engine, model, work_dir,
project path, parent session/task, instance). The model is not trusted to
re-state launch metadata correctly.

Everything must be **toggleable from the TUI and WebUI settings panels and
persisted to the TOML/JSON config file**.

## 2. What already exists in Pando (verified against the code)

### 2.1 Agent-loop steering (the feedback inbox) — REUSE, do not rebuild
- `internal/llm/agent/agent.go`:
  - `agent.steeringQueue map[string][]steeringMessage` guarded by `steeringMu`.
  - `Steer(sessionID, content, attachments...)` (agent.go:390) — queues if the
    session is busy, else returns `ErrSessionNotBusy`.
  - `PendingSteering`, `dequeueSteering`, `clearSteering`.
  - `drainSteeringInto(ctx, sessionID, msgHistory, eventCh)` (agent.go:447) —
    materializes queued items as persisted `message.User` via `createUserMessage`,
    appends to history, publishes `AgentEventTypeSteeringInjected`.
  - Drained at TWO safe boundaries inside `processGeneration`:
    1. agent.go:865 — after `FinishReasonToolUse`, tool_results already persisted.
    2. agent.go:893 — end of turn: instead of returning Done, inject + refit +
       `continue`.
  - End-of-turn `Done` return is agent.go:902. **This is the resurrection hook.**
  - `Run` goroutine defer clears steering (agent.go:635); `Cancel` clears it
    (agent.go:368). `IsSessionBusy` = presence in `activeRequests` (agent.go:504).
- Feature doc: `pando/features/agent_loop_steering.md`. Plan:
  `pando/plans/agent_loop_steering_plan.md`. Always-on, no config flag yet.

### 2.2 Mesnada orchestrator (the supervisor data layer) — EXTEND
- `internal/mesnada/orchestrator/orchestrator.go`:
  - `Orchestrator` holds `store store.Store` (JSON `FileStore`, NOT sqlite),
    `manager *agent.Manager`, `subscribers map[string][]chan *models.Task`.
  - `Spawn` (orchestrator.go:275) creates+persists+starts a task.
  - `onTaskComplete(task)` (orchestrator.go:151) saves final state, notifies
    `subscribers[task.ID]`, then `processDependentTasks`. **Single choke point
    for completion** — the resurrection subscriber hooks here.
  - `Wait`/`WaitMultiple` (orchestrator.go:420/480) — bounded blocking waits.
- `pkg/mesnada/models/task.go`:
  - `Task` already has `Output`, `OutputTail`, `Error`, `ExitCode`, `Engine`,
    `Model`, `WorkDir`, `Status`, `CreatedAt/StartedAt/CompletedAt`, `Tags`,
    `Persona`, `ACPSessionID`. **No** parent-session correlation, **no**
    conclusion concept yet.
  - Terminal states: completed/failed/cancelled/paused (`IsTerminal`).

### 2.3 Mesnada spawn tools — EXTEND
- `internal/llm/tools/mesnada.go`: `MesnadaSpawnTool` (+ get/list/wait/cancel/
  output). `Run` (mesnada.go:153): background tasks are fire-and-forget, returns
  `{task_id, status, work_dir, created_at}` immediately; foreground returns
  `output_tail`/`exit_code`.
  - **Key fact**: the tool ctx already carries the parent session id via
    `ctx.Value(tools.SessionIDContextKey)` (set in agent.go:1070 before every tool
    Run). So we can correlate task→session with **no signature change**.

### 2.4 Config plumbing — pattern to follow
- `internal/config/config.go`: `MesnadaConfig` (config.go:247) with
  `Orchestrator`/`ACP`/`TUI` sub-structs; `InternalToolsConfig` (config.go:451)
  uses `*Enabled`/`*Disabled` bool fields. `UpdateMesnada` (config.go:3079) and
  `UpdateInternalTools` (config.go:3136) persist with rollback on failure.
- App wiring `internal/app/app.go`: orchestrator created (app.go:481) BEFORE
  `CoderAgent` (app.go:577), which receives the orchestrator via
  `CoderAgentToolsWithMesnada(app.MesnadaOrchestrator, ...)`. Good: a supervisor
  can hold references to both.

### 2.5 IPC / inter-instance (the "peer" transport) — OPTIONAL later
- `internal/ipc/*` (ZMQ PUB/SUB + ROUTER/DEALER, JSON-RPC 2.0), discovery via
  `internal/instanceregistry`. Relevant only for the "hot peer instance"
  optimization (Phase 7). The CLI/ACP spawner path (orchestrator) is the MVP.

## 3. How the initial analysis maps onto Pando (review)

The user's initial analysis is sound; the corrections vs. Pando reality:

- ❌ "Define a new `Delegate` interface unifying CLI + peer." → ✅ Pando already
  has `internal/mesnada/agent/spawner_interface.go` + `Manager`, and the
  orchestrator wraps them. **Reuse the orchestrator as the supervisor; do not
  introduce a parallel abstraction.**
- ❌ "Persist a delegation registry in SQLite." → ✅ The orchestrator already
  persists tasks in a JSON `FileStore`. **Add fields to `Task`** and a
  session-correlation index; do not add a second store.
- ✅ "Conclusion = sentinel-delimited block; software guarantees capture via a
  fallback." → matches; we parse `<pando:conclusion>`, enrich with launch
  metadata, and synthesize from `OutputTail` when the block is absent.
- ✅ "Two re-entry cases (loop alive vs. loop ended)." → Case A = inject into the
  steering inbox; Case B = event-driven resurrection off `onTaskComplete`.
- ✅ "Conclusions carry pointers, not dumps; budgets/caps; idempotency via
  correlation id; resurrection turn as first-class." → adopted.
- ✅ "Hot-vs-cold peer selection" → deferred to optional Phase 7 (now mapped onto
  the existing Projects child-instance machinery — see Section 10).

## 4. The conclusion contract (software-filled metadata)

The subagent is instructed (via an injected brief snippet) to end its run with a
single delimited block carrying ONLY what the model actually knows:

```
<pando:conclusion>
status: success | partial | failed | blocked
summary: <2-4 sentences>
artifacts: [paths or kb doc-ids it produced]
memory_refs: [kb doc-ids / memory keys it wrote]
follow_up: <what remains, or nil>
confidence: 0.0-1.0
</pando:conclusion>
```

The **software fills the rest** from the task record it owns (task id, engine,
model, work_dir, project id+path+name, parent_session_id, parent_task_id,
instance id, started/completed timestamps, exit code, duration). The model never
re-states those — eliminating a class of hallucinated metadata.

**Capture guard (guaranteed by code, not goodwill):**
1. Scan the captured stdout / final ACP message for the sentinel block (CLI: tail
   of `Output`; ACP: last agent message).
2. If absent and the task succeeded → synthesize a conclusion from `OutputTail`
   (deterministic truncation + optional cheap-model summarization, reusing the
   evaluator's cheap model from `EvaluatorConfig` if enabled).
3. If failed/cancelled → synthesize a `failed`/`blocked` conclusion from
   `Error`/exit code.

`Conclusion` is enriched and stored on the `Task`, so it is durable and re-fetchable.

## 5. Target data model

`pkg/mesnada/models/task.go` additions:

```go
type Conclusion struct {
    Status      string    `json:"status"`            // success|partial|failed|blocked
    Summary     string    `json:"summary"`
    Artifacts   []string  `json:"artifacts,omitempty"`
    MemoryRefs  []string  `json:"memory_refs,omitempty"`
    FollowUp    string    `json:"follow_up,omitempty"`
    Confidence  float64   `json:"confidence,omitempty"`
    Synthesized bool      `json:"synthesized,omitempty"` // true if no block emitted
    CapturedAt  time.Time `json:"captured_at"`
}

// added to Task:
ParentSessionID string      `json:"parent_session_id,omitempty"`
ParentTaskID    string      `json:"parent_task_id,omitempty"` // for depth/trace
CorrelationID   string      `json:"correlation_id,omitempty"` // idempotency
ProjectID       string      `json:"project_id,omitempty"`     // resolved from work_dir
ProjectPath     string      `json:"project_path,omitempty"`   // CanonicalProjectPath
Conclusion      *Conclusion `json:"conclusion,omitempty"`
Depth           int         `json:"depth,omitempty"`          // anti-fork-bomb cap
```

## 6. Config (toggle + persist) — `MesnadaDelegationConfig`

Add to `MesnadaConfig` (config.go:247) a new sub-struct (env override
`PANDO_DELEGATION_*`):

```go
type MesnadaDelegationConfig struct {
    // Master switch for conclusion capture + re-entry. Off => current behavior.
    Enabled              bool   `json:"enabled,omitempty"`
    // Case A: inject conclusions into a still-running parent loop.
    InjectIntoLiveLoop   bool   `json:"injectIntoLiveLoop,omitempty"`
    // Case B: resurrect an idle parent session when a correlated task completes.
    ResurrectIdleLoop    bool   `json:"resurrectIdleLoop,omitempty"`
    // Synthesize a conclusion when the subagent omits the block.
    SynthesizeFallback   bool   `json:"synthesizeFallback,omitempty"`
    // Caps (anti-fork-bomb / cost control).
    MaxResurrections     int    `json:"maxResurrections,omitempty"`   // per session per turn-chain
    MaxDepth             int    `json:"maxDepth,omitempty"`           // delegated-of-delegated
    MaxConcurrent        int    `json:"maxConcurrent,omitempty"`      // outstanding per parent
    ResurrectionTimeout  string `json:"resurrectionTimeout,omitempty"`// how long to wait for pending
}
```

Wire: `UpdateMesnadaDelegation(...)` mirroring `UpdateMesnada`; defaults
(Enabled=false to preserve today's behavior; MaxResurrections=4, MaxDepth=3,
MaxConcurrent=8, ResurrectionTimeout=10m). Hot-reload via existing config update
path so TUI/WebUI changes take effect without restart.

## 7. Phased implementation

### Phase 0 — Foundations: correlation + config + model (no behavior change)
- Add `Conclusion` + correlation/depth/project fields to
  `pkg/mesnada/models/task.go`.
- `MesnadaSpawnTool.Run`: read `ctx.Value(tools.SessionIDContextKey)`; set
  `SpawnRequest.ParentSessionID` + generate `CorrelationID`; carry parent task id
  + `Depth = parent.Depth+1` when the spawner itself is a delegated task.
- Extend `SpawnRequest` + `Orchestrator.Spawn` to persist these and to resolve
  `work_dir → CanonicalProjectPath → project registry` into `ProjectID/ProjectPath`.
- Add `MesnadaDelegationConfig` + `UpdateMesnadaDelegation` + defaults + env.
- Store query: `Store.ListByParentSession(sessionID)` and an in-memory index
  `subscribersBySession` alongside `subscribers` (by task id).
- Tests: model round-trip, correlation propagation, project resolution, config
  persist/rollback.
- **Verify**: `go test ./internal/llm/tools ./internal/config ./internal/mesnada/...`

### Phase 1 — Conclusion protocol: brief injection + capture/enrichment
- Brief snippet: when `Delegation.Enabled`, the orchestrator prepends to the
  subagent prompt the instruction to close with the `<pando:conclusion>` block
  (only the model-known fields). Centralize in
  `internal/mesnada/orchestrator` (or `agent/manager`) prompt assembly.
- Parser `internal/mesnada/conclusion/parse.go`: extract+validate the YAML body
  from the sentinel; tolerant (missing fields default).
- Enricher: in `onTaskComplete`, build `Conclusion` from the parsed block +
  task-owned metadata (incl. resolved project name/id); on absence run the
  fallback synthesis (gated by `SynthesizeFallback`); store on `Task`; persist.
- Tests: parse happy/missing-block/partial; enrichment fills metadata;
  failed-task synthesis.
- **Verify**: `go test ./internal/mesnada/...`

### Phase 2 — Case A: inject conclusion into a LIVE parent loop
- Generalize the steering inbox to carry a typed payload:
  `steeringMessage{ kind: feedback|conclusion, content, attachments, meta }`.
  (Keep the existing user-feedback path identical.)
- New `agent.Service` method `InjectConclusion(sessionID, formatted, meta)` that
  enqueues a `kind=conclusion` message; `drainSteeringInto` renders it as a
  persisted `message.User` framed as:
  `[delegated-result task=… agent=… project=… status=…]\nsummary: …\nmemory_refs: …`
  (pointers, not dumps — the parent lazily fetches detail via get_task/kb).
- New event types `AgentEventTypeConclusionQueued/Injected` (distinct UI framing).
- Subscriber: when `Delegation.Enabled && InjectIntoLiveLoop`, a supervisor
  (owned by app, subscribing to `onTaskComplete`) checks
  `CoderAgent.IsSessionBusy(parentSessionID)`; if busy → `InjectConclusion`.
- Idempotency: dedupe by `CorrelationID` so a task reported via both CLI tail and
  IPC is injected once.
- Tests: busy session receives one injection at a safe boundary; never between
  tool_call/tool_result; dedupe.
- **Verify**: `go test ./internal/llm/agent ./internal/mesnada/...`

### Phase 3 — Case B: event-driven resurrection of an IDLE parent loop
- Supervisor goroutine (new `internal/mesnada/supervisor` or method on app) that
  subscribes to the orchestrator completion stream. On a correlated task
  completing while `!IsSessionBusy(parentSessionID)` and
  `Delegation.ResurrectIdleLoop`:
  - Check there are no more outstanding correlated tasks OR wait up to
    `ResurrectionTimeout` to batch sibling conclusions (join policy below).
  - Start a **new** agent run via a first-class entrypoint
    `agent.Resume(ctx, sessionID, ResumeReason{Kind: DelegatedConclusion, ...})`
    that injects the conclusion(s) as the opening turn, framed:
    "You are resuming because task X reported: …". This is distinct from a
    user-initiated `Run` and from system steering — the model reasons better
    about *why* it woke.
- Loop-end change: at agent.go:902, before returning `Done`, if
  `Delegation.Enabled` and there are outstanding correlated tasks for this
  session, do NOT mark a hard end — record intent so the supervisor will resurrect
  (or, if `MaxResurrections` exhausted/budget spent, return Done normally).
- Caps enforced here: `MaxResurrections` (per turn-chain), `MaxDepth`
  (delegated-of-delegated, via `Task.Depth`), `MaxConcurrent`.
- Persistence/recovery: on restart, the orchestrator reconciles outstanding
  correlated tasks (it already recovers stale tasks) and can resurrect or mark
  for reconciliation.
- Tests: idle session resurrected exactly once per completion batch; depth cap
  blocks fork-bomb; budget exhaustion returns Done; no double-resurrection.
- **Verify**: `go test ./internal/llm/agent ./internal/mesnada/... ./internal/app`

### Phase 4 — Agent-directed waiting (`await_task` / `await_any`)
- Distinct from system resurrection: a tool the MODEL calls when it knows it
  cannot proceed without a result. Thin wrapper over `Orchestrator.Wait`/
  `WaitMultiple` returning the enriched `Conclusion` (pointers, not full output).
- Join policies surfaced as params: first-to-finish | all | quorum(n) |
  best-of-N(by confidence). `Cancel` losers on best-of-N to stop paying for
  unneeded work.
- Tests: each join policy; cancellation of losers.

### Phase 5 — Config UI (TUI + WebUI + API + i18n + persistence)
- Backend API: extend settings handlers (`internal/api/handlers_settings.go`) to
  read/write `MesnadaDelegationConfig` via `UpdateMesnadaDelegation`.
- TUI: new fields in `internal/tui/page/settings.go` (toggles + numeric caps),
  following the existing General settings pattern.
- WebUI: fields in `web-ui/src/components/settings/` + `settingsStore.ts` +
  `types/index.ts`; i18n keys in all `web-ui/src/i18n/locales/*.json`.
- Confirm hot-reload: changing a flag updates the live supervisor/agent behavior
  without restart.
- Tests: API round-trip; web-ui typecheck.
- **Verify**: `go test ./internal/api`; `cd web-ui && npm run typecheck`.

### Phase 6 — Tests, docs, end-to-end
- E2E: spawn a delegated `engine=pando` task in a temp project, assert the
  conclusion is captured+enriched, Case A injection while parent busy, Case B
  resurrection while parent idle, caps respected.
- Update README / docs. Save a `pando/features/...` summary doc on completion
  (mandatory per project workflow).

### Phase 7 (OPTIONAL) — Warm per-project instance reuse (hot vs cold)
- When the target `work_dir` canonicalizes to a registered project whose child
  ACP instance is `running` (and not `External`), delegate into that warm instance
  (index already in memory) instead of cold-spawning a CLI subprocess; receive the
  conclusion as a typed message (no stdout scan). Else cold CLI/ACP task. The
  heuristic lives in the supervisor, consulting the project `Manager`/global
  registry + `internal/instanceregistry`, transparent to the model. See Section 10.

## 8. Risks / decisions to confirm with the user before building
- **Default-off**: Phase 0 ships with `Delegation.Enabled=false` to preserve
  today's fire-and-forget semantics; opt-in via settings. (Recommended.)
- **Resurrection vs. inline only**: support both, each behind its own flag
  (`InjectIntoLiveLoop`, `ResurrectIdleLoop`).
- **Cost**: resurrection starts a new billed turn; caps + budgets are mandatory,
  not optional.
- **Store**: stay on the orchestrator's JSON `FileStore`; do not add sqlite.
- **Project targeting**: path-based + registry-for-metadata for MVP; warm-instance
  reuse / target-by-id deferred to Phase 7 (see 10.4).

## 9. Touch list (anticipated)
- `pkg/mesnada/models/task.go` (Conclusion + correlation + project fields)
- `internal/mesnada/orchestrator/orchestrator.go` (Spawn, onTaskComplete, store
  index, brief injection, project resolution, supervisor hook)
- `internal/mesnada/conclusion/` (new: parse + enrich + synthesize)
- `internal/mesnada/supervisor/` (new: resurrection subscriber) OR app method
- `internal/llm/agent/agent.go` (typed steering payload, InjectConclusion,
  Resume entrypoint, loop-end resurrection gate)
- `internal/llm/tools/mesnada.go` (parent correlation; await_task/await_any)
- `internal/config/config.go` (MesnadaDelegationConfig + UpdateMesnadaDelegation)
- `internal/api/handlers_settings.go` + routes (settings API)
- `internal/tui/page/settings.go` (TUI toggles)
- `web-ui/src/components/settings/*`, `settingsStore.ts`, `types/index.ts`,
  `i18n/locales/*.json` (WebUI toggles + i18n)
- `internal/app/app.go` (wire supervisor)
- `internal/project/service.go` + `internal/config/global_projects.go`
  (project resolution helper used by the conclusion enricher; Phase 7 warm reuse)

## 10. Projects ↔ subagents linkage (gap analysis, added 2026-06-21)

User question: "how are Pando projects listed (a path + folder name are stored)?
does the Projects section link to per-project subagent launching?"

### 10.1 What Projects actually is (verified)
- `internal/project/` + `internal/config/global_projects.go`. **SQLite-backed**
  (migration `20260419120000_add_projects.sql`): table `projects(id, name, path
  UNIQUE, status, initialized, acp_pid, acp_port, last_opened, created_at,
  updated_at)`. (Note: this is sqlite, UNLIKE mesnada tasks which use a JSON
  FileStore.)
- `project.Project` struct mirrors that + a computed non-persisted `External`
  flag (true when the project's ACP instance was launched by another app, e.g. an
  editor, so it cannot be stopped from here).
- **Name** defaults to `filepath.Base(path)` (service.go:66) — exactly the folder
  name, as the user said.
- **Path** is normalized through `config.CanonicalProjectPath` (global_projects.go):
  expands `~/`, makes absolute, `filepath.EvalSymlinks` → single source of truth so
  a directory reachable via a symlink is never registered twice. Mirrored in a
  cross-instance **global registry** under the user config dir.
- A registered project can spawn a **long-lived child `pando acp` process**
  (`Manager.spawnChild`, manager.go:186: `exec ... "pando","acp"` with
  `cmd.Dir = proj.Path`), tracked by `acp_pid`/`acp_port`, with lifecycle
  start/stop/status. This is the Projects panel.

### 10.2 The gap: Projects and mesnada subagents are DISCONNECTED at model level
Confirmed by grep — there are **zero** references between the two subsystems:
- The mesnada orchestrator/spawn path does **not** import or query
  `internal/project`. `Orchestrator.Spawn` (orchestrator.go:277) takes a free-form
  `work_dir` (defaults to `"."`); it is never resolved against the projects
  registry. `engine=pando` → cold CLI subprocess `pando -p` in `work_dir`;
  `engine=acp-*` → fresh ACP agents.
- The spawn tool does not validate `work_dir` against registered projects, nor
  reuse a project's already-running child ACP instance.
- The ONLY existing coupling is **implicit, at the IPC layer**: a pando subprocess
  started in the same canonical path as a running primary becomes an IPC secondary
  and proxies DB writes via dbproxy (single-writer SQLite). The orchestrator does
  not exploit this deliberately for delegation.

### 10.3 Impact on this plan
1. **The conclusion `project` field has no source today.** The orchestrator only
   knows `work_dir`. To populate `project` (the user's explicit "software fills
   the metadata" requirement) the enricher MUST resolve
   `work_dir → CanonicalProjectPath → projects registry lookup` to get the
   canonical path + project id + folder name. If not registered, fall back to the
   canonical path and `filepath.Base` as the display name.
   → **Adjust Phase 0/1**: add a small project-resolution helper and call it in the
   enricher; store `Task.ProjectID` + `Task.ProjectPath` (canonical) alongside the
   existing `WorkDir`.
2. **Identity/correlation should key off the canonical project path**, not the raw
   `work_dir`, so "you are agent X of project Y" and depth/trace are stable across
   symlinks and relative paths. Use `CanonicalProjectPath` when building the brief
   and the `CorrelationID` scope.
3. **Hot-vs-cold selection (Phase 7) becomes concrete.** Pando ALREADY has
   long-lived per-project child ACP instances (Projects panel) and IPC
   primary/secondary discovery. The "hot peer" optimization = when the target
   `work_dir` canonicalizes to a project whose child ACP instance is `running`
   (and not `External`), delegate into that warm instance (index already in
   memory) instead of cold-spawning a CLI subprocess. The orchestrator would
   consult the project `Manager`/`global registry` to decide.
   → **Phase 7 rewrite**: "reuse a running per-project child ACP instance / IPC
   primary" replaces the generic ZMQ-peer description; same idea, but it maps onto
   existing Pando machinery rather than something to invent.
4. **Optional UX tie-in (new, low priority)**: the Projects panel could surface
   delegated tasks grouped by project (since both can now share a canonical-path
   key), and a spawn could optionally target a registered project by id instead of
   a raw path. Track as a stretch goal, not MVP.

### 10.4 Open decision for the user
Should a delegated spawn be allowed to **target a registered project by id** (and
reuse its warm child instance when running), or stay path-based with the registry
used only to *resolve metadata* for the conclusion? MVP recommendation:
path-based + registry-for-metadata now (Phases 0–6); warm-instance reuse as the
opt-in Phase 7.
