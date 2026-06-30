---
created_at: 2026-06-30T09:02:41.102137054Z
updated_at: 2026-06-30T09:02:41.102137054Z
tags:
    - change
    - lean-ctx
    - token-optimization
    - read-modes
    - view
    - cache
    - phase1
    - phase2
---
# Change: lean-ctx Context-Intelligence Phases P1 + P2 (read modes + re-read dedup)

**Date:** 2026-06-30
**Plan:** `pando/plans/leanctx_context_intelligence_plan.md` (Phases 1 & 2)

## What was implemented

### Phase 1 — Smart file-read modes on `view` (additive over existing pagination)
New `mode` param on the `view` tool: `full` (default — byte-identical to legacy), `signatures`, `map`, `auto`.
- New package `internal/llm/tools/readmode/`:
  - `readmode.go` — `Mode` type, `Normalize`, `Resolve` (deterministic auto resolver mirroring lean-ctx `resolve_inner`: instruction files / non-code / diagnostics-active / small windows → full; medium code → map; large code → signatures), `IsCodeLanguage`, instruction-file detection. Thresholds: `smallAutoBytes=1500`, `largeAutoBytes=12000`.
  - `parse.go` — lazy shared `treesitter.Parser`+`ASTWalker` (IncludeSourceCode=false); `ParseSymbols(ctx, path, content)` returns symbols+lang+ok (false ⇒ fall back to raw). Per-language regex `extractImports` (go/ts/js/python/rust/java/php) with Go scoped to import region via `goImportRegion`.
  - `render.go` — `RenderSignatures` (all symbols, indented by ParentID depth) and `RenderMap` (imports + top-level symbols only). Every entry carries real source line `L<StartLine+lineOffset>` so the agent can follow up with offset/limit. Signature collapsed to one line; falls back to `<type> <name>`; optional docstring summary.
  - `diff.go` — `DiffWindows` compact prefix/suffix-trimmed line diff (caps at 200 lines/side) for changed re-reads.
- `view.go`: added `Mode` to `ViewParams` + JSON schema (enum). Both the normal Run path and the ACP path now call the new helpers.
- `view_modes.go` (new, package tools): `displayPath`, `dedupViewRead` (P2), `renderViewMode` (P1). **Hard safeguard:** if the compressed render is not strictly smaller than the raw window, or parsing fails, it returns ok=false ⇒ raw full window (never emits more than the raw read). `full` default ⇒ byte-identical to legacy output (first read always proceeds normally).

### Phase 2 — Content-hash F-references for unchanged re-reads (on by default)
- `cache.go`: `SessionCache` gained `readWindows map[string]*readWindowRecord`, `readLabels`, `dedupHits`. New `RecordRead(path, startLine, endLine, content) ReadDedupResult` keyed by `path\x00start-end`, SHA-256 hash of window content. Statuses: `ReadDedupNew` (assign stable label `F<n>`), `ReadDedupUnchanged` (identical ⇒ stub, increments DedupHits, keeps record), `ReadDedupChanged` (returns PrevContent for diff, updates record, keeps label). Content retained for diff capped at 256KB. `Clear` resets all dedup state; `Stats()`/`CacheStats` expose `DedupHits`.
- `view.go`: before rendering, `dedupViewRead` collapses an unchanged re-read to `[unchanged: <relpath> lines a-b — content identical to earlier read F<n> ...]` or emits a `<file ... changed-since=F<n>>` diff. New windows proceed normally (no header change ⇒ Phase-1 byte-identical guarantee preserved even with dedup on).
- `cache_stats.go`: shows "Unchanged re-read dedup hits".

### Config (backing P1+P2)
- `config.go`: new `TokenOptimizationConfig` struct (`[TokenOptimization]`) with `ReadModeDefault` ("full"), `ReadDedupDisabled` (false), plus forward fields for later phases (`ReadModeLearning`, `BuildCodeGraph`, `RelatedFilesHint`, `SavingsLedgerDisabled`). Field added to `Config`. Helpers `ResolveReadModeDefault()` (env `PANDO_READ_MODE_DEFAULT` override → config → "full") and `ReadDedupEnabled()` (nil-safe). viper defaults set under `tokenOptimization.*`.

## Files touched
- New: `internal/llm/tools/readmode/{readmode,parse,render,diff}.go` + `readmode_test.go`
- New: `internal/llm/tools/view_modes.go`, `internal/llm/tools/cache_dedup_test.go`
- Modified: `internal/llm/tools/view.go`, `cache.go`, `cache_stats.go`, `internal/config/config.go`

## Verification
- `go build ./...` clean; `go vet ./internal/llm/tools/... ./internal/config/...` clean.
- `go test -race ./internal/llm/tools/ ./internal/llm/tools/readmode/` green.
- Verified-command suite `go test ./internal/llm/agent ./internal/api` green.
- Tests cover: Normalize, auto Resolve matrix, Go signatures/map render + line offset, TS map-omits-nested vs signatures-includes-nested, unsupported-ext fallback, DiffWindows, extractImports, RecordRead new/unchanged/changed + Clear reset. Existing `view_test.go` still passes (full-mode path unchanged).

## Deferred (NOT in this change)
- The unified **"Token Optimization" settings section** (WebUI `TokenOptimizationSettings.tsx` + TUI `buildTokenOptimizationSection` + surfacing the existing RTK toggle + 7-locale i18n) — that is the plan's separate cross-cutting "Configuration & UI" section. Backend config struct/defaults/env are in place; UI wiring is the next task.
- Phases 3 (bounce tracker), 4 (code graph), 5 (savings ledger), 6 (compaction) remain per plan.
