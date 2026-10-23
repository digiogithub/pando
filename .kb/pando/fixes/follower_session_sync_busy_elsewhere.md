---
created_at: 2026-10-23T13:35:17.869388462Z
updated_at: 2026-10-23T15:46:30.988162441Z
tags:
    - fix
    - ipc
    - mesnada
    - webui
    - desktop
    - subagent
    - sse
---
# Follower → leader session sync + "busy elsewhere" sessions (subagents)

## Problem
Mesnada `pando -p` subagents run as IPC followers and write their own session/messages to the shared pando.db, but only the IPC leader's in-process brokers feed the WebUI/desktop/TUI. The leader never learned about follower writes, so a subagent session looked frozen and its chat input stayed enabled. Follow-up (user feedback): no flicker, event-driven incremental updates, sidebar list updated by event.

## Backend design
- RPC `follower.sync` (`protocol.MethodFollowerSync`, `FollowerSyncParams{InstanceID, BusySessions, Sessions, Messages []FollowerChange{ID, SessionID, Kind}}`). Only ids travel; DB is the source of truth.
- Follower (`internal/app/follower_sync.go`, `followerSync`, `App.StartFollowerSync` from `cmd/ipc_wiring.go`): subscribes to its own brokers, coalesces per id (created+updated → created, deleted wins), flushes every **150ms**; busy set = event sessions with `CoderAgent.IsSessionBusy`; heartbeat every 2s while busy; failed sends requeued, 2s backoff; own `ipc.Client`; RPC port re-read from the lock file each send; stops when promoted; `Shutdown` → final sync with empty busy set.
- Leader: `SetupIPC` registers `handleFollowerSync`, `agent.SetExternalRunCheck`, and `startExternalRunSweeper` (every TTL/4 republishes sessions whose follower expired). `externalRuns` registry: per-instance busy set, 10s TTL. Rows re-read and republished via `session.Republish`/`message.Republish` (local broker only). Sessions whose busy state flips are republished as Updated. Own instance id ignored, followers never consume republished topics → no loop. Remembrances indexer skips follower-owned sessions.
- Agent: `ErrSessionBusyElsewhere` in `runInternal` and `ResumeRun`.
- API: `sessionWithStatus` is now package-level with `sessionListItem()` (shared by list and push streams), `busy_elsewhere` always present. 409 `{code: busy_elsewhere}` on `/chat`, `/chat/stream`, `/sessions/{id}/steer`. `busy_elsewhere` also in session detail and `/pending`; the pending poll stays only as fallback (it also carries permissions/questions).

## Push streams (handlers_session_watch.go), coalesced per row id (200ms, latest state wins)
- `GET /api/v1/sessions/{id}/watch`: `session {sessionId,busyElsewhere}` → `message {sessionId, message: <Go message.Message JSON, same as history API>}` / `message_deleted {sessionId,id}` / `session_update {sessionId, session: <list item>}` → `done {sessionId}` when the external run ends (final session_update with busy_elsewhere=false first).
- `GET /api/v1/sessions/events` (global): `ready {}`, `session_upsert {session: <list item>}` (only top-level sessions with messages, like ListSessions), `session_deleted {id}`, `: keepalive` every 25s. Fires for local sessions, republished follower sessions and busy_elsewhere flips (incl. TTL expiry via the sweeper).
- End-to-end latency follower write → client ≈ 150ms + RPC + 200ms.

## WebUI
- `pando-client/services/liveUpdates.ts`: `applyRawMessage` (upsert by id, created_at order; role=tool fills matching tool_call results; keeps known results), `removeMessage`, `reconcileMessages` (identity-preserving merge), `upsertSession` (merge keeping client-only fields, sort updated_at/created_at desc), `removeSession`. All return the same array when nothing changed.
- `services/eventSource.ts` (EventSource with ?token=), `stores/sessionEventsStore.ts` (connect once in App.tsx after splash, exponential backoff 1s→30s, refetches the list once after a reconnect).
- sessionStore actions: `resyncActiveSession` (merge history in place), `applyPushedMessage`, `removePushedMessage`, `upsertPushedSession`, `removePushedSession`.
- `src/hooks/useExternalSessionWatch.ts`: EventSource on /watch, applies events in place; resync on open and on done; reconnect after 2s.
- No flicker: messages are never cleared/refilled, keys are message ids, unchanged message objects keep identity; MessageList follow-scroll now also reacts to last message part count/length.
- ChatView disables input + `.chat-notice` (`chat.busyElsewhere.notice`); Sidebar shows running state + `chat.busyElsewhere.label`; i18n in en/es/fr/de/pt/ja/zh.

## Verification
Go: follower_sync_test.go (registry TTL/flip/prune/expire, batching/heartbeat/requeue/final, two services on one DB, real ZMQ RPC), handlers_session_watch_test.go (409s, pending flag, watch typed+coalesced events, global events filtering), busy_elsewhere_test.go. `go build ./...`; `go test ./internal/api/... ./internal/app/... ./internal/session/... ./internal/message/... ./internal/ipc/... ./internal/llm/agent` pass; new handlers pass with -race. WebUI: typecheck OK, lint 0 errors (6 pre-existing warnings), vitest 125 pass (liveUpdates.test.ts, sessionStore.push.test.ts), build:embedded OK.

## Risks
- Busy tracking is leader-only (a WebUI served by a follower does not see other followers' runs).
- Extension event fan-out on the leader also sees republished events.
- A tool result whose tool_call message is not loaded yet is dropped until the resync on done.
- Sessions pushed that were outside the loaded page are inserted (total adjusted); deleting an unloaded session does not change the total.
- Not manually tested on the macOS desktop.

Related: [[feature_sqlite_multiwriter_engine_v130]], [[fix_ipc_failover_p0_inplace_promotion]], [[fix_resumed_run_live_updates_webui_acp]]
