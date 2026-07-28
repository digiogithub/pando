---
created_at: 2026-07-29T09:28:34.613171713Z
updated_at: 2026-07-29T09:28:34.613171713Z
tags:
    - fix
    - agui
    - copilotkit
    - state
    - mesnada
---
# AG-UI: per-thread state document + non-JSON tool results (2026-07-29)

Second full browser pass over `examples/copilotkit` (Next.js 15.5 + CopilotKit
1.64.1 against `pando agui-serve`). Two real defects surfaced that no unit test
could see, both fixed adapter-side. See [[analysis_copilotkit_agui_integration]]
and [[agui_tool_calls_missing_from_event_stream]].

## Bug 1 — the shared-state document was per run, not per thread

`server.go` called `newStateTracker(...)` on every request, so `STATE_SNAPSHOT`
published an empty document at the start of each turn: the page's todo list,
touched files and sub-agents blinked out on every new message. The type's own
comment already said "one thread's state document" — the lifecycle disagreed.

Fix: `stateStore` in `state.go`, held by `Runtime.states`.

- `get(threadID, sessionID, agent, model, clientState)` returns the thread's
  tracker, creating it on first contact.
- A tracker built for a different session is discarded (the thread was rebound;
  its files belong to a conversation that no longer exists).
- `rebind` refreshes the model and the client-owned state blob between turns.
- Memory only (the durable half is the `agui_threads` binding), bounded by
  `maxStateThreads = 256` with LRU eviction.

## Bug 2 — mesnada tool results are TOON/TOML, never JSON

`subagents.go` decoded mesnada results with `json.Unmarshal`. Pando's tools
answer through `tools.NewStructuredResponse`, which renders **TOON** when it
can, TOML next, and indented JSON only as a last resort — chosen per value. The
decode therefore always failed, the branch returned `handled=true` with no
events, and the "Sub-agents" panel stayed empty in every real run. P7's unit
tests passed because their fixtures were hand-written JSON.

Fix: `decodeToolResult(content, v)` tries JSON, then `toon.DecodeString`
(re-encoded through JSON so the struct tags remain the single description of the
shape), then TOML.

## Files touched

- `internal/agui/state.go` — `stateStore`, `stateTracker.rebind`, `.session()`
- `internal/agui/runtime.go` — `Runtime.states`
- `internal/agui/server.go` — `r.states.get(...)` instead of `newStateTracker`
- `internal/agui/subagents.go` — `decodeToolResult`, used by `observeMesnadaLocked`
- `internal/agui/doc.go` — two new sections recording both caveats
- Tests: `state_test.go` (`TestStateStoreKeepsDocumentAcrossRuns`,
  `TestStateStoreEvictsLeastRecentlyUsed`), `subagents_test.go`
  (`TestSubAgentSpawnInToolOutputFormat`,
  `TestSubAgentTaskLookupInToolOutputFormat`, both driving the real
  `tools.FormatStructuredData` and asserting it is not JSON)

## Verification

`gofmt` clean, `go vet` clean, `go test -race ./internal/agui ./internal/api`
green. Live browser pass on one thread, everything accumulating in the dashboard:

1. chat — "PONG" streamed; model card `Copilot: GPT-5 mini — 19.5k / 264k`
2. todos — 3 items, one `in_progress`, still present in every later turn
3. file — `write` raised `pando_permission_request`, approved in the page,
   `report.txt` written in the project, `Files touched: write: report.txt`
4. sub-agent — `mesnada_spawn_agent` produced `task-4794c1e7 — pending`, and a
   later `mesnada_get_task` advanced the same card in place to `completed`
5. frontend tool — `highlight_in_page` suspended the run, the page outlined the
   `Sub-agents` heading (`orange solid 2px`), the run resumed and answered DONE

## Not bugs, worth knowing

- The model once wrote to `/tmp/.../-www-MCP-Pando-pando-<session>/...` — a
  hallucinated path where a `/` became `-`. Pando wrote exactly where it was
  told; the permission card showed the wrong path before approval. Asking for a
  relative `file_path` avoided it.
- The permission card's `path` field is the session working directory, not the
  target file (the file appears in the description line below it). Cosmetic.
- Invariants I1-I7 hold: no agent signature changed, no new `agent.AgentEvent`
  type, everything stayed inside `internal/agui`.
