---
created_at: 2026-06-27T22:23:55.042396448Z
updated_at: 2026-06-27T22:23:55.042396448Z
tags:
    - change
    - code-indexing
    - sqlite
    - vacuum
    - ipc
    - slash-command
---
# Change: `/db-compact` slash command + IPC `db.compact` RPC routing (Phase 6)

Date 2026-06-28. Implements Phase 6 of `pando/plans/code_index_drop_source_text_and_db_compact_plan.md`, completing the DB-compaction feature (Phases 1 + 5 were done 2026-06-27, see `pando/changes/code_index_drop_source_text_phase1_5.md`).

## Goal

Expose database compaction (VACUUM) as a `/db-compact` slash command in every interface (TUI, WebUI, ACP) and make the standalone `pando db compact` CLI cooperate with a running instance. Only the IPC **primary** owns DB writes, so VACUUM must run there; secondaries and the CLI route the request to the primary instead of opening a second writer (which would only collide with "database is locked").

## What changed

### Protocol (`internal/ipc/protocol/rpc.go`)
- New `MethodDBCompact = "db.compact"`.
- New `DBCompactParams{Incremental, EnableAutoVacuum}` and `DBCompactResult{Mode, SizeBefore, SizeAfter, Freed}`.

### IPC client (`internal/ipc/client.go`)
- New `CallWithTimeout(ctx, endpoint, method, params, timeout)`; `Call` now delegates to it with the default `CallTimeout`. Needed because a full VACUUM on a large DB far exceeds the default 10s call timeout (effective deadline is min(ctx, timeout)).

### App funnel (`internal/app/app.go`)
- New unexported field `App.rwConn *sql.DB` (the primary's read-write connection). Set in `New()` from the `conn` param and refreshed in `PromoteToPrimary` after failover acquires a new RW connection.
- New `App.CompactDatabase(ctx, incremental, enableAutoVacuum) (protocol.DBCompactResult, error)` — the single funnel used by all UIs and the IPC handler:
  - Secondary (`!IPCIsPrimary && ipcClient != nil`): forwards `db.compact` to the primary over IPC with `dbCompactCallTimeout = 30m`, decodes the result.
  - Primary / standalone: runs `db.Compact` directly on `rwConn`.
- New `acpDBCompactorAdapter` + `App.ACPDBCompactor()` returning a `mesnadaACP.DBCompactor` (translates `protocol.DBCompactResult` → `acp.DBCompactResult`) to avoid an import cycle.

### IPC handler (`cmd/bridge_delegation.go`)
- `registerBridgeHandlers` now also registers `bus.RegisterMethod(protocol.MethodDBCompact, …)` calling `pandoApp.CompactDatabase` (runs locally — this bus only exists on the primary). Registered at every entrypoint (tui/acp/serve/app/desktop + failover-promoted buses) via the existing single funnel, so no per-entrypoint edits were needed.

### CLI (`cmd/db.go`)
- `pando db compact` (+ hidden alias `pando db-compact`) now PREFERS forwarding: `compactViaRunningInstance` reads the IPC lock with `ipc.ReadLockForPath(cwd)`, probes liveness via `instance.ping` (3s), and if a live primary holds the lock forwards `db.compact` over IPC with a 30m timeout. A stale lock (dead primary) or no lock → falls back to an in-process VACUUM. Method-not-found from an old running instance → clear "too old" message. Local-path BUSY still prints the friendly "stop the other instance" hint.

### Slash command registration + dispatch
- `internal/commands/registry.go`: added `{Name:"db-compact", …}` to `BuiltinCommands()` → auto-surfaces in TUI slash completion and the WebUI (`GET /api/v1/commands`).
- TUI (`internal/tui/page/chat.go`): `handleDBCompactCommand` runs `app.CompactDatabase` off the UI thread (tea.Cmd) and reports via `util.InfoMsg`; wired into `sendMessage`. Local `formatChatBytes` helper.
- WebUI/API (`internal/api/handlers_chat.go`): new `case "db-compact"` in `handleSlashCommandStream`, streams SSE `content_delta` with reclaimed bytes; new `humanizeBytes` helper.
- ACP: new `dbCompactCommandToken/Name` (`session_state.go`), `slashCommandDBCompact` kind + spec (`slash_commands.go`), `processDBCompactCommand` handler + `formatACPBytes` (`goal_commands.go`), and an optional `DBCompactor` interface + `DBCompactResult` type + `SetDBCompactor` setter on `PandoACPAgent` (`agent.go`). Injected via `pandoApp.ACPDBCompactor()` at both ACP construction sites (`cmd/root.go` standalone stdio, `internal/app/app.go` embedded HTTP transport). If no compactor is injected, the command reports "not available".

## Design notes
- Single funnel `App.CompactDatabase` keeps primary-local vs secondary-forward in one place; the IPC handler reuses it (no re-entrancy loop: forwarded calls land on the primary where `IPCIsPrimary` is true → local path).
- VACUUM runs on a pooled connection shared with the writecoordinator; SQLite serializes (busy_timeout), so a concurrent write may wait. Acceptable for a maintenance command; errors are surfaced.
- Matched the existing slash-command pattern (English descriptions, no per-command i18n — consistent with `/compact`, `/goal`).

## Verification
- `go build ./...` clean; `go vet` clean on all touched packages (pre-existing unrelated warning in `internal/mesnada/agent/spawner_template.go`).
- Tests: `internal/app/compact_test.go` (`TestCompactDatabaseLocalPath` churns+deletes then asserts full VACUUM shrinks DB, sets auto_vacuum=2, incremental follow-up works; `TestCompactDatabaseNoConnection` errors clearly). ACP `TestParseSlashCommand_UsesRegistry` + `TestAvailableCommands_ExposeGoalSlashCommands` updated for the 7th command. `go test ./internal/mesnada/acp ./internal/ipc/... ./internal/db ./internal/llm/agent ./internal/api` all green; `-race` green on `internal/app`. CLI verified (`pando db compact --help`).

## Status
**Phase 6 DONE → whole "drop source text + DB compaction + /db-compact" feature COMPLETE (Phases 1,5,6).** Future-optional: route VACUUM through the writecoordinator for strict single-writer serialization; per-command i18n if the project later i18n's the slash registry.
