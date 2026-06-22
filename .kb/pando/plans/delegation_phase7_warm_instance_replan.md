---
created_at: 2026-06-21T20:00:42.070394425Z
updated_at: 2026-06-21T20:53:21.450698958Z
tags:
    - plan
    - mesnada
    - delegation
    - phase7
    - projects
    - acp
    - concurrency
    - ipc
    - architecture
---
# Plan: Delegation Phase 7 (RE-PLAN) — Warm per-project instance reuse + parallel agent loops

Created 2026-06-21. Status: PLAN (not implemented; decisions RESOLVED 2026-06-21,
to be started in a clean session). Re-plans the single optional Phase 7 of
`pando/plans/delegated_conclusion_resurrection_plan.md` into sub-phases after a
codebase review driven by the user's two requirements:
1. Show how the WebUI/TUI Projects panel (start/stop a per-project instance) fits
   the "reuse a warm ACP instance" delegation optimization.
2. A warm instance already running an agent loop must run SEVERAL agent loops in
   parallel (each in its own goroutine) so none blocks the others.

## RESOLVED DECISIONS (user, 2026-06-21) — drive the phases below
1. **Reuse-then-autostart**: prefer reusing an ALREADY-RUNNING manager-owned
   instance for the project; if none is running, AUTO-START one. (Not reuse-only.)
   → Implies a new Manager path that starts/reuses a child WITHOUT changing the
   active project (see 7.3). `AutoStartWarmInstance` defaults TRUE under the master
   flag; can be set false for reuse-only.
2. **External instances → always cold path (for now)**: editor-launched/external
   instances (`Runtime` external=true) are NOT used as warm targets; such spawns
   take the cold subprocess path.
3. **Stop with in-flight delegations → cancel + fallback**: stopping a project
   instance (panel or shutdown) CANCELS its in-flight delegated sessions and the
   orchestrator falls back to the cold path for them (with a UI warning); never a
   silent mid-run kill that loses the conclusion.
4. **Bookkeeping → most-robust option (decided): single-owner parent FileStore +
   always-terminal conclusion** (full spec in "Bookkeeping" section below).
5. **Sequencing**: ship 7.1 (concurrency hardening) first as it is broadly useful;
   then 7.2→7.5. Implementation will begin in a fresh session.

## Codebase findings (verified 2026-06-21)

### Concurrency is ALMOST ready — one real blocker
- ACP SDK (madeindigio/acp-go-sdk@v0.15.0 connection.go:412) dispatches every
  inbound request (e.g. session/prompt) in its OWN goroutine — a long Prompt does
  not block other requests (notifications are sequential; requests concurrent).
- The agent already runs each session loop in its own goroutine keyed by sessionID
  in activeRequests sync.Map (agent.go runInternal go func ~815); IsSessionBusy is
  per-session. Distinct sessions already run in parallel within one process.
- BLOCKER: the ACP Prompt handler (internal/mesnada/acp/agent.go:279) applies
  per-session model/persona by mutating GLOBAL shared agentService state per prompt:
  SetModelOverride (~324) and SetActivePersona (~335). Two concurrent sessions with
  different model/persona clobber each other. Thinking/effort are already per-session
  via SetSessionLLMOverrides(sessionID,…) — the correct pattern. Until fixed,
  parallel loops in one instance are unsafe.

### Projects manager already supports multiple live instances
- internal/project/manager.go: instances map[projectID]*Instance holds many running
  child `pando acp` processes at once. Activate spawns (if needed) + sets activeID; it
  does NOT stop others. activeID is only the UI focus — delegation can reuse any
  running instance without changing the active project. (But Activate ALSO sets
  activeID, so 7.3 needs a no-activeID variant — see below.)
- Runtime(projectID,path)->(running,external,pid) classifies ours vs external
  (editor ACP). Stop refuses external (ErrExternalInstance). ListSessions enumerates
  a child's sessions. spawnChild only starts when project has a config file.

### The Manager's ACP client is a no-op
- internal/project/acp_client.go projectACPClient implements acpsdk.Client with
  no-ops: SessionUpdate DISCARDS notifications. Manager uses the conn only for
  lifecycle + session/list. Capturing a delegated conclusion over the wire needs a
  capturing client.

### Orchestrator decoupled from projects
- internal/mesnada/orchestrator does not import internal/project. Spawn takes a
  free-form work_dir; engine=pando cold-spawns `pando -p`, engine=acp-* spawns fresh
  ACP agents. Only implicit coupling is IPC (same canonical path → secondary,
  dbproxy single-writer SQLite). Mesnada tasks live in a JSON FileStore (NOT project
  SQLite).

## Re-plan: split optional Phase 7 into 7.1 – 7.5

### Phase 7.1 — Concurrency hardening: per-session model/persona (PREREQUISITE, ship first)
Make a single instance safe to run N parallel agent loops (foundation for the user's
requirement and warm reuse).
- Replace global SetModelOverride/SetActivePersona in the ACP Prompt path with
  per-session scoping threaded through agent.Run (mirror SetSessionLLMOverrides, or
  pass via genCtx into processGeneration).
- Audit every shared-agentService mutation reachable from Prompt (persona
  auto-select, thinking reconcile, permission mode) for per-session safety;
  configurePermissionMode(sessionID,…) is already keyed — verify.
- Tests: two sessions with different model/persona run concurrently and neither
  clobbers the other (race detector).
- Independently valuable (benefits ACP server + multi-session WebUI), ships even if
  7.2-7.5 are deferred.

### Phase 7.2 — Delegating ACP client (capture conclusions over the wire)
Turn the no-op client into a capturing client used only for delegation.
- Open a NEW session in the child (conn.NewSession, cwd=project path), send prompt +
  conclusion brief (reuse conclusion.BriefInstruction), stream via conn.Prompt
  (synchronous → per-task goroutine).
- Capture the agent message stream in SessionUpdate, accumulate OutputTail, extract
  the <pando:conclusion> block. Produce a synthetic terminal models.Task (engine=
  warm-acp, model, project, parent session, correlation, depth, output) so the
  EXISTING Phases 1-6 pipeline (conclusion.Enrich → supervisor Case A/B / await)
  consumes it unchanged.
- Map ACP cancel ↔ orchestrator cancel; close the delegated session on completion.
- Per decision 3: a cancel (instance stopping) terminates the child session and the
  task takes the cold-path fallback.

### Phase 7.3 — Warm-target routing in the spawn path (reuse-then-autostart)
- New narrow interface (WarmTargetResolver) injected into the orchestrator (avoid an
  internal/project import cycle; same pattern as ProjectResolver/ModelResolver).
  Canonicalize work_dir → registry lookup. Routing per decision 1:
  1) if a manager-owned instance is running for the project (Runtime running &&
     !external) → reuse it (7.2);
  2) else if AutoStartWarmInstance → auto-start a child for the project and reuse it;
  3) else (autostart disabled, or project unregistered/no config, or external) →
     cold-spawn (today's behavior).
- DECISION 2: external instances are never warm targets → always cold path.
- REQUIRED Manager change: a start-or-reuse path that does NOT mutate activeID (the
  delegation must not switch the user's focused project). Add e.g.
  `EnsureInstance(ctx, projectID) (*Instance, error)` that reuses
  `instances[projectID]` or calls `spawnChild` WITHOUT setting `m.activeID`. Mark
  such instances as delegation-spawned (lifecycle below).
- New config flags under Delegation: `ReuseWarmInstances` (master, default OFF),
  `AutoStartWarmInstance` (default TRUE; false = reuse-only).
- `MaxConcurrent` also bounds concurrent warm sessions per instance; over the cap →
  cold-spawn (or queue) rather than overloading one instance.
- Lifecycle of auto-started instances: they PERSIST and appear in the Projects panel
  so the user can stop them; tagged delegation-spawned. (Optional later: idle
  auto-GC when they have no delegated sessions and were not user-activated.)

### Phase 7.4 — Projects panel integration (WebUI + TUI) — the user's question
- Start a project instance from the panel = pre-warm a delegation target; Stop =
  remove it. Per decision 3, Stop with in-flight delegated sessions → cancel them +
  cold-path fallback, surfaced with a UI warning (count of cancelled sessions).
- Per project row, surface the delegated sessions running inside the instance (reuse
  Manager.ListSessions + a live count): "project X running N delegated loops". Show
  whether the instance is user-activated vs delegation-spawned.
- Optionally allow a delegated spawn to TARGET a registered project by id (not just a
  raw path) from the UI/tool.
- Live updates: publish a ManagerEvent on delegated session start/end → SSE (WebUI) /
  IPC (TUI) so counts refresh live.
- External instances (ErrExternalInstance): excluded from warm reuse (decision 2);
  the panel still shows them as running/external (current behavior unchanged).

### Phase 7.5 — Tests, docs, config UI, e2e
- Concurrency e2e: a warm instance serves 2+ parallel delegated sessions with
  different models; both yield conclusions; neither blocks (race detector).
- Routing tests: running→reuse, not-running+autostart→start+reuse,
  autostart-off/external/unregistered→cold; cap enforcement; activeID NOT changed by
  delegation.
- Stop-with-inflight test: cancel + cold fallback, no lost conclusion.
- Panel integration + i18n; expose ReuseWarmInstances + AutoStartWarmInstance in
  TUI/WebUI/API per the Phase 5 pattern.
- Update feature doc + README; KB change docs per sub-phase.

## Bookkeeping (decision 4 — most-robust option, chosen)
Goal: never lose a delegated result, never double-track, survive child death /
disconnect / parent restart.
- **Single source of truth = the parent orchestrator's JSON FileStore.** The
  orchestrator creates the delegated Task (status=running) and stores the child's
  ACP session id (in ACPSessionID) for correlation/recovery BEFORE dispatching the
  prompt to the warm instance.
- **The child session is ephemeral from the orchestrator's view.** The child keeps
  its OWN normal session history in its project SQLite (unchanged), but the
  orchestrator NEVER reads/writes the child's SQLite — no cross-store coupling, no
  double bookkeeping.
- **Always reach a terminal conclusion.** On ACP run completion → mark Task
  completed + run the existing captureConclusion/broadcast. On ANY failure path
  (child death, ACP disconnect, cancel from a panel Stop, timeout) → mark Task
  failed with a clear error and let the synthesize fallback produce a
  failed/blocked conclusion, so the supervisor STILL re-enters the parent loop
  (Case A/B) — a delegated run can never leave the parent hanging.
- **Idempotency** via CorrelationID (already enforced by the supervisor) guards
  against a result delivered twice (e.g. wire + fallback racing).
- **Parent restart**: warm delegated sessions are NOT re-attached in MVP — the
  orchestrator's stale-task recovery marks the running warm Task failed and
  synthesizes a conclusion (the parent resurrects with a blocked/failed result).
  Re-attach-on-reconnect (LoadSession by stored ACP session id) is a future
  enhancement, not MVP.

## Why a re-plan, not the old single phase
The original Phase 7 bundled "reuse a warm instance" as one opt-in item. The review
shows it requires (a) a concurrency-safety prerequisite valuable on its own, (b) a
new capturing ACP client, (c) orchestrator↔projects routing without an import cycle
plus a no-activeID start-or-reuse Manager path, and (d) real Projects-panel
UX/lifecycle work — separable, independently testable deliverables. Splitting
de-risks the work and lets 7.1 land independently.
