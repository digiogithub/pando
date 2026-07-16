---
created_at: 2026-07-17T06:57:12.842020467Z
updated_at: 2026-07-17T06:57:12.842020467Z
tags:
    - fix
    - copilot
    - models
    - cache
    - routing
---
# Fix: MAI-Code-1-Flash still routed to /chat/completions (stale model cache)

Date: 2026-07-17. Follow-up to [[copilot_model_endpoint_and_capability_metadata]].

## Symptom

After the metadata-driven routing fix, `mai-code-1-flash-picker` (Copilot) *still*
went to `/chat/completions` even on a freshly compiled binary, despite the model
advertising `supported_endpoints: ["/responses"]` only.

## Root cause (two compounding defects)

1. **The refresh could never update an already-known model.**
   `LoadModelCache` (`internal/llm/models/cache.go`) restores cached dynamic
   models into **both** `SupportedModels` and `dynamicModels`. But
   `RefreshProviderModels` skipped any ID already present in `SupportedModels`
   ("don't overwrite statically defined models"), and `shouldSkipAccountScopedModel`
   did the same. Presence in `SupportedModels` does not mean *static* once a cache
   is loaded, so every cached model was frozen with whatever metadata the build
   that wrote the cache happened to persist. `~/.pando_models.json` on the dev
   machine had 452 entries, **all with `supported_endpoints` absent** (written by a
   pre-fix binary) — so `CopilotModelUsesResponsesAPI` saw no metadata, fell back
   to the `^gpt-(\d+)` name heuristic, and `mai-code-1-flash-picker` lost.

2. **No cache schema versioning.** Even with (1) fixed, the stale cache is live
   from startup until the async refresh lands. A missing field is
   indistinguishable from a legitimately empty one, so the wrong route is used in
   that window.

## Changes

`internal/llm/models/registry.go`
- New `isStaticModel(ModelID)`: static == registered **and not** in `dynamicModels`.
- `RefreshProviderModels`: skip only `isStaticModel`; previously fetched/cached
  models are re-registered so the live API stays authoritative.
- New `staticModelExistsByAPIModel` (static-only variant of
  `modelExistsByAPIModel`); used by the refresh dedup and the Antigravity branch
  of `shouldSkipAccountScopedModel`.
- `shouldSkipAccountScopedModel`: skip only on a static entry.
- `staticModelByAPIModel` / `StaticModelsForProvider`: also require
  `isStaticModel`, so cached account-less dynamic models can no longer masquerade
  as curated catalogue entries.

`internal/llm/models/cache.go`
- Added `cacheSchemaVersion = 1` and the `modelCacheFile{Version, Models}` layout.
  `SaveModelCache` writes it; `LoadModelCache` drops any cache whose version does
  not match. A pre-versioning cache (bare `map[ModelID]Model`) decodes with
  `Version == 0` and is discarded, then rewritten by the next refresh.
  Bump the constant whenever a newly persisted `Model` field changes behaviour.

## Verification

- New `internal/llm/models/cache_staleness_test.go`:
  `TestRefreshProviderModelsUpdatesStaleCachedMetadata`,
  `TestRefreshProviderModelsForAccountUpdatesStaleCachedMetadata` (both register a
  cached endpoint-less `mai-code-1-flash-picker`, serve `/responses` from an
  httptest Copilot `/models` stub, assert `CopilotModelUsesResponsesAPI` flips to
  true), `TestLoadModelCacheDropsUnversionedCache`,
  `TestModelCacheRoundTripPreservesEndpoints`.
- `go build ./...` clean.
- `go test ./internal/llm/models ./internal/llm/agent ./internal/api ./internal/llm/provider ./internal/config` all pass.

## Lesson

`SupportedModels` is a **merged** map (static + dynamic + cache). Any "is this
model already known?" guard written against it silently becomes "never update
anything after the first cache load". Check `dynamicModels` to tell origin apart.
Any persisted-metadata cache needs a schema version, or a field added later is
dead on every machine that already has a cache.
