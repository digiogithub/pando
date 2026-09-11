---
created_at: 2026-09-11T11:24:44.802308801Z
updated_at: 2026-09-11T11:24:44.802308801Z
tags:
    - analysis
    - telemetry
    - acp
    - ios-hang
---
# Analysis: can commit 60acf60c (remote telemetry) hang ACP stdio mode?

Task: task-65657173. iOS user reports pando hangs in ACP mode. Scope: telemetry subsystem only. Analysis only, no fixes.

## Verdict: NO — telemetry cannot deadlock ACP stdio. All send paths are non-blocking or time-bounded (≤2s, only on panic/shutdown).

## Key evidence
- Enqueue never blocks: `select/default` drop-on-full (internal/telemetry/shipper.go:128-134); queue bounded at 1000 (options.go:13).
- No mutex held across HTTP: Shipper has no mutex at all; HTTP happens only in the single worker goroutine (shipper.go:237-266, 452-487).
- Flush/Stop bounded by caller ctx (`select` with ctx.Done, shipper.go:180-197, 201-222); RecoverPanic uses 2s ctx (logging/logger.go:97-98).
- config.Bus.Publish non-blocking select/default (config/eventbus.go:62-66); telemetryRuntime.busCh buffered 8 (app/telemetry.go:67).
- Tee handler hot path: atomic loads only, no locks (logging/remote_sink.go:48,62-68; tee_handler.go:57-98).
- All regex compiled at package level (redact/patterns.go:10-53, redact/value.go:12) — no MustCompile inside hot-path functions.
- Telemetry IS initialized in ACP mode (cmd/root.go:629 StartupMode "acp" → app/app.go:340 initTelemetry), but it is opt-in: viper default telemetry.enabled=false; also gated by build-time token (telemetry.Available()). An enterprise overlay can lock telemetry.enabled=true (config/config.go:1856-1867).
- slog primary handler writes to file/in-memory writer, never stdout → no ACP JSON-RPC framing corruption from logging.

## Residual risks (slowdowns, not hangs)
1. Redaction runs in the CALLING goroutine: tee_handler.go:96 → Shipper.Handle → NewRecord (record.go:108) → redact.Value before truncation (record.go:169). ~17 regex passes O(n) over the FULL string attr before the 2KiB cap (patterns.go:79-95). Huge logged strings (file dumps/tool results) = CPU spike on the logging goroutine while telemetry is enabled.
2. Worker stuck worst-case ~31.5s on dead backend (3 attempts × 10s HTTPTimeout + 0.5s+1s backoff, shipper.go:26-27,354-394): during that window the queue fills and drops; callers unaffected.
3. Worst-case queue memory ~1000 records × (8KiB msg + 16KiB attrs budget) ≈ 25 MB if backend unreachable — bounded, minor for iOS.
4. tr.mu held across shipper.Stop (≤2s) in telemetry.go:184-197 — only watch/shutdown goroutines, never hot path.

## Not part of this commit
- logging/writer.go in-memory LogData buffer is unbounded but pre-existing (unchanged in 60acf60c).

Recommendation: iOS ACP hang must be investigated elsewhere (stdio/IPC loop, write coordinator, permissions path).
