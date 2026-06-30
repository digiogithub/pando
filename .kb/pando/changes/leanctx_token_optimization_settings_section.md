---
created_at: 2026-06-30T09:27:00.282473857Z
updated_at: 2026-06-30T09:27:00.282473857Z
tags:
    - change
    - lean-ctx
    - token-optimization
    - settings
    - ui
    - tui
    - webui
    - i18n
    - config
---
# Change: unified "Token Optimization" settings section (TUI + WebUI + i18n)

**Date:** 2026-06-30
**Plan:** `pando/plans/leanctx_context_intelligence_plan.md` — "Configuration & UI" cross-cutting section.
**Follows:** `pando/changes/leanctx_p1_p2_read_modes_and_dedup.md` (P1+P2 backend already shipped the `TokenOptimizationConfig` struct/defaults/env).

## What was implemented
A dedicated **Token Optimization** settings section that surfaces the new `[TokenOptimization]` knobs plus the existing RTK shell-output toggle (which stays on `Bash.OutputFilterDisabled` — surfaced, not moved).

### Backend / API
- `config.go`: new `UpdateTokenOptimization(TokenOptimizationConfig) error` (mirrors `UpdateBash`, persists via `updateCfgFile`, rolls back on error).
- `internal/api/handlers_config.go`: new `handleConfigTokenOptimization` (GET/PUT) + DTO `TokenOptimizationConfigResponse` (embeds `config.TokenOptimizationConfig` + surfaced `outputFilterEnabled` (inverse of `Bash.OutputFilterDisabled`) + `outputFilterPaths`). PUT applies the TokenOptimization config **and** writes the inverted RTK enable flag + paths back into `Bash`.
- `internal/api/routes.go`: registered `/api/v1/config/token-optimization`.

### WebUI
- `web-ui/src/types/index.ts`: new `TokenOptimizationConfig` interface (incl. surfaced `outputFilterEnabled`/`outputFilterPaths`).
- `web-ui/src/stores/settingsStore.ts`: new `useTokenOptimizationStore` (fetch/updateField/save/reset against the new endpoint, defaults merge).
- `web-ui/src/components/settings/TokenOptimizationSettings.tsx`: new component with 4 sub-sections — **File reads** (`readModeDefault` select full/auto/signatures/map, `readDedup` toggle inverted, `readModeLearning`), **Shell output (RTK)** (`outputFilterEnabled` toggle + `outputFilterPaths` via `TagListEditor`), **Code graph** (`buildCodeGraph`, `relatedFilesHint`), **Savings** (`savingsLedger` toggle inverted). Save/Reset buttons mirror `BashSettings`.
- `web-ui/src/components/settings/SettingsView.tsx`: registered category `token-optimization` (after `bash`) + render branch + import.

### TUI
- `internal/tui/page/settings.go`: new `buildTokenOptimizationSection(cfg)` (FieldSelect for read mode + toggles + text for filter paths; RTK toggle reads `!cfg.Bash.OutputFilterDisabled`), registered under the **Tools** group after `buildBashSection`. New `saveTokenOptimization(field)` routed via the `tokenOptimization.` key prefix in `saveField`; the `outputFilter`/`outputFilterPaths` keys persist to `Bash` (surfaced), the rest to `TokenOptimization`.

### i18n
- Added `settings.categories.tokenOptimization` + the full `settings.tokenOptimization.*` namespace (loading/title/description, 4 section headers, read-mode select labels, all toggle labels+descriptions) to **all 7 locales** (en, es, fr, de, pt, ja, zh). Minimal per-file diff (~32 lines each).

## Verification
- `go build ./...` clean; `go test ./internal/api ./internal/config/... ./internal/tui/...` green.
- `web-ui`: `tsc --noEmit` clean; `npm run build` succeeds (dist regenerated for the embedded UI).
- RTK toggle in the new section flips `Bash.OutputFilterDisabled` on save (WebUI PUT + TUI saveTokenOptimization); read-mode/dedup/etc. persist under `[TokenOptimization]`.

## Notes
- The General settings tab still has its own `output_filter_enabled` toggle; both write the same `Bash.OutputFilterDisabled` consistently (hot-reload SSE re-syncs on next fetch).
- Code-graph toggles (`BuildCodeGraph`, `RelatedFilesHint`) are present for parity but back Phase 4, not yet wired to indexing.

## Status
**P1 + P2 + the Token Optimization Configuration/UI section are now COMPLETE.** Remaining: P3 (bounce tracker), P4 (code graph), P5 (savings ledger + `pando gain` widget that will live in the Savings sub-section), P6 (optional).
