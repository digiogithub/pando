---
created_at: 2026-09-30T17:08:27.788158971Z
updated_at: 2026-09-30T17:08:27.788158971Z
tags:
    - fix
    - models
    - context-window
    - modelsdev
    - openai-compatible
    - pando
---
# Fix: openai-compatible models all showed 128K context (GitHub issue #20, 2026-09-30)

## Problem

Issue #20 (v1.2.4): every model of an `openai-compatible` account (e.g. Kilo gateway) showed "128K ctx" in the model switcher. Two causes:

1. `fetchOpenAICompatibleModels` (`internal/llm/models/fetcher.go`) decoded only `id`/`created`. Kilo answers with an OpenRouter-shaped listing (`context_length`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, `supported_parameters`) that was discarded.
2. `modelsDevProviders` (`internal/llm/models/modelsdev_enrich.go`) has no entry for `ProviderOpenAICompatible`, so models.dev enrichment was a no-op and `fetchedModelContextWindow` fell back to 128_000. OpenCode Zen/Go `/models` only report ids, so they depend entirely on models.dev.

## Fix

- `fetcher.go`: extracted `parseOpenRouterStyleModels(body)` from `fetchOpenRouterModels`; `fetchOpenAICompatibleModels` now uses it too. Plain OpenAI listings decode to zero values (unchanged behaviour). Side effect: gateways that report `name` now show human names in the switcher.
- `modelsdev/catalog.go`: `Provider.API` (`json:"api"`, base URL published by models.dev for ~199/225 providers), `Catalog.byBaseURL` index, `Catalog.ProvidersForBaseURL(baseURL)`, `NormalizeBaseURL` (drops scheme, `www.`, trailing slashes and trailing `/v1`, lowercases; templated `${...}` URLs yield "").
- `modelsdev_enrich.go`: `RememberAccountBaseURL(accountID, baseURL)` (sync.Map registry), `modelsDevMetadataFor(ctx, provider, apiModel, baseURL)` and `modelMetadata(ctx, model)`; for openai-compatible the catalog providers are resolved from the account base URL. `EnrichModelFromModelsDev` and `EnrichRegisteredModels` use it. Public `ModelsDevMetadata` signature unchanged.
- `registry.go` `RefreshProviderModelsForAccount` and `internal/api/handlers_models.go` (live model listing) call `RememberAccountBaseURL` before fetching.
- Provider listing values still win over the catalog (enrichment fills only zero fields). Stale cached 128K entries are fixed by the startup refresh, which re-registers dynamic models.

Examples of matched URLs: Kilo `https://api.kilo.ai/api/gateway`, OpenCode Zen `https://opencode.ai/zen/v1`, OpenCode Go `https://opencode.ai/zen/go/v1`, DeepSeek `https://api.deepseek.com`.

## Verification

- New tests: `internal/llm/models/openai_compatible_test.go` (OpenRouter-shape decoding via httptest; base-URL enrichment with seeded on-disk models.dev cache in isolated HOME, unknown host keeps fallback), `modelsdev/catalog_test.go` (`TestProvidersForBaseURL`, `TestNormalizeBaseURL`).
- `go test ./internal/llm/models/... ./internal/api ./internal/llmproxy`, `go vet` OK.
- Live check against real endpoints: Kilo 397 models, only 29 at 128K (all genuinely 128K per the listing); Zen 84 models, 3 fallback; Zen Go 43 models, 9 fallback (models retired from models.dev).

## Not done (issue point 2)

OpenCode Zen/Go/Kilo presets in the add-provider UI. Caveat found: in models.dev many Zen/Go models set `provider.npm` to `@ai-sdk/anthropic` (/messages), `@ai-sdk/openai` (/responses) or `@ai-sdk/google`, so Pando's chat/completions client may fail for them; needs per-model routing or filtering before shipping presets.

Related: [[modelsdev_model_metadata_catalog]], [[context-window-fetch-and-tui-token-warning-2026-06-22]]
