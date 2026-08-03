---
created_at: 2026-08-03T22:05:15.994814544Z
updated_at: 2026-08-03T22:05:15.994814544Z
tags:
    - fix
    - models
    - concurrency
    - tui
---
# Fix: "concurrent map iteration and map write" on the model catalogue

Date: 2026-08-04

## Problem

Startup crash while building the TUI settings page:

```
fatal error: concurrent map iteration and map write
internal/tui/page.supportedModelOptions(...) internal/tui/page/settings.go:3185
  -> for _, model := range models.SupportedModels
```

`models.SupportedModels` was a plain global `map[ModelID]Model` read from every
surface (TUI, web API, agents, ACP) while background provider refreshes wrote to
it via `RegisterDynamicModel` / `LoadModelCache` / `EnrichRegisteredModels` /
prune. A long-standing data race; it started crashing after the Copilot token
exchange (see [[copilot_api_token_exchange_byok_custom_models]]) added an extra
network round trip to the startup refresh, widening the window.

## Change

`internal/llm/models/models.go` — the catalogue is now copy-on-write:

- `supportedModelsStore atomic.Pointer[map[ModelID]Model]` holds an immutable
  snapshot; `supportedModelsMu` serialises writers.
- `SupportedModels()` (was an exported var, now a function) returns the current
  snapshot. Readers get a map that is never written to.
- `updateSupportedModels(mutate)` clones, mutates and swaps. Public helpers:
  `SetSupportedModel`, `SetSupportedModels` (batch), `DeleteSupportedModels`.
- The static catalogue literal became `staticSupportedModels`; `init()` builds
  the initial snapshot from it plus the Azure/VertexAI/Gemini/Antigravity/
  Anthropic maps.

All ~129 read sites across 39 files became `SupportedModels()[...]` /
`range SupportedModels()` (mechanical rewrite); writes outside the package
(tests) go through `SetSupportedModel` / `DeleteSupportedModels`.

Because each write copies the map, the batch paths were converted to a single
update per pass:

- `RegisterDynamicModels([]Model)` (new) used by `RefreshProviderModels` and
  `RefreshProviderModelsForAccount`, including the static account-scoped branch.
- `pruneDynamicModelsForProvider` / `pruneDynamicModelsForAccount` collect the
  removed ids and call `DeleteSupportedModels(removed...)` once.
- `LoadModelCache`, `EnrichRegisteredModels` and `loadLocalModels` build a map
  and publish it with `SetSupportedModels`.

## Verification

- `go build ./...`, `timeout 900 go test ./internal/...` — all green.
- `go test -race ./internal/llm/models` — clean, including the new
  `internal/llm/models/models_concurrency_test.go`
  (`TestSupportedModelsConcurrentReadWrite` hammers 8 writer + 8 reader
  goroutines, `TestSupportedModelsSnapshotIsStable`, `TestSetSupportedModelsBatch`).
- Binary builds and runs (`pando --version` → v0.643.1).

## Note

`internal/tui/tui.go` is unformatted per gofmt, but that predates this change and
was left alone.
