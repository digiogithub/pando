---
created_at: 2026-07-21T09:21:59.805310922Z
updated_at: 2026-07-21T09:21:59.805310922Z
tags:
    - pando
    - feature
    - models
    - cost
    - modelsdev
    - webui
    - tui
---
# Feature: model pricing & capabilities from models.dev (2026-07-21)

## Motivation

The TUI/WebUI side panels show session token usage and, when known, **Cost** — but almost no
provider reports per-token pricing in its model-listing API, so `Model.CostPer1M*` stayed 0 for
every dynamically discovered model and `session.Cost` (computed in `TrackUsage`,
`internal/llm/agent/agent.go`) was always 0, hiding the row. Same for real context windows
(`fetchedModelContextWindow` fell back to a hardcoded 128K) and capability flags.

Fix: complete the metadata from the community catalog [models.dev](https://github.com/anomalyco/models.dev),
cached per instance, strictly best-effort (any failure keeps the previous behaviour).

## What was implemented

### New package `internal/llm/models/modelsdev`

- `catalog.go` — types (`Model`, `Cost{Input,Output,CacheRead,CacheWrite}`, `Limit{Context,Output}`,
  `Modalities`, `ReasoningOption`), `Catalog` with per-provider lookup indexes, `Lookup`,
  `LookupAny`, `Model.SupportsReasoningEffort()`. `lookupKeys` registers/queries each model under
  several spellings (exact, lowercase, vendor-prefix stripped, dated/versioned suffix stripped via
  `versionSuffix` regexp) so `claude-sonnet-4-5-20250929`, `anthropic/claude-sonnet-4-5` and
  `claude-sonnet-4-5-latest` all resolve. The **zero `Catalog` is valid** and answers "not found".
- `fetch.go` — models.dev publishes only a single ~3.2 MB `https://models.dev/api.json`
  (per-provider routes 302 to the site), so it is fetched **once per process** with `sync.Once`
  (`Get(ctx)`, memoizes the failure too, so a refresh never retries a 3 MB download per model) and
  cached on disk in `~/.pando_modelsdev.json` (`diskCache{Version,FetchedAt,Payload}`, TTL 24 h,
  schema version 1). Resolution order: fresh disk copy → network → **stale disk copy**. 20 s
  timeout, 64 MB read cap. Package var `Disabled` + test hook `Reset()`.

### Enrichment `internal/llm/models/modelsdev_enrich.go`

- `modelsDevProviders`: Pando provider type → ordered models.dev provider ids
  (anthropic, openai, google, google-vertex(+ -anthropic), azure, github-copilot, groq, openrouter,
  xai, amazon-bedrock, antigravity→google/anthropic/openai). **Local runtimes (Ollama, llama.cpp,
  local, openai-compatible) are deliberately unmapped** — they cost nothing to run and importing a
  hosted price for a same-named open-weights model would invent a cost. Guarded by a test.
- `ModelsDevMetadata(ctx, provider, apiModel)`, `EnrichModelFromModelsDev(ctx, *Model)`,
  `EnrichRegisteredModels(ctx)` (tops up the whole registry — needed for curated static catalogues
  such as Antigravity/Bedrock/Vertex that are hand-written without prices).
- `applyModelsDevMetadata` is pure and **only fills zero/empty fields**; booleans are only ever
  raised. Returns whether anything changed (via the comparable `enrichable` projection, since
  `Model` holds a slice and is not comparable). Note the inverted naming: Pando's
  `CostPer1MInCached` = models.dev `cache_write`, `CostPer1MOutCached` = `cache_read`.

### Wiring

- `internal/llm/models/registry.go`: `RefreshProviderModels` and `modelFromFetchedAccountModel`
  (now takes a `ctx`) build the model from the **raw** fetched limits, enrich, and only then apply
  `fetchedModelContextWindow` / `fetchedModelMaxOutputTokens`, so a catalog window beats the 128K
  fallback. The static-inherit branch is enriched too.
- `internal/llm/models/models.go`: new `Model.Description`, `Model.Knowledge`, `Model.ReleaseDate`.
- `internal/llm/models/cache.go`: `cacheSchemaVersion` 1 → **2** (a v1 cache carries zero costs).
- `internal/app/app.go` `RefreshDynamicModels`: calls `models.EnrichRegisteredModels(ctx)` first.
- `internal/config/config.go`: new `[ModelsDev] Enabled` (default true, `viper.SetDefault
  ("modelsDev.enabled", true)`), pushed down at the end of `Load` as `modelsdev.Disabled =
  !cfg.ModelsDev.Enabled` (the models package cannot import config). Documented in
  `internal/config/init.go` template and README (“Model pricing and capabilities (models.dev)”).

### UI

- `internal/api/handlers_models.go`: `ModelInfo` gains `supportsAttachments`, `contextWindow`,
  `maxOutputTokens`, `costPer1MIn`, `costPer1MOut`, `knowledge`, `releaseDate`. New
  `modelInfoMetadata()` fills them at all four construction sites; new `badgesForKnownModel()`
  derives badges from the **real blended price** (≤2 → fast+cost, ≤12 → fast, else capable, plus
  `reasoning`) and only falls back to the old ID heuristic `badgesForModel` when the price is
  unknown. The handler's own inline registration path now enriches before applying the 128K/4096
  defaults.
- TUI `internal/tui/components/dialog/models.go`: footer detail line for the highlighted model
  (`selectedModelDetails`, `formatTokenCount`) — `200K ctx · $3/$15 per 1M · reasoning+images ·
  cutoff 2025-01`. Kept out of the rows because the dialog is 44 columns wide.
- WebUI: `ModelCombobox.tsx` exports `formatTokenLimit` / `modelMetaLine` and renders the meta line
  under each model id; `ModelSwitcher.tsx` imports `modelMetaLine`, extends its `ModelInfo` and adds
  a `reasoning` badge colour.

## Fallback contract

Offline, HTTP error, disabled config, unknown provider or unknown model → no enrichment, no error
surfaced, models behave exactly as before (no cost shown). A missing price must never render as
"free" in the UIs.

## Verification

- `go build ./...`, `go vet` on the touched packages.
- `go test ./internal/llm/models/... ./internal/api ./internal/config ./internal/llm/agent
  ./internal/app ./internal/tui/...` — all pass.
- New tests: `modelsdev/catalog_test.go` (exact/suffix/vendor-prefix lookup, unknown provider,
  zero-catalog safety, `decode` rejecting empty/malformed payloads),
  `modelsdev_enrich_test.go` (fills missing, never overwrites known values, only raises
  capabilities, no-op without catalog + nil model, local providers unmapped),
  `internal/llm/models/main_test.go` `TestMain` sets `modelsdev.Disabled = true` so the package's
  unit tests stay offline.
- Live, opt-in (`PANDO_MODELSDEV_LIVE=1`): `modelsdev/live_test.go` resolved anthropic dated ids,
  openai/github-copilot `gpt-5.4`, google, openrouter `anthropic/claude-sonnet-4.5`, groq;
  `modelsdev_live_test.go` produced `copilot.gpt-5.4: ctx=1050000 max=128000 in=2.5 out=15
  cacheRead=0.25` through the real registry path.
- `web-ui`: `npx tsc --noEmit` clean, `npm run build` OK.

Related: [[copilot_stale_model_cache_endpoint_routing]], [[token_panel_context_window_plan]],
[[llm_proxy_model_resolution_and_anthropic]].
