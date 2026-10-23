---
created_at: 2026-10-23T13:36:12.226312345Z
updated_at: 2026-10-23T13:36:12.226312345Z
tags:
    - fix
    - mesnada
    - delegation
    - copilot
    - provider
    - ipc
    - webui
    - macos
---
# Fix: macOS "subagents stuck" report — poisoned Copilot history, sibling tasks marked failed, frozen subagent sessions (2026-10-23)

Reported by a macOS user (debug_id 5197-1123-6710-9997, v1.4.3 desktop). Related: [[follower_session_sync_busy_elsewhere]], [[fix_copilot_poisoned_tool_history]], [[warm_acp_pending_and_self_delegation]], [[feature_sqlite_multiwriter_engine_v130]].

## Telemetry findings (Better Stack source 2751484)
- Desktop session c26529d1 spawned 4 `pando-cli` subagents (copilot.gpt-6-luna); each spawned more (10 grandchildren in ~/GIT/app_ios_ivooxnew). All 14 tasks finished `completed`.
- Every child `pando -p` ran `recoverStaleTasks` on the shared `./.pando/mesnada/tasks.json` and marked the still-running sibling tasks failed (25 `recovering_stale` events). `FileStore.save` rewrote the whole file every 5s => last writer wins across processes.
- Subagent sessions are written to pando.db by IPC followers, which never broadcast session/message events: desktop showed them frozen with input enabled.
- 12:31: Claude Opus via Copilot hit max_tokens -> `Dropping tool call truncated by max_tokens`, then 400 `tool call must have a tool call ID and function name` (`invalid_tool_call_format`). A call without ID/name was kept, executed and persisted; every later request in the session 400s.

## Changes
1. **Provider (Copilot)** — `internal/llm/provider/tool_args.go`: `isReplayableToolCall`, `dropIncompleteToolCalls`, `replayableToolCallIDs`. `copilot.go`: drop incomplete calls on stream + non-stream responses; `convertMessages` and `convertMessagesToResponsesInput` skip calls without ID/name, their orphan tool results, and assistant messages left empty (no more random call_ ids in Responses path). Heals already-poisoned sessions on replay. openai.go intentionally untouched (OpenAI-compatible local models may emit empty ids).
2. **Mesnada stale recovery** — `orchestrator.Config.SkipStaleRecovery` (set from `AppOptions.SkipStartupIndexing` in `internal/app/app.go`, i.e. one-shot runs); `recoverStaleTasks` leaves tasks whose `PID` is alive (`processAlive`, alive_unix.go/alive_other.go). Risk: PID reuse after a crash can keep a stale task `running`.
3. **Shared task store** — `internal/mesnada/store/store.go`: FileStore tracks `owned`/`deleted` ids; `save()` takes an inter-process lock (`tasks.json.lock`, lock_unix.go flock / lock_windows.go LockFileEx), re-reads the file and only overlays the tasks this process wrote.
4. **Follower -> leader sync + busy-elsewhere sessions** — implemented by a subagent, see [[follower_session_sync_busy_elsewhere]] (`follower.sync` RPC, `/api/v1/sessions/{id}/watch` SSE, 409 `busy_elsewhere`, WebUI input disabled with notice, i18n x7).

## Verification
- New tests: `TestDropIncompleteToolCalls`, `TestCopilotConvertMessagesSkipsIncompleteToolCalls`, `TestCopilotResponsesInputSkipsIncompleteToolCalls`, `TestRecoverStaleTasksSkipsLiveProcesses`, `TestFileStoreSharedFileKeepsOtherProcessesTasks`, `TestFileStoreDeleteSurvivesMerge` (+ subagent's follower_sync/session_watch/busy_elsewhere tests).
- `go build ./...` OK; `go test ./internal/llm/... ./internal/api/... ./internal/app/... ./internal/mesnada/... ./internal/ipc/... ./internal/session/... ./internal/message/... ./cmd/...` all ok; store/orchestrator with `-race` ok. WebUI typecheck/lint/vitest/build:embedded ok.
- Not tested by hand on macOS desktop. Uncommitted (jj working copy).
