---
created_at: 2026-06-30T14:22:09.035043679Z
updated_at: 2026-06-30T14:22:09.035043679Z
tags:
    - change
    - lean-ctx
    - token-optimization
    - savings-ledger
    - analytics
    - phase5
---
# Change: lean-ctx Phase 5 — Token-savings ledger + `pando gain`

**Date:** 2026-06-30
**Plan:** `pando/plans/leanctx_context_intelligence_plan.md` (Phase 5)
**Builds on:** P1 (read modes), P2 (dedup), P3 (bounce tracker) — closes the
skipped RTK analytics gap, making all token reduction measurable.

## Goal
Append-only JSONL ledger of every token-reduction event (compressed `view` reads,
F-reference dedup, RTK shell-output filtering) + a `pando gain` CLI, a
`pando_stats` MCP tool and a savings widget in the Token Optimization settings
section. Strictly observational, fail-safe, on by default.

## What changed

### New package: `internal/savings`
- `ledger.go` — `Entry{TS, Source(view|dedup|bash|search), Detail, BaselineTokens,
  ActualTokens, Mode, Saved}`. Process-wide `recorder` with a non-blocking buffered
  channel (depth 512) + background writer goroutine: `Record(Entry)` lazily inits
  from `config.Get().Data.Directory`, drops on saturation (never stalls the agent),
  appends to `<data-dir>/savings/ledger.jsonl`, rotates to a single `.1` backup at
  8 MB. Gated live by `config.SavingsLedgerEnabled()`. `Close()` flushes (wired into
  `app.Shutdown`). `TokensFromChars(n) = (n+3)/4` — the consistent estimator used at
  all emit points so cross-source totals are comparable.
- `report.go` — `Summarize(dataDir, SummaryOptions{Since})` reads the rotated `.1`
  backup then the active file, skips malformed lines, aggregates into `Report`
  {Events, Baseline/Actual/SavedTokens, ReductionPct, BySource (sorted desc),
  ByDay}. `LedgerPath`, `Report.EstimatedUSD(pricePerMillion)`.

### Config
- `config.SavingsLedgerEnabled()` helper (nil-safe; on by default; inverse of the
  existing `TokenOptimization.SavingsLedgerDisabled`).

### Emit points (all in `internal/llm/tools`, no-op when actual ≥ baseline)
- `view_modes.go`: new `recordSaving(source, detail, mode, baselineChars,
  actualChars)` + `bashSavingDetail`. Dedup `ReadDedupUnchanged` ⇒ SourceDedup
  (baseline=len(content), actual=len(stub)); `ReadDedupChanged` ⇒ dedup/"diff".
- `view.go` (Run + ACP): compressed branch ⇒ SourceView, mode=concrete,
  baseline=len(raw window), actual=len(rendered body).
- `bash.go` (Run + ACP): after `applyOutputFilter`, when a filter matched ⇒
  SourceBash, mode=filter name, baseline=`filterResult.Before`, actual=`.After`.

### MCP tool `pando_stats`
- `savings_stats.go` — `NewSavingsStatsTool()` (`pando_stats`, optional `days`
  param) renders the summary + per-source breakdown. Registered in
  `builtin_names.go`, `cmd/mcp_server.go` (next to cache_stats) and the coder agent
  toolsets in `internal/llm/agent/tools.go` (both gateway + non-gateway; the minimal
  TaskAgent subagent set intentionally omits it).

### CLI `pando gain` (aliases `stats`, `savings`)
- `cmd/gain.go` — loads config, `Summarize`s the ledger, prints a human table
  (scope, events, saved + %, baseline→actual, per-source, last-14-day rollup) or
  `--json`. Flags `--days N`, `--price <per-1M>` (est. USD), `--json`. Smoke-tested
  end-to-end.

### API + WebUI widget
- `GET /api/v1/savings` (`handlers_config.go` `handleSavings`, route in `routes.go`):
  read-only, optional `?days=N`, returns `Report` + `ledgerEnabled`.
- `TokenOptimizationSettings.tsx`: new read-only `SavingsWidget` (fetch on mount +
  refresh button) under the existing Savings sub-section — total saved, % reduction,
  per-source table. New `SavingsReport`/`SavingsSourceTotal` types.
- i18n: `common.refresh` + `settings.tokenOptimization.savingsWidget{Title,Empty,Saved}`
  in all 7 locales. `web-ui/dist` rebuilt.

## Verification
- `go build ./...`; `go test -race ./internal/savings/... ./internal/llm/tools/`;
  `go test ./internal/llm/agent ./internal/api ./internal/savings/...` all green.
- New `internal/savings/savings_test.go`: TokensFromChars, missing ledger ⇒ zero,
  aggregation + sort + ReductionPct + EstimatedUSD, Since filter, rotated-backup
  read, malformed-line skip, recorder rotation.
- `tsc --noEmit` clean; `npm run build` OK.
- `pando gain --price 3` / `--json` smoke-tested against a synthetic ledger.
- Pre-existing config failures unchanged/unrelated (init.go template refactor +
  viper nested-default shadow).

## Notes
- Token baselines use raw-window char counts (line-numbered full output is slightly
  larger), so reported savings are mildly conservative — intentional.
- P3 `BounceTracker.WastedBytes` is available to subtract bounce waste from reported
  savings in a future refinement; not yet subtracted in the ledger totals.
- Remaining lean-ctx phases: P4 (code property graph), P6 (optional transcript
  compaction / session brief / budget guard).
