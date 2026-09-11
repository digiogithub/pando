---
created_at: 2026-09-11T11:51:16.052294496Z
updated_at: 2026-09-11T11:51:16.052294496Z
tags:
    - analysis
    - telemetry
    - acp
    - ios-hang
---
# Analysis: why ACP sessions are almost invisible in Better Stack telemetry (2026-09-11)

Context: investigating iOS/Xcode report "Pando as ACP hangs / no response". Remote telemetry (Better Stack source `pando`, id 2751484, table t596690.pando) was enabled, but an ACP session run from Zed showed almost no ACP logs. Builds on [[pando/analysis/telemetry_acp_hang_analysis.md]] (telemetry itself cannot hang ACP).

## Root cause of missing logs
1. **ACP protocol logger is discarded by default.** `cmd/root.go:583` sets `logOutput := io.Discard`; only `--log-file` writes it to a file. All ~157 `logger.Printf` calls in `internal/mesnada/acp` (agent.go, prompt_handler.go, transport_stdio.go, session_state.go, permission_bridge.go) use this std `*log.Logger`, NOT slog, so they never go through the tee handler and never reach Better Stack (or even stderr / Zed).
2. **Only Info+ ships.** `internal/app/telemetry.go` `effectiveMinLevel` clamps to Info unless `cfg.Debug`. In agent loop only `logging.Info("Result", ...)` (agent.go:1279) is Info; everything else is Debug.
3. **No session_id on records.** `record.go:118` promotes a top-level `session_id` attr, but hot-path logs do not pass it, so all rows have empty session_id — cannot correlate a hanging session.
4. Flush interval 5s (options.go:16): SIGKILL of the ACP subprocess by the client loses the last <=5s (no `App shutdown` record for ACP pid 206790).

## What the Zed session actually showed
- ACP pid 206790 started 11:14:42 UTC, session 511ad861. Cancel at 11:19:52 ("request cancelled by user" in Zed.log, expected). Session continued fine; last assistant message finished 11:34:06 (DB). No user prompt persisted after that; process alive and idle (futex), no ongoing IO.
- Noise seen: repeated `sqlite3: database is locked` (session index, code reindex) while 2 mesnada subagent processes ran; `failed to create assistant message: sqlite3: interrupted` at 11:16:19 (context enrichment). Candidate contributors to stalls, not proven.
- Process rchar ~100 GB in 35 min (startup indexing/agentvcs hashing), then flat.

## Recommended fixes (not implemented yet)
- Route ACP `*log.Logger` into slog (e.g. `slog.NewLogLogger` or a writer bridging to `logging.Info`) so ACP lifecycle ships to telemetry, still keeping stdout clean.
- Promote key ACP lifecycle events to Info with `session_id`: initialize, session/new|load, prompt start/end + stop reason, cancel, permission request/response, tool call start/end, stdio read/write errors, EOF.
- Watchdog: log Warn when a prompt has no progress for N seconds (prompt in-flight heartbeat) to detect hangs remotely.
