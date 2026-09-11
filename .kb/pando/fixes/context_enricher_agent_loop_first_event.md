---
created_at: 2026-09-11T12:44:21.36539098Z
updated_at: 2026-09-11T13:19:27.801072467Z
---
# Fix design: context-enrichment agent loop aborts on its first run event (2026-09-11)

Status: **IMPLEMENTED 2026-09-11**. Point 2 of 3.
Builds on: [[pando/analysis/sqlite_locked_interrupted_errors.md]] (Cause 1). Interplay: [[pando/plans/mcp_server_ipc_bootstrap.md]].

Telemetry (`pando acp`, Zed stdio, 2026-09-11 11:16:19 UTC):
- warn `context enrichment agent loop failed {error: "enrichment loop produced no assistant message"}`
- then error `failed to process events: failed to create assistant message: sqlite3: interrupted`

## 1. Verified root cause (two defects that compound)

### Defect A: `runLoop` reads exactly one event (primary bug)
`internal/app/context_enricher_agent.go:180-192`:
```go
select { case result = <-done: ... case <-runCtx.Done(): ... }   // ONE receive
if result.Error != nil { ... }
if result.Message.Role != message.Assistant { return "", fmt.Errorf("enrichment loop produced no assistant message") }
```
It then returns, and `defer cancel()` (line 167) cancels `runCtx`. The enrichment agent's `genCtx` derives from it (`agent.go:929`). Every agent run streams many events on the same channel before the terminal one, so the first receive is essentially never the final response.

Events on the `Run` channel (`events`, buffered 512, `agent.go:922`), all non-terminal unless noted:
- `SystemMessage`:
  - `emitStatus` (`agent.go:865-874`): history trim at `1120`, enrichment notices at `1174/1184` (only for non-enricher agents)
  - auto-compaction before send (`1345/1354/1373`) and mid-loop (`1296/1306`)
  - model-switch messages (`model_switch.go:231-241`)
- `Error` (non-terminal): `emitCompactionError` (`agent.go:894-904`)
- `SteeringInjected` / `ConclusionInjected`: `729/741`
- `ThinkingDelta` / `ContentDelta` / `ToolCall`: `1859/1868/1877/1901/1919/1965`
- `ToolResult`: `1675/1705/1718/1734/1762`
- `TodosUpdated`: `1787`
- `TokenUsage` (estimated at `1770`, confirmed at `1985`, via `publishTokenUsage` `2038-2047`)
- Terminal: exactly ONE event, the value returned by `processGeneration`, sent at `agent.go:978`, followed by `close(events)` at `979`.
  - On success it is `Response` with `Done:true` (`agent.go:1326-1331`).
  - On failure it is `a.err(...)` = `Error` with `Done:false` (`agent.go:822-827`).
  - On panic, `a.err` is sent from the recover handler (`942`).

Other consumers drain correctly until close:
- non-interactive `-p`: `for event := range done { result = event }` (`internal/app/app.go:1783-1785`)
- ACP: `forwardEvents` ranges `realCh` (`cmd/root.go:758-796`); `mesnada/acp/prompt_handler.go:420`
- REST: `internal/api/handlers_chat.go:89`, `background_runner.go:111`
- AG-UI: loops until `Response`/`Error`/closed (`internal/agui/server.go:430-450`)
- wrappers: `learning_session.go:68`, `superpowers_session.go:68`, `goal_runner.go:192`

The enricher is the only consumer that reads a single event.

### Defect B: model-switch check is not agent-aware, which makes the first event arrive BEFORE the first DB write
- `applyPendingModelSwitch` runs at the top of EVERY loop iteration, before `streamAndHandleEvents` (`agent.go:1246`).
- `desired := effectiveSessionModel(sessionID)` (`model_switch.go:173`). With no override it falls back to `configuredAgentModel()`, which returns the **coder** agent's model (`setup_bridge_model.go:172-178, 200-210`) whatever `a.agentName` is.
- This project configures coder = `openrouter.qwen/qwen3.8-2.4t-a95b` and context-enricher = `ollama.nova4b:latest` (`.pando.toml:49-66`).
- For the enricher, `desired != current`. `prepareProvider` then rebuilds the enricher's own model (no override), and the defensive check fails with "the rebuilt provider still runs ollama.nova4b".
- `emitModelSwitchMessage` then sends a `SystemMessage` ("⚠ Could not switch to openrouter… Continuing with ollama.nova4b") to the run channel (`model_switch.go:196-199, 231-241`).
- This is the first event. The receive at line 182 gets it (Role empty), runLoop returns and `cancel()` fires.
- In parallel, the agent goroutine proceeds to `streamAndHandleEvents`:
  1. `StreamResponse` starts the LLM HTTP request (`agent.go:1592`).
  2. `messages.Create(ctx, …)` (`1594`) runs with a cancelled ctx. ncruces `SetInterrupt` turns that into `sqlite3: interrupted`.
  3. It is wrapped at `1598` ("failed to create assistant message") and `1257` ("failed to process events").
  4. Because it is not `context.Canceled`, it is logged with `logging.ErrorPersist` at `agent.go:961`.
- DB evidence confirms the Create is what gets interrupted: 28 `ctxenrich-*` sessions carry 28 user messages but only 1 assistant message (with `parts=[]`), and every session has `prompt/completion tokens = 0`, `cost = 0`.
- Defect B also affects every other non-coder agent (task/sub-agents, persona-selector if run as an agent, etc.). Whenever their model differs from the coder's they emit a spurious "Could not switch" message on each loop iteration and rebuild a provider each time.

Note on the prior analysis: it attributed the first event to a streaming delta. With matching models that would be true, and the failure would then hit a later write. With differing models (this setup) the first event is the model-switch SystemMessage, which is why the specific error is the assistant-message INSERT.

### Origin / regression?
- Not a regression. `git log -S "enrichment loop produced no assistant message"` shows a single commit: `c0251b29` (2026-08-08, "feat: improved agent for context enrichment and config"). It first shipped in tag **v0.646.4**, and the file has had no further commits.
- Streaming deltas on the run channel predate it: ContentDelta/ToolCall since `d39d96fb`/`e86bac3c` (2026-03), TokenUsage since `39f99030` (2026-06-15). `git show c0251b29:internal/llm/agent/agent.go` already contains them.
- `applyPendingModelSwitch` came in `afeb4999` (2026-07-30), also before.
- The agent-loop enricher has therefore **never produced a result** since it was introduced. Every run took the error path and then the `rag.ContextEnricher` search fallback. Its only unit test (`context_enricher_agent_test.go`) covers `normalizeEnrichedBlock`, never `runLoop`.

## 2. Side effects
- **Feature silently dead:** the user always gets the single-shot search fallback, never the agent-loop context, while the chat still shows "🧠 Context enrichment agent gathering…/✓ done" notices (`agent.go:1172-1185`).
- **LLM cost:**
  - Per session start, one enricher `StreamResponse` is started and then aborted by ctx cancel within milliseconds. Cost is at most a partial prompt charge; `TrackUsage` never runs, so session cost shows 0 and `chargeParent` is never reached.
  - The enricher is local ollama here, so the real waste is small. Plus one `prepareProvider` rebuild per run (Defect B).
  - The fallback search still runs its embedding calls.
  - No title LLM call: `ctxenrich` titles stay "Context enrichment", so the title path doesn't fire for this agent.
- **DB litter:**
  - With `hiddenInChat=false` (default) and a parent session, each run creates a child session `ctxenrich-<uuid>` via `CreateTaskSession` (`context_enricher_agent.go:204-209`) and never deletes it.
  - Read-only count today: **28 sessions** (2026-08-16 → 2026-09-11), 29 messages (28 user, 1 empty assistant), 0 cost.
  - The session indexer has no filter, so it also indexed them: **47 `events` rows** with `subject='session'` and `session_id LIKE 'ctxenrich-%'`. Those are extra `ReplaceSessionEvents` writes per run.
  - They also appear as child sessions in the UI.
- **Interrupted writes:** the interrupted statement is an autocommit INSERT. SQLite rolls back the statement's implicit transaction, and ncruces returns the conn to the pool with the interrupt cleared on the next `SetInterrupt`. It is not `driver.ErrBadConn`, so the pool is not poisoned and there is **no dangling transaction or connection**. The only data artefact is the occasional empty assistant row, when the Create wins the race and the cancelled path then skips the Update.
- **Hidden-session race:** with `hiddenInChat=true` (or no parent), `cleanup()` deletes the session (`context_enricher_agent.go:216-221`) while the agent goroutine may still be writing to it. That can produce FK errors (messages → sessions `ON DELETE CASCADE`) or rows inserted after the delete. The timeout branch (`184-185`) has the same race: it calls `Cancel` and returns without waiting.
- **Prompt latency / blocking:**
  - Enrichment is **synchronous** on the main prompt path. `processGeneration` calls `EnrichContextForSession(ctx, …)` before creating the user message (`agent.go:1165-1194`). It runs once per session (first message) unless `ContextEnrichmentAgentLoopEveryMessage`. It is bounded by `ContextEnrichmentAgentLoopTimeoutSeconds`, default **60 s** (`context_enricher_agent.go:22,68-71`), and by the prompt ctx.
  - Today the bug makes it return fast: here ~3 s from session creation (11:16:16) to failure (11:16:19), spent building the provider and running `messages.Create`/List, followed by the synchronous search fallback.
  - After the fix, the first prompt of each session will genuinely wait for the loop. On Zed/ACP that means up to 60 s of silence apart from the status notice. The timeout default should be revisited (see below).
- **"database is locked":** only a marginal contributor. Each enrichment adds about 3 small writes (session create, user message, assistant message) plus the indexer's `ReplaceSessionEvents` at session start. It holds no long transaction. The interrupt releases the write lock immediately and does not block other writers.

## 3. Fix design

### 3.1 Drain the channel in `runLoop` (Defect A)
```go
func (e *agentLoopEnricher) runLoop(ctx context.Context, sessionID, query string) (string, error) {
    enrichAgent, err := e.ensureAgent()
    if err != nil { return "", fmt.Errorf("enrichment agent unavailable: %w", err) }

    runCtx, cancel := context.WithTimeout(ctx, e.timeout)
    defer cancel()                                   // runs only after drain returned (agent finished)

    loopSession, cleanup, err := e.createSession(runCtx, sessionID)
    if err != nil { return "", err }
    defer cleanup()                                  // LIFO: cleanup runs after the drain as well

    done, err := enrichAgent.Run(runCtx, loopSession.ID, query)
    if err != nil { return "", fmt.Errorf("enrichment run failed: %w", err) }

    final, timedOut := drainRun(runCtx, done, func() { enrichAgent.Cancel(loopSession.ID) })
    if timedOut { return "", fmt.Errorf("enrichment loop timed out after %s", e.timeout) }
    if final.Error != nil {
        if errors.Is(final.Error, agent.ErrRequestCancelled) || errors.Is(final.Error, context.Canceled) {
            return "", fmt.Errorf("enrichment loop cancelled: %w", final.Error)
        }
        return "", fmt.Errorf("enrichment loop error: %w", final.Error)
    }
    if final.Type != agent.AgentEventTypeResponse || final.Message.Role != message.Assistant {
        return "", fmt.Errorf("enrichment loop ended without a response (last event %q)", final.Type)
    }
    e.chargeParent(ctx, sessionID, loopSession.ID)
    return normalizeEnrichedBlock(final.Message.Content().String(), e.maxChars), nil
}

// drainRun consumes a Service.Run channel until it closes and returns the terminal event
// (the last one sent). Intermediate event types — present or future — are ignored, so a
// new streaming event can never be mistaken for the result again. On ctx expiry it cancels
// the run and keeps draining (bounded grace) so the agent's own writes finish before the
// caller cancels contexts / deletes the session.
func drainRun(ctx context.Context, ch <-chan agent.AgentEvent, cancelRun func()) (agent.AgentEvent, bool) {
    var last agent.AgentEvent
    for {
        select {
        case ev, ok := <-ch:
            if !ok { return last, false }
            if isTerminal(ev) { last = ev }           // Response(Done) or Error; ignore deltas etc.
        case <-ctx.Done():
            cancelRun()
            grace := time.NewTimer(5 * time.Second); defer grace.Stop()
            for {
                select {
                case _, ok := <-ch: if !ok { return last, true }
                case <-grace.C:      return last, true   // agent stuck: give up, don't leak the caller
                }
            }
        }
    }
}
```
- Treat "last event before close" as authoritative. The agent guarantees the terminal event is the final send before `close` (`agent.go:978-979`).
- `isTerminal` must not trust a mid-run `Error` from `emitCompactionError` (non-terminal). Keep it simple: the event received immediately before close is the terminal one, i.e. `last = ev` for every event and return `last` on close. Optionally also remember the last `Response` seen.
- Guard against a future change to the channel contract: a test (below) asserts that intermediate `Error`/`SystemMessage`/`TokenUsage` events before the final response do not affect the result.
- Preferably put `drainRun` (a `CollectRunResult(ctx, ch, cancel)`) in `internal/llm/agent` so every "fire and wait for the result" caller shares it: enricher, app.go non-interactive, goal runner, background_runner. That removes the need for each consumer to know the channel shape.
- Ordering guarantee: `cancel()` and `cleanup()` run only after `drainRun` returned. Channel close happens after the agent's final DB write (`processGeneration` returned, `agent.go:960-979`), so no write is interrupted on the success path. On timeout, `Cancel` makes the agent stop through its own cancelled path (`context.Canceled` → `Update(context.Background())`, `agent.go:1252-1255`) before we delete anything.

### 3.2 Make the model-switch check agent-aware (Defect B)
Only a per-session **override** should trigger a mid-run switch, and the coder's configured model must not be applied to other agents:
```go
// model_switch.go, applyPendingModelSwitch
desired, isOverride := effectiveSessionModel(sessionID)
if !isOverride && a.agentName != config.AgentCoder { return current, msgHistory }
if desired == "" || desired == current.Model().ID { return current, msgHistory }
```
Alternatively, give `effectiveSessionModel` an agent parameter and compare with `cfg.Agents[a.agentName].Model`. Either way this removes the spurious "Could not switch" SystemMessage and the per-iteration provider rebuild for every non-coder agent (enricher, task agents).

### 3.3 Session persistence policy
- Keep the visible child session (it is the documented "inspectable retrieval trace"), but:
  - (a) delete it when the run produced no usable context or failed, unless `cfg.Debug`; or
  - (b) add config `ContextEnrichmentAgentLoopKeepSessions` (default false) and otherwise delete it after the run. Delete only after `drainRun` returns (already guaranteed by the defer order).
- Exclude enrichment sessions from the session indexer. They duplicate KB/code content back into `events` and add writes. Filter by the `ctxenrich-` id prefix, or better by a session flag/title constant exported from `internal/app`, in `remembrances_indexer.go` (the payload handler around line 55 and `indexSessionConversation`). Title sessions (`title-*`) could use the same filter.
- Longer term (optional): an ephemeral in-memory `message.Service`/`session.Service` for the enricher so it never touches SQLite. That is not needed for correctness.

### 3.4 Latency
- Once the loop really runs, lower the default `defaultEnrichmentLoopTimeout` (60 s → 20-30 s), or make it proportional.
- Consider running enrichment concurrently with provider preparation, or streaming a heartbeat SystemMessage.
- Document that the first prompt of each session blocks on enrichment. This is relevant to the ACP/iOS-hang report: a slow local enricher model stalls the editor for up to the timeout.

### 3.5 One-off cleanup of the 28 existing `ctxenrich-*` sessions
Preferred: a small `pando db` subcommand or startup migration that uses `session.Service.Delete`. That publishes deleted events and IPC. Messages cascade via FK (`messages.session_id … ON DELETE CASCADE`).

SQL equivalent (run with pando stopped, or through the primary):
```sql
DELETE FROM sessions WHERE id LIKE 'ctxenrich-%';            -- messages cascade (needs PRAGMA foreign_keys=ON on that conn)
DELETE FROM messages WHERE session_id LIKE 'ctxenrich-%';    -- belt and braces if FKs are off
-- events rows (47): delete via rag EventStore (keeps events_fts in sync), not raw SQL,
-- e.g. EventStore.ReplaceSessionEvents(id, nil) / deleteSessionEventsTx per session id.
```
Parents' `cost` is unaffected: `chargeParent` never ran.

## 4. Tests to add
Existing test: only `internal/app/context_enricher_agent_test.go` (`TestNormalizeEnrichedBlock`). The `agentLoopEnricher.newAgent` func field is a ready seam for injecting a fake `agent.Service`. Pattern: `steerMockAgent` in `internal/api/handlers_steer_test.go:22-52`.
1. `TestRunLoopDrainsUntilFinalResponse`: the fake emits `SystemMessage`, `ThinkingDelta`, `ContentDelta`, `ToolCall`, `ToolResult`, `TokenUsage`, then `Response{Done:true, Message: assistant "<enriched_context>x</enriched_context>"}`, then closes. Expect the block `x` and no error.
2. `TestRunLoopIgnoresNonTerminalError`: an intermediate `Error` (compaction-error style) followed by a final `Response`. Expect success.
3. `TestRunLoopFinalError`: the terminal `Error` event, then close. Expect an error and the fallback to be used.
4. `TestRunLoopTimeoutCancelsAndWaits`: the fake blocks until `Cancel(sessionID)` is called, then emits `Error(ErrRequestCancelled)` and closes. Assert that `Cancel` was called, that `runLoop` returns only after close, and that `cleanup` (fake session service Delete) ran after close.
5. `TestRunLoopClosedWithoutEvents`: close with no events. Expect a clean error, no panic.
6. `internal/llm/agent`: `TestApplyPendingModelSwitchIgnoresConfiguredCoderModelForOtherAgents`. With an enricher agent on model X and coder configured on Y, it emits no event and does not switch. With an explicit override it still switches.
7. Session indexer: `ctxenrich-` sessions are skipped.

Command: `go test ./internal/app ./internal/llm/agent`.

## 5. Interplay with point 1 ([[pando/plans/mcp_server_ipc_bootstrap.md]])
- The enricher is wired in `app.New` for every process where remembrances + `ContextEnrichmentEnabled` + `ContextEnrichmentAgentLoopEnabled` are on (`internal/app/app.go:411-431`), regardless of IPC role. `agent.SetContextEnricher` is a process global.
- It runs in whichever process hosts the prompting agent: the primary ACP here, but also secondaries (TUI, `-p` subagents, desktop, serve).
- It is a **prompt-path** component, not a background service. §5.4 of the plan (`startPrimaryServices`) must NOT move it to primary-only, or secondaries would lose enrichment. `Warmup` per process is fine.
- Writes:
  - The enricher uses `app.Sessions`/`app.Messages`, which are built on `q` (`app.go:238-246`). On a secondary `q` is the `DBProxy`, so writes are **direct-first** on the 1-conn/200 ms secondary connection, and proxied via IPC `db.write` only on BUSY/LOCKED (`proxy.go:233-277`).
  - `sqlite3: interrupted` is not a lock error (`isLockError`, `proxy.go:130-142`), so an interrupted write on a secondary is returned as-is and never proxied. There is no risk of a cancelled write being replayed on the primary.
  - The enricher's tools (`ContextEnricherAgentTools`, `internal/llm/agent/tools.go:392-421`) are read-only (glob/grep/ls/view, KB/events/code search, recall). Its only remembrances writes come indirectly, from the session indexer re-indexing `ctxenrich` sessions. On secondaries those go through the remembrances proxy to the primary's writecoordinator.
- With §3.3 (skip indexing, delete sessions) the enricher adds no coordinator load. After the plan's in-place promotion (§5.5 G1 fix), the enricher keeps working because it holds the same session/message services.

## 6. Implementation (2026-09-11)

All of §3 implemented as designed, with a few deliberate deviations noted below. Verified with `go build ./...`, `go vet ./...`, `gofmt -l` (clean) and the test command from §4 (extended).

### 6.1 Drain fix (§3.1) — `internal/llm/agent/run_result.go` (new)
- Added exported `agent.CollectRunResult(ctx context.Context, ch <-chan AgentEvent, cancelRun func()) (last AgentEvent, timedOut bool)`, placed in `internal/llm/agent` as suggested so other "fire and wait" consumers can adopt it later (none were refactored now — app.go `-p`, `cmd/root.go` forwardEvents, AG-UI and the wrappers already drain correctly until close, so touching them was out of scope/unnecessary risk).
- Implementation is the "keep it simple" variant from §3.1: every event overwrites `last` unconditionally (no `isTerminal` type-switch needed) — the agent's channel contract guarantees only one event precedes `close`, so whatever survives until close *is* that event by construction. This also trivially satisfies "ignore intermediate Error events that are not terminal".
- On `ctx.Done()` it calls `cancelRun` (nil-safe) then drains for a bounded grace period (`runDrainGrace`, a `var` — not `const` — set to 5s so tests can shrink it) before giving up and reporting `timedOut=true` regardless of whether the channel closed within the grace window or the timer won.
- `internal/app/context_enricher_agent.go`'s `runLoop` now calls `agent.CollectRunResult(runCtx, done, func() { enrichAgent.Cancel(loopSession.ID) })` instead of the single-receive `select`. `defer cancel()` and `defer cleanup()` are unchanged (still after the call returns), so the ordering guarantee from the design holds unmodified.
- Tests: `internal/llm/agent/run_result_test.go` — last-event-before-close, non-terminal Error ignored, final Error returned, closed-with-no-events, timeout (Cancel called, waits for the simulated delayed close), and grace-timeout give-up (a permanently stuck channel).

### 6.2 Agent-aware model switch (§3.2) — `internal/llm/agent/model_switch.go`
- Implemented exactly as designed: `applyPendingModelSwitch` now reads `desired, isOverride := effectiveSessionModel(sessionID)` and returns `current, msgHistory` unchanged when `!isOverride && a.agentName != config.AgentCoder`, before ever calling `prepareProvider`. The coder's path (`isOverride` false, `agentName == AgentCoder`) is untouched — verified by the pre-existing `TestApplyPendingModelSwitchIsANoOpWithoutAnOverride`/`...KeepsRunAliveWhenTheProviderCannotBeBuilt` still passing unmodified.
- Tests added: `TestApplyPendingModelSwitchIgnoresConfiguredCoderModelForOtherAgents` (enricher agent, coder configured on a different model, no override → no-op, no event) and `TestApplyPendingModelSwitchOverrideStillAppliesForNonCoderAgent` (enricher agent, explicit override → the switch is still attempted, proven by the reported failure event, since `models.ProviderMock`'s client deliberately `panic("not implemented")` and cannot be used to build a real successful provider in a unit test — see `provider.go:274`).

### 6.3 Ephemeral sessions (§3.3) — chosen design: **delete, not "don't persist"**
Decision: kept creating the child session (still the "inspectable retrieval trace" when needed) but made deletion unconditional after every run, not just for the previously-already-deleted "hidden" standalone case. This was chosen over an in-memory/non-persisted session because it requires no new `session.Service`/`message.Service` implementation and preserves the existing debug story.
- `internal/app/context_enricher_agent.go`:
  - `createSession` now always returns a `deleteSessionCleanup(sessionID)` cleanup (both the child-session and standalone branches), instead of only the standalone one.
  - `deleteSessionCleanup` skips the delete when `config.Get().Debug` is true, so a developer can still open the run's session from the UI when actively debugging — the only place this diverges from "always delete"; justified because losing the retrieval trace entirely would regress the one diagnostic tool this feature has, and Debug is opt-in and rare.
  - Cleanup still runs from `runLoop`'s existing `defer cleanup()`, which (via `CollectRunResult`'s ordering guarantee, §6.1) only fires after the drain returns — no FK/write race reintroduced.
  - Added `agentLoopEnricher.fallback`'s type change (see §6.5) as a side effect of testability work, unrelated to persistence but touched in the same area.
- Indexer skip — `internal/app/remembrances_indexer.go`: added `isEphemeralIndexSession(id)` (checks `ctxenrich-` and `title-` prefixes via a shared `ephemeralIndexSessionPrefixes` var) and applied it both in the debounce-timer watcher loop (fast path, avoids scheduling work at all) and defensively at the top of `indexSessionConversation` (belt-and-braces for direct/future callers, e.g. tests or a manual re-index path). The `ctxenrich-` prefix constant (`ctxEnrichSessionIDPrefix`) now lives in `context_enricher_agent.go` and is reused here instead of being duplicated as a literal.
- Startup cleanup — new `internal/app/enrichment_cleanup.go`, `App.cleanupLeftoverEnrichmentSessions(ctx)`:
  - Lists sessions, deletes every `ctxenrich-*` one via `Sessions.Delete` (messages cascade via FK, as designed), and for each also calls `Remembrances.Events.ReplaceSessionEvents(ctx, id, "session", nil, nil, nil)` to remove any previously-indexed rows for that session (FTS stays consistent — verified: `ReplaceSessionEvents` with zero chunks is exactly `deleteSessionEventsTx` followed by an empty insert loop).
  - Startup call site in `app.New` (`internal/app/app.go`, right after `initRemembrancesSessionIndexing`): gated on `remembrancesProxy == nil` — i.e. this instance holds the DB connection directly (primary, or a single non-IPC instance) — rather than a `pando db` CLI subcommand. Chosen because: (a) the check was already computed right there for the remembrances-service constructor, so it is a genuinely free/available role signal, matching the task's "primary-only if a check is available" preference; (b) it needs no new CLI surface or user action — it just quietly fixes itself on the next start, which is what a maintenance cleanup for a bug that is being fixed in the same release should do; (c) it is idempotent and cheap (one `List` + a handful of deletes; a no-op once the backlog is gone), so redundant runs across restarts cost nothing. Runs in a background goroutine so it never delays startup.
  - Tests: `internal/app/enrichment_cleanup_test.go` — removes matching sessions and their indexed events (seeded via a real in-memory `events.EventStore`), leaves an unrelated session and its events untouched, and is idempotent on a second call; plus a nil-safety no-op test (no `Sessions`/`Remembrances` wired).

### 6.4 Timeout (§3.4)
- `defaultEnrichmentLoopTimeout`: 60s → **25s** in `internal/app/context_enricher_agent.go` (comment updated to explain why: the main prompt blocks on this, and even with the new heartbeat notices 25s is already a long wait for an ACP client).
- Template default in `internal/config/init.go` (`ContextEnrichmentAgentLoopTimeoutSeconds = 60` → `25`) and the doc comment in `internal/config/config.go`. WebUI default/placeholder in `web-ui/src/components/settings/RemembrancesSettings.tsx` (60 → 25).
- User overrides are unaffected: this project's own `.pando.toml` has an explicit `ContextEnrichmentAgentLoopTimeoutSeconds = 60`, which is a real override (not "unset"), so it keeps running at 60s — deliberately left untouched.

### 6.5 ACP progress notices (user request, not in the original §3 design)
Investigated how the existing "Context enrichment…" notices reach ACP: `agent.go`'s `emitStatus` (same helper compaction uses) publishes an `AgentEventTypeSystemMessage` on both the pubsub broker and the run's `eventCh`; `mesnada/acp/prompt_handler.go`'s event loop (`case AgentEventTypeSystemMessage`, line ~459) calls `normalizeSystemMessage`, whose `default:` branch forwards any text it doesn't specifically recognize as visible agent text (`updateAgentMessageTextWithID`). **The start/done notices already reached ACP before this change** — nothing needed to be added to the `acp` package itself. What was missing: a properly-informative done message (only "chars added" before) and a heartbeat for a slow run.
- `internal/llm/agent/agent.go`:
  - `SessionContextEnricher.EnrichContextForSession` now returns a new `agent.EnrichmentOutcome{Block, Source, TimedOut, Duration}` instead of a bare `string` (sole implementer: `agentLoopEnricher`; sole caller: `processGeneration` — verified via grep before changing, so this is a safe non-breaking signature change within the module).
  - New `agent.EnrichmentSource` consts: `EnrichmentSourceAgentLoop`, `EnrichmentSourceSearchFallback`.
  - New `runSessionEnrichment` helper wraps the `EnrichContextForSession` call with a background ticker (only when `announce` is true) that calls `emitStatus` every `enrichmentHeartbeatInterval` (7s, inside the requested 5-10s band) with "🔍 Still enriching context… Ns" — same mechanism as the start/done notices, so **no new ACP/TUI/WebUI wiring was needed**; it reuses the exact channel every other notice already uses.
  - New `describeEnrichmentOutcome` builds the done message covering all four cases the task asked for: nothing found, found via the agent loop, found via search fallback (with a "loop timed out" suffix when both happened), and a bare timeout with nothing found — each with the run's duration.
  - Start message changed from "🧠 Context enrichment agent gathering project context..." to "🔍 Enriching context…" per the task's suggested wording; done/heartbeat messages use the same 🔍 glyph family for visual consistency.
  - Added `logging.Info("context enrichment: run started"/"run finished", "session_id", sessionID, ...)` around the announced run (heartbeats log at Debug to avoid Info-level spam every 7s).
- `internal/app/context_enricher_agent.go`: `EnrichContextForSession` now tracks `time.Since(start)`, wraps the timeout into a sentinel `ErrEnrichmentTimeout` (`errors.Is`-checkable, replacing brittle string matching) and sets `Source` based on whether the block came from the loop or the fallback.
- TUI/WebUI: unchanged. They already render `AgentEventTypeSystemMessage` generically (confirmed no code anywhere string-matches the old "chars of context added"/"Context enrichment agent gathering" text), so the new/changed message text and the extra heartbeat events flow through with no duplication and no separate wiring.
- Tests: `internal/mesnada/acp/context_enrichment_notice_test.go` — `TestNormalizeSystemMessagePassesThroughContextEnrichmentNotices` asserts the start/heartbeat/two done variants/timeout messages all hit `normalizeSystemMessage`'s pass-through default (not suppressed, not otherwise intercepted), using a zero-value `&PandoACPAgent{}` since the default branch touches no other agent state. This is the closest equivalent to "acp package test with fake connection" available: no existing ACP test in this repo drives a full fake-connection prompt loop, so testing the actual forwarding decision directly was the pragmatic choice.

### 6.6 Testability refactor (incidental)
- `agentLoopEnricher.fallback` changed from the concrete `*rag.ContextEnricher` to a narrow local interface `searchFallbackEnricher { EnrichContext(ctx, query) string }`. The constructor (`newAgentLoopEnricher`) converts the incoming `*rag.ContextEnricher` explicitly (`var fb searchFallbackEnricher; if fallback != nil { fb = fallback }`) specifically to avoid the typed-nil-interface pitfall (a nil `*rag.ContextEnricher` boxed directly into an interface value is non-nil). Behavior for the real production path (`app.go`'s call site, where the argument is never nil) is unchanged; this only exists so unit tests can inject a `fakeSearchFallback` instead of constructing a real `rag.ContextEnricher`.

### 6.7 Test files (new/changed)
- `internal/llm/agent/run_result.go` + `run_result_test.go` (new).
- `internal/llm/agent/model_switch.go` (fix) + `model_switch_test.go` (+2 tests).
- `internal/llm/agent/agent.go` (`EnrichmentOutcome`/`EnrichmentSource`, `runSessionEnrichment`, `describeEnrichmentOutcome`, updated processGeneration block).
- `internal/app/context_enricher_agent.go` (drain fix, outcome reporting, always-delete cleanup, 25s default, `searchFallbackEnricher`) + `context_enricher_agent_test.go` (rewritten with fakes; 5 new scenario tests plus the original `TestNormalizeEnrichedBlock`).
- `internal/app/enrichment_cleanup.go` (new) + `enrichment_cleanup_test.go` (new).
- `internal/app/remembrances_indexer.go` (skip filter) + `remembrances_indexer_test.go` (+2 tests).
- `internal/app/app.go` (startup cleanup call site).
- `internal/config/config.go`, `internal/config/init.go`, `web-ui/src/components/settings/RemembrancesSettings.tsx` (25s default).
- `internal/mesnada/acp/context_enrichment_notice_test.go` (new).

### 6.8 Verification
- `gofmt -l` on every changed file: clean.
- `go build ./...`: clean.
- `go vet ./...`: clean.
- `go test ./internal/app ./internal/llm/agent ./internal/mesnada/acp ./internal/rag/... ./cmd`: all packages pass except the 4 **pre-existing, unrelated** failures in `internal/llm/agent` called out in the task (`TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`, `TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged` — all read the real project `.pando.toml`/global config state, unrelated to this change; confirmed unchanged before/after).
- `go test -race` on the new/changed test files (`internal/app` enrichment tests, `internal/llm/agent` run_result + model_switch tests): all pass, no data races (relevant given the heartbeat goroutine and `CollectRunResult`'s grace-drain goroutine interactions).

### 6.9 Left undone / risk notes
- Other "fire and wait for the result" consumers (app.go `-p`, `cmd/root.go` forwardEvents, AG-UI, the wrapper sessions) were **not** refactored onto `agent.CollectRunResult` — the task said to do so "only if trivial and safe", and each already drains correctly until close with its own established shape; changing them was judged out of scope/unnecessary risk for this fix.
- The one-off startup cleanup only runs on the instance with direct DB access (`remembrancesProxy == nil`). If a deployment only ever runs secondaries against a primary that never restarts, the 28 pre-existing leftover sessions on that primary's DB will not be cleaned until the primary itself restarts. This is an accepted tradeoff (documented in §3.5/§6.3) rather than adding a `pando db` CLI subcommand; a manual restart of the primary (or a future explicit CLI command) remains available if needed sooner.
- `enrichmentHeartbeatInterval` (7s) and the drain grace period (5s) are fixed, not configurable — consistent with how compaction's own notices have no configurable cadence either.