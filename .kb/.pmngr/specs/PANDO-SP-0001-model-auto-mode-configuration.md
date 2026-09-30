---
id: PANDO-SP-0001
type: spec
title: "Model auto mode: configuration"
status: in_review
priority: high
author: mcp
labels: [model-routing, config, PANDO-EP-0015]
created: 2026-09-30T20:01:14Z
updated: 2026-09-30T21:06:56Z
started: 2026-09-30T21:06:56Z
requirements:
  R1:
    status: in_review
    trace:
      code: [internal/config/model_auto_mode.go]
      tests:
        - internal/config/model_auto_mode_test.go#TestModelAutoModeProviderDefaults
        - internal/config/model_auto_mode_test.go#TestModelAutoModeProviderValidation
  R2:
    status: in_review
    trace:
      code: [internal/config/model_auto_mode.go]
      tests:
        - internal/config/model_auto_mode_test.go#TestModelAutoModeRouteValidation
  R3:
    status: in_review
    trace:
      code:
        - internal/config/model_auto_mode.go
        - internal/config/agecrypto.go
        - internal/api/handlers_model_auto_mode.go
      tests:
        - internal/config/model_auto_mode_test.go#TestModelAutoModeAPIKeyEncryption
        - internal/api/handlers_model_auto_mode_test.go#TestGetModelAutoModeMasksKey
  R4:
    status: in_review
    trace:
      code:
        - internal/config/model_auto_mode.go#UpdateModelAutoMode
        - internal/config/config.go
      tests:
        - internal/config/model_auto_mode_test.go#TestUpdateModelAutoModePersistReload
        - internal/config/model_auto_mode_test.go#TestUpdateModelAutoModeLocked
        - internal/config/model_auto_mode_test.go#TestUpdateModelAutoModeRevertsOnWriteFailure
  R5:
    status: in_review
    trace:
      code: [internal/api/handlers_model_auto_mode.go, internal/api/routes.go]
      tests:
        - internal/api/handlers_model_auto_mode_test.go#TestPutModelAutoModeValidation
        - internal/api/handlers_model_auto_mode_test.go#TestPutModelAutoModeKeepsKey
---

## Purpose

This spec defines the `modelAutoMode` configuration block: the decision provider (Ollama, TypeSafe Jev or a custom Jev-compatible provider), the router model, the thresholds and the task routes. Each route has a primary model and up to 2 fallbacks. The spec also covers how the block is validated, persisted, secured and exposed over REST.

## Scope

In scope:
- `internal/config` (types, defaults, validation, persistence, hot reload)
- `pando-schema.json`
- `GET/PUT /api/v1/config/model-auto-mode`

Out of scope: runtime routing, which is covered by the spec "Model auto mode: prompt routing and provider failover".

Epic: PANDO-EP-0015. Stories: PANDO-US-0077.

## Requirements

### PANDO-SP-0001.R1 — Decision provider kinds and defaults

The system SHALL accept `modelAutoMode.router.provider` values `ollama`, `typesafe` and `custom`. A missing value SHALL default to `ollama`.

Base URL defaults per provider kind:

| Provider kind | Default base URL |
|---|---|
| `ollama` | the Ollama provider raw base URL, or `http://localhost:11434` if none is configured |
| `typesafe` | `https://api.typesafe.ai` |
| `custom` | none; an explicit http(s) `baseURL` is required |

#### Scenario: Ollama defaults
- GIVEN `modelAutoMode.enabled=true`, `router.provider` is unset, and `providers.ollama.baseURL=http://gpu-box:11434/v1`
- WHEN the config loads
- THEN the effective router provider is `ollama`
- AND the effective base URL is `http://gpu-box:11434`

#### Scenario: TypeSafe default base URL
- GIVEN `router.provider=typesafe` and no `baseURL`
- WHEN the config loads
- THEN the effective base URL is `https://api.typesafe.ai`

#### Scenario: Custom provider without base URL is invalid
- GIVEN `router.provider=custom` and an empty `baseURL`
- WHEN the config is validated
- THEN a validation error names the field `modelAutoMode.router.baseURL`

#### Scenario: Unknown provider kind
- GIVEN `router.provider=gemini`
- WHEN the config is validated
- THEN a validation error lists the allowed values

### PANDO-SP-0001.R2 — Task route validation (25 routes, 2 fallbacks, reserved none)

The system SHALL validate `modelAutoMode.routes` as follows:

- at most 25 enabled routes
- each `id` is non-blank and unique
- `none` SHALL NOT be used as an `id`
- `description` is non-empty and at most 500 characters
- `model` is required
- `fallbacks` has at most 2 entries, with no duplicates and none equal to the primary model

`threshold` SHALL be in (0,1]. When `enabled` is true, `router.model` SHALL be required. For the `ollama` provider, `router.model` SHALL NOT end in `:cloud`.

A model id that the registry does not know SHALL produce a warning, not an error.

#### Scenario: Too many routes
- GIVEN 26 enabled routes
- WHEN the config is validated
- THEN a validation error reports that the maximum is 25 enabled routes

#### Scenario: Three fallbacks rejected
- GIVEN a route with `fallbacks=[a,b,c]`
- WHEN the config is validated
- THEN a validation error names that route's `fallbacks`

#### Scenario: Fallback equals primary
- GIVEN a route with `model=x` and `fallbacks=[x]`
- WHEN the config is validated
- THEN a validation error is returned

#### Scenario: Reserved id
- GIVEN a route with `id=none`
- WHEN the config is validated
- THEN a validation error reports that `none` is reserved

#### Scenario: Unknown model is only a warning
- GIVEN a route whose model is not in `models.SupportedModels()`
- WHEN the config loads
- THEN loading succeeds
- AND a warning names the unknown model

#### Scenario: Ollama cloud model rejected as router
- GIVEN `router.provider=ollama` and `router.model=qwen3.5:cloud`
- WHEN the config is validated
- THEN a validation error reports that System One requires a local model

### PANDO-SP-0001.R3 — Router API key is encrypted, masked and env-resolvable

The system SHALL store `modelAutoMode.router.apiKey` encrypted at rest, in the same way as provider API keys. It SHALL resolve `$ENV_VAR` references at runtime.

For the `typesafe` provider, an empty key SHALL fall back to `$TYPESAFE_API_KEY`.

The plain key SHALL NOT be returned by any REST response, log line or telemetry event.

#### Scenario: Encrypted on disk
- GIVEN a PUT that sets `router.apiKey=sk-test-123`
- WHEN the config file is read from disk
- THEN the value is not the plaintext `sk-test-123`
- AND loading the config yields `sk-test-123` in memory

#### Scenario: Env fallback for TypeSafe
- GIVEN `router.provider=typesafe`, an empty `apiKey`, and env `TYPESAFE_API_KEY=abc`
- WHEN the router client is built
- THEN requests carry `Authorization: Bearer abc`

#### Scenario: Masked on GET
- GIVEN a stored key `sk-test-1234`
- WHEN `GET /api/v1/config/model-auto-mode` is called
- THEN the response has `apiKeySet=true` and a masked tail `••••1234`
- AND the response never contains `sk-test-1234`

### PANDO-SP-0001.R4 — Persistence, hot reload and config locks

`UpdateModelAutoMode` SHALL persist the block through `updateCfgFile`, SHALL revert the in-memory value if the write fails, SHALL refuse changes to locked fields with `ErrIfLocked("modelAutoMode…")`, and SHALL publish a config-reloaded event on the config bus.

A change made externally to the config file SHALL be hot-reloaded without restarting Pando.

#### Scenario: Persist and reload
- GIVEN auto mode is disabled
- WHEN `UpdateModelAutoMode` enables it with one route
- THEN the config file contains the route
- AND a fresh `Load()` returns an equal block

#### Scenario: Revert on write failure
- GIVEN the config file is read-only
- WHEN `UpdateModelAutoMode` is called
- THEN an error is returned
- AND the in-memory config equals the previous value

#### Scenario: Locked field
- GIVEN an enterprise lock on `modelAutoMode.router`
- WHEN a PUT changes `router.model`
- THEN the response is a lock error
- AND the config is unchanged

#### Scenario: External edit hot reload
- GIVEN Pando is running
- WHEN the config file's `modelAutoMode.threshold` is edited to 0.7
- THEN the next routing decision uses 0.7

### PANDO-SP-0001.R5 — REST endpoint GET/PUT /api/v1/config/model-auto-mode

The API SHALL expose `GET` and `PUT /api/v1/config/model-auto-mode`.

- The `PUT` SHALL validate the body and return field-level errors with status 400.
- A `PUT` with an empty `router.apiKey` SHALL keep the stored key.
- A `PUT` with `router.clearApiKey=true` SHALL remove the stored key.
- The endpoint SHALL require the same authentication as the other `/api/v1/config/*` endpoints.

#### Scenario: Field errors
- GIVEN a PUT body with `threshold=1.5`
- WHEN the handler runs
- THEN the status is 400
- AND the body lists `threshold` with a message

#### Scenario: Empty key keeps stored key
- GIVEN a stored key
- WHEN a PUT changes only `threshold`, with an empty `apiKey`
- THEN the stored key is unchanged

#### Scenario: Unauthenticated
- GIVEN WebUI basic auth is enabled
- WHEN the endpoint is called without credentials
- THEN the status is 401
