---
created_at: 2026-09-11T12:04:56.332327351Z
updated_at: 2026-09-11T12:04:56.332327351Z
tags:
    - change
    - acp
    - telemetry
    - logging
    - ios-hang
---
# Change: ACP logger bridged to slog + Info lifecycle events with session_id (2026-09-11)

Follows [[pando/analysis/acp_telemetry_visibility_gap.md]]. Related: [[pando/analysis/sqlite_locked_interrupted_errors.md]], [[pando/analysis/telemetry_acp_hang_analysis.md]].

## Motivation
Investigating an iOS/Xcode report "Pando in ACP mode hangs". Remote telemetry (Better Stack) showed almost nothing from ACP sessions: in stdio mode the ACP `*log.Logger` wrote to `io.Discard` unless `--log-file` was passed, all ~157 `logger.Printf` calls in `internal/mesnada/acp` bypassed slog (so never reached the tee handler / telemetry), and the agent loop "Result" log had no `session_id`.

## What changed
- **New `internal/logging/std_bridge.go`**: `NewStdLogger(component, level) *log.Logger` — a *log.Logger whose writer emits each line into `slog.Default()` (resolved at write time, so it follows later `slog.SetDefault` and live level changes) with attr `component`. Never writes to stdout. Tests: `internal/logging/std_bridge_test.go`.
- **`cmd/root.go` (`runACPServerWithOptions`)**: ACP logger = `logging.NewStdLogger("acp", slog.LevelDebug)`; removed the ad-hoc file handle / `io.Discard` / `quietStdioLogs`. Startup logged as Info `acp: starting stdio agent` (version, cwd, debug, log_file, auto_permission) after `config.Load`; `acp: auto-permission forced on for stdio mode` at Info. `--log-file` still implies `cfg.Debug=true` (config.go:1883), so the full protocol trace (Debug) still lands in the file exactly as before.
- **ACP fallbacks** `log.Default()` → `logging.NewStdLogger("acp", slog.LevelDebug)` in agent.go, transport_stdio.go, transport_http.go (desktop/serve HTTP ACP passed nil and logged every request at Info via log.Default), permission_bridge.go, plan_service.go, agent_simple.go.
- **Info/Warn/Error structured events (`acp: ...`, top-level `session_id` so telemetry promotes it)** — existing Printf lines kept as Debug trace (tests assert some of them):
  - agent.go: `acp: initialize` (client_name, client_version, protocol_version, fs_read, fs_write, terminal); `acp: session created|loaded|resumed|closed` (cwd, mode); `acp: session create failed`; `acp: load/resume session not found`; `acp: cancel requested` / `acp: cancel for unknown session`; `acp: prompt started` (mode, prompt_chars, attachments); `acp: steering queued` (pending); `acp: session mode|model|persona|config option set`.
  - Prompt outcome via `defer logPromptOutcome(...)` (Prompt now has named results): `acp: prompt completed` (stop_reason, duration_ms), `acp: prompt cancelled` (Info; detected by context.Canceled, ctx.Err or "cancelled by user" message), `acp: prompt failed` (Error).
  - permission_bridge.go: `acp: permission requested`, `acp: permission resolved` (outcome, wait_ms), `acp: permission request failed`, `acp: permission denied, no client connection`.
  - transport_stdio.go: `acp: stdio transport started|stopped` (reason), `acp: stdin closed by client (EOF)`, `acp: stdin read error`, `acp: dropped oversized JSON-RPC line`, `acp: shrunk oversized payload`, `acp: forwarding oversized payload as-is`.
  - session.go `SendUpdate`: `acp: session update to client failed` (elapsed_ms, error), throttled to 1 per 10s per session (field `lastUpdateErrLog`, guarded by `updateMu`) — signals a client that stopped reading stdout.
- **internal/llm/agent/agent.go**: `"Result"` Info log now includes `session_id` (all modes).

## Hang diagnosis with these events
In Better Stack: filter `message LIKE 'acp:%'` by `session_id`. `prompt started` without `completed/cancelled/failed` = stuck turn; `permission requested` without `resolved` = waiting on client dialog (RequestPermission uses context.Background, no timeout); `session update to client failed` = client not draining stdout; `stdin closed (EOF)` = client went away.

## Verification
- `gofmt -l` clean; `go build ./...` OK; `go vet ./internal/logging ./internal/mesnada/acp ./cmd` clean.
- `go test ./internal/logging ./internal/mesnada/acp ./cmd` pass. `./internal/llm/agent` has 4 pre-existing failures (TestSetAndGetCavemanMode, TestCavemanActivatesTheSessionPolicyPath, TestCavemanSessionPolicyInstructions, TestApplyToolDiscoveryWithoutManagerIsUnchanged) caused by the tests reading the real project `.pando.toml` ([Caveman], [ToolDiscovery]) — unrelated to this change (only a log attr added there).
- Smoke test: scratch binary `pando acp --log-file` in an isolated temp dir, piped initialize + session/new then EOF: stdout only JSON-RPC, stderr empty, log shows `acp: starting stdio agent`, `acp: stdio transport started`, `acp: initialize client_name=smoke-client ...`, `acp: session created session_id=...`, `acp: stdin closed by client (EOF)`, `acp: stdio transport stopped reason="connection closed"`.
