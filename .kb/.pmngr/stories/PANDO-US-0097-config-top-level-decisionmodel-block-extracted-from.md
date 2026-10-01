---
id: PANDO-US-0097
type: story
title: "Config: top-level `decisionModel` block extracted from `modelAutoMode.router`, one-shot migration, validation, encrypted key, REST + legacy aliases, JSON schema"
status: in_review
priority: high
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, config, api, migration]
estimate: 5
created: 2026-10-01T16:19:43Z
updated: 2026-10-01T16:33:26Z
started: 2026-10-01T16:33:26Z
---

## Description

As a user, I want the decision provider (Ollama / TypeSafe / custom Jev gateway) configured once, in its own section, so that model auto mode, persona auto-select and context filtering share it without me opening "Model auto mode".

Add `Config.DecisionModel` (`config.DecisionModelConfig{Router DecisionRouterConfig, TimeoutMs int}`) in a new `internal/config/decision_model.go`. Move from `model_auto_mode.go`: `DecisionRouterConfig`, `EffectiveProvider/BaseURL/APIKey`, timeout defaults, `MaskAPIKey`, the router part of `ValidateModelAutoMode`/`normalizeModelAutoMode`, `ClearModelAutoModeAPIKey`. `ModelAutoModeConfig.Router` is removed; `ModelAutoModeConfig.EffectiveTimeout()` becomes `DecisionModelConfig.EffectiveTimeout()`.

Migration in the loader (next to `normalizeModelAutoModeDefaults`): when `decisionModel.router.model == ""` and the legacy TOML/JSON key `modelAutoMode.router.model != ""`, copy the whole legacy router (including the encrypted key) into `decisionModel`, persist through `updateCfgFile`, log once. Keep decoding the legacy key so an old file never fails to load.

REST (`internal/api/handlers_decision_model.go`): `GET/PUT /api/v1/config/decision-model` (masked key, empty incoming key keeps stored one, `FieldError` list on 400), `DELETE /api/v1/config/decision-model/api-key`, and move `router/{models,test,health,pull,pull/{id}}` under `/api/v1/decision-model/`. Keep `/api/v1/model-auto-mode/router/*` and the `router` field of `GET /api/v1/config/model-auto-mode` as read-only aliases for one release with a once-per-process deprecation log. `PUT /api/v1/config/model-auto-mode` ignores `router` with a warning in the response `warnings`.

## Acceptance Criteria

- [ ] `decisionModel` block with viper defaults (`router.provider=ollama`, `timeoutMs=0` → 1500 local / 3000 remote), `ValidateDecisionModel` (provider enum, custom needs baseURL, http(s) URL, `:cloud` model rejected on ollama, typesafe key warning), normalisation, `ErrIfLocked("decisionModel")`, `UpdateDecisionModel`, `ClearDecisionModelAPIKey`, `ConfigChangeEvent{Section:"decisionModel"}`.
- [ ] `agecrypto.go` encrypts `DecisionModel.Router.APIKey`; `$ENV` references and `$TYPESAFE_API_KEY` fallback still work.
- [ ] `ValidateModelAutoMode` no longer validates the router but errors with `router.model` → `decisionModel.router.model is required when model auto mode is enabled` when enabled and the shared model is empty.
- [ ] Migration test: legacy-only file → migrated and persisted; already-migrated file → untouched; locked file → in-memory only with a warning; both present → `decisionModel` wins.
- [ ] `cmd/schema/main.go`, `internal/config/init.go` template (`[DecisionModel.Router]`), `cmd/init.go` and `.pando.toml` examples updated.
- [ ] REST handlers and tests (`handlers_decision_model_test.go`): masking, keep-key, clear-key, validation errors, alias deprecation, alias returns the same payload.
- [ ] `go test ./internal/config ./internal/api` green.

## Notes

- Anchors: `internal/config/model_auto_mode.go` (whole file), `config.go:2588-2594`, `agecrypto.go:417`, `init.go:624`, `internal/api/handlers_model_auto_mode.go`, `routes.go:221-228`.
- Do not change the TOML casing convention (`[DecisionModel.Router]`, `Provider`, `BaseURL`, …).
- Spec: "Decision model: configuration, migration and REST".
