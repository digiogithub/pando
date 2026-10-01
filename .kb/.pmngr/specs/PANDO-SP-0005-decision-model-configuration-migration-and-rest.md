---
id: PANDO-SP-0005
type: spec
title: "Decision model: configuration, migration and REST"
status: backlog
priority: high
author: mcp
labels: [decision-model, config, PANDO-EP-0018]
created: 2026-10-01T17:06:38Z
updated: 2026-10-01T17:10:58Z
requirements:
  R1:
    status: backlog
    trace:
      code: [internal/config/decision_model.go, internal/config/config.go]
      tests:
        - internal/config/decision_model_test.go#TestDecisionModelEffectiveTimeout
        - internal/config/decision_model_test.go#TestDecisionModelDefaultsReachLoadedConfig
        - internal/config/decision_model_test.go#TestNormalizeDecisionModel
    verified: {rev: "sha256:7d767980a3222c36", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R2:
    status: backlog
    trace:
      code:
        - internal/config/decision_model.go
        - internal/config/model_auto_mode.go
      tests:
        - internal/config/decision_model_test.go#TestValidateDecisionModel
        - internal/config/decision_model_test.go#TestUpdateDecisionModelRejectsInvalid
        - internal/config/model_auto_mode_test.go#TestModelAutoModeRequiresSharedDecisionModel
    verified: {rev: "sha256:90880481dad1ad76", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R3:
    status: backlog
    trace:
      code:
        - internal/config/decision_model.go
        - internal/config/agecrypto.go
        - internal/api/handlers_decision_model.go
      tests:
        - internal/config/decision_model_test.go#TestDecisionModelAPIKeyEncryptionKeepAndClear
        - internal/api/handlers_decision_model_test.go#TestDecisionModelGetMasksKey
        - internal/api/handlers_decision_model_test.go#TestDecisionModelPutKeepsAndClearsKey
    verified: {rev: "sha256:0475ff8176e15969", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R4:
    status: backlog
    trace:
      code: [internal/config/decision_model.go]
      tests:
        - internal/config/decision_model_test.go#TestUpdateDecisionModelLocked
        - internal/config/decision_model_test.go#TestDecisionModelAPIKeyEncryptionKeepAndClear
    verified: {rev: "sha256:ca43036b97156046", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R5:
    status: backlog
    trace:
      code:
        - internal/config/decision_model.go#migrateLegacyDecisionModel
        - internal/config/model_auto_mode.go
      tests:
        - internal/config/decision_model_test.go#TestMigrateLegacyRouterPersists
        - internal/config/decision_model_test.go#TestMigrateAlreadyMigratedUntouched
        - internal/config/decision_model_test.go#TestMigrateBothPresentDecisionModelWins
        - internal/config/decision_model_test.go#TestMigrateLockedStaysInMemory
    verified: {rev: "sha256:31d4dc3f0362e2b2", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R6:
    status: backlog
    trace:
      code: [internal/api/handlers_decision_model.go, internal/api/routes.go]
      tests:
        - internal/api/handlers_decision_model_test.go#TestDecisionModelGetMasksKey
        - internal/api/handlers_decision_model_test.go#TestDecisionModelPutKeepsAndClearsKey
        - internal/api/handlers_decision_model_test.go#TestDecisionModelPutValidationErrors
    verified: {rev: "sha256:08fa799d317780d2", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R7:
    status: backlog
    trace:
      code:
        - internal/api/handlers_decision_model.go
        - internal/api/handlers_model_auto_mode.go
        - internal/api/routes.go
      tests:
        - internal/api/handlers_model_auto_mode_test.go#TestRouterTestEndpointDraft
        - internal/api/handlers_model_auto_mode_test.go#TestRouterEndpointsNeverEchoKey
        - internal/api/handlers_model_auto_mode_test.go#TestRouterModelsEndpoint
        - internal/api/handlers_model_auto_mode_test.go#TestRouterModelsSuggestionsAndPull
        - internal/api/handlers_decision_model_test.go#TestLegacyRouterAliasServesSamePayload
        - internal/api/handlers_decision_model_test.go#TestModelAutoModeRouterIsReadOnlyAliasOfDecisionModel
        - internal/api/handlers_decision_model_test.go#TestPutModelAutoModeRouterForwardsWithDeprecationWarning
    verified: {rev: "sha256:f5029f7df145bd15", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R8:
    status: backlog
    trace:
      code: [cmd/init.go, internal/config/init.go]
      tests: [cmd/init_template_test.go#TestInitTemplatesUseDecisionModelRouter]
    verified: {rev: "sha256:481cc6092d3f01a8", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
---

## Purpose

This spec defines the top-level `decisionModel` configuration block: the shared System One / Jev decision provider (Ollama 0.35+, TypeSafe Jev or a custom gateway) that model auto mode, persona auto-select and the context relevance filter all use. It also covers validation, secret handling, persistence, the one-time migration from the legacy `modelAutoMode.router` and the REST surface.

## Scope

In scope:
- `internal/config` (types, defaults, validation, persistence, migration)
- `GET/PUT /api/v1/config/decision-model` and `/api/v1/decision-model/router/*`
- the deprecated `/api/v1/model-auto-mode/router/*` aliases
- the `pando init` template block

Out of scope: how the engine uses the block (spec "Decision model: shared engine and consumers") and the settings UIs (spec "Decision model: settings surfaces").

Epic: PANDO-EP-0018. Stories: PANDO-US-0097, PANDO-US-0096.

## Requirements

### PANDO-SP-0005.R1 — Block shape, provider kinds, timeout and normalisation

The system SHALL expose the shared decision provider as the top-level `decisionModel` block `{router{provider, baseURL, apiKey, model, keepAlive, headers}, timeoutMs}`.

- A missing `router.provider` SHALL default to `ollama`.
- A `timeoutMs` of 0 SHALL mean 1500 ms for `ollama` and 3000 ms for any other provider; an explicit value SHALL win.
- Normalisation SHALL trim values, lower-case the provider, default `keepAlive` to `30m` for `ollama` only and turn an empty `headers` map into nil.
- A freshly loaded config SHALL carry the defaults, and a viper default SHALL NOT make the legacy `modelAutoMode.router` look present.

#### Scenario: Timeout defaults
- GIVEN an empty block
- WHEN the effective timeout is computed
- THEN it is 1500 ms for ollama and 3 s for typesafe
- AND `timeoutMs=700` yields 700 ms

#### Scenario: Normalisation
- GIVEN `provider=" Ollama "`, `model="  tev1:0.8b "` and an empty headers map
- WHEN the block is normalised
- THEN the provider is `ollama`, the model is trimmed, `keepAlive` is `30m` and headers are nil

#### Scenario: Defaults after Load
- GIVEN no config file
- WHEN the config loads
- THEN `decisionModel.router.provider=ollama`, `keepAlive=30m` and `timeoutMs=0`

### PANDO-SP-0005.R2 — Validation rules

The system SHALL validate the block as follows; errors block a save, warnings never do:

- `router.provider` SHALL be `ollama`, `typesafe` or `custom`.
- `router.baseURL` SHALL be required for `custom`, and any given value SHALL be a valid http(s) URL.
- For `ollama`, `router.model` SHALL NOT end in `:cloud`.
- `timeoutMs` SHALL NOT be negative.
- A `typesafe` provider without a key (and without `$TYPESAFE_API_KEY`) SHALL only produce a warning.
- An empty `router.model` is valid for the block itself; model auto mode, when enabled, SHALL require it (`decisionModel.router.model is required`).
- `UpdateDecisionModel` SHALL reject an invalid block and leave the config unchanged.

#### Scenario: Unknown provider
- GIVEN `provider=gemini`
- WHEN validated
- THEN an error on `router.provider` lists ollama, typesafe and custom

#### Scenario: Custom without base URL
- GIVEN `provider=custom` and no `baseURL`
- WHEN validated
- THEN an error on `router.baseURL` is returned
- AND `ftp://x` is also rejected

#### Scenario: Cloud model on Ollama
- GIVEN `provider=ollama` and `model=qwen3.5:cloud`
- WHEN validated
- THEN an error on `router.model` says a local model is required

#### Scenario: TypeSafe without key
- GIVEN `provider=typesafe`, no key and no env var
- WHEN validated
- THEN there are no errors and one warning, which `$TYPESAFE_API_KEY` silences

#### Scenario: Auto mode needs a model
- GIVEN model auto mode enabled and an empty decision model
- WHEN auto mode is validated
- THEN an error on `router.model` is returned, and none when auto mode is disabled

### PANDO-SP-0005.R3 — Router API key: encrypted at rest, masked, env-resolvable, keep and clear

The system SHALL store `decisionModel.router.apiKey` encrypted at rest, resolve `$ENV_VAR` references at runtime, fall back to `$TYPESAFE_API_KEY` for the `typesafe` provider, and mask the key in every REST response (`apiKeySet` plus a masked tail). An update with an empty key SHALL keep the stored key; clearing SHALL remove it. The plain key SHALL NOT appear in a REST response.

#### Scenario: Encrypted on disk and reloadable
- GIVEN an update that sets `apiKey=sk-test-123`
- WHEN the config file is read
- THEN the plaintext is absent and the encrypted marker is present
- AND a fresh Load yields `sk-test-123` in memory

#### Scenario: Keep and clear
- GIVEN a stored key
- WHEN an update changes only the timeout with an empty key
- THEN the stored key is unchanged
- AND `ClearDecisionModelAPIKey` empties it

#### Scenario: Masked on GET
- GIVEN a stored key ending `7890`
- WHEN `GET /api/v1/config/decision-model` is called
- THEN `apiKeySet=true`, the masked tail ends `7890`, and the plain key is absent from the body

#### Scenario: Masking and env
- GIVEN `apiKey=$MY_ROUTER_KEY` with that env var set
- WHEN the key is resolved
- THEN the env value is used
- AND `MaskAPIKey("sk-test-1234")` is `••••1234`

### PANDO-SP-0005.R4 — Persistence, hot-reload event and config locks

`UpdateDecisionModel` SHALL persist the block through the config file, apply it in memory, publish a `decisionModel` config-change event on the config bus so every consumer picks it up on the next prompt, and refuse changes to a locked `decisionModel` with a lock error while leaving the config unchanged. If persisting fails the in-memory value SHALL be reverted.

#### Scenario: Event on save
- GIVEN a subscriber on the config bus
- WHEN a valid block is saved
- THEN an event with section `decisionModel` is published

#### Scenario: Locked block
- GIVEN an enterprise lock on `decisionModel`
- WHEN `UpdateDecisionModel` is called
- THEN a lock error is returned
- AND the config is unchanged

### PANDO-SP-0005.R5 — One-time migration from modelAutoMode.router

On load, when `decisionModel.router.model` is empty and the legacy `modelAutoMode.router` has a model, the system SHALL copy the legacy router (key kept encrypted) and `timeoutMs` into `decisionModel`, rewrite the config file once, log one info line and clear the legacy fields. The legacy keys SHALL be tolerated on read without a startup error and SHALL NOT be written again. If `decisionModel` already has a model it SHALL win and the legacy values SHALL be dropped from memory only. If the file is locked, the migrated value SHALL stay in memory only, the file SHALL NOT be rewritten and a warning SHALL be logged. The migration SHALL be idempotent.

#### Scenario: Legacy file migrated
- GIVEN a config with `[ModelAutoMode.Router]` (custom, a model and a key) and `TimeoutMs=2500`
- WHEN it loads
- THEN `decisionModel` holds the router and timeout, the file now has the `DecisionModel` block with the key encrypted, and the legacy fields are cleared

#### Scenario: Idempotent
- GIVEN an already migrated file
- WHEN it loads again
- THEN decisionModel is unchanged and the file is not rewritten

#### Scenario: Both present
- GIVEN a file with a decisionModel model and a legacy router
- WHEN it loads
- THEN decisionModel wins and the legacy router is not kept in memory

#### Scenario: Locked file
- GIVEN an enterprise lock on `decisionModel`
- WHEN a legacy file loads
- THEN the router is migrated in memory only and the file is untouched

### PANDO-SP-0005.R6 — REST: GET/PUT /api/v1/config/decision-model and DELETE api-key

The API SHALL expose `GET` and `PUT /api/v1/config/decision-model` and `DELETE /api/v1/config/decision-model/api-key`.

- `GET` SHALL return the router (provider, baseURL, effective base URL, model, keepAlive, headers, `apiKeySet`, masked key), `timeoutMs` and a `warnings` array (never null), and never the plain key.
- `PUT` SHALL validate the body and answer field-level errors with status 400 naming `decisionModel.<field>`; an invalid PUT SHALL NOT change the config.
- A `PUT` with an empty key SHALL keep the stored key; `clearApiKey=true` or the DELETE endpoint SHALL remove it.
- The endpoints SHALL require the same authentication as the other `/api/v1/config/*` endpoints.

#### Scenario: Field errors
- GIVEN a PUT with `provider=custom` without `baseURL` and `timeoutMs=-5`
- WHEN the handler runs
- THEN the status is 400 and the errors list `decisionModel.router.baseURL` and `decisionModel.timeoutMs`
- AND the stored config is unchanged

#### Scenario: Keep and clear
- GIVEN a stored key
- WHEN a PUT omits the key
- THEN the key is kept
- AND a PUT with `clearApiKey=true` removes it

#### Scenario: Warnings are an array
- GIVEN a valid block
- WHEN it is read
- THEN `warnings` is an empty array rather than null

### PANDO-SP-0005.R7 — REST: decision-model router endpoints and deprecated aliases

The API SHALL expose `GET/POST /api/v1/decision-model/router/models`, `POST .../router/test`, `GET .../router/health`, `POST .../router/pull` and `GET .../router/pull/{id}`. Draft test and model-list calls SHALL work with an unsaved draft router and SHALL NEVER echo the API key.

The old `/api/v1/model-auto-mode/router/*` paths SHALL keep working for one release as aliases serving the same payload as the canonical ones. `GET /api/v1/config/model-auto-mode` SHALL return `router` only as a read-only copy of the decision model, and a `PUT` that still sends a `router` SHALL forward it to the decision model and return a deprecation warning.

#### Scenario: Draft test
- GIVEN a draft router in the request body
- WHEN `POST /decision-model/router/test` is called
- THEN the health report of the draft is returned and the key is not echoed

#### Scenario: Alias serves the same payload
- GIVEN the same draft
- WHEN the canonical and the legacy alias paths are called
- THEN both answer 200 with the same payload

#### Scenario: Read-only router in the auto mode response
- GIVEN a configured decision model
- WHEN `GET /config/model-auto-mode` is called
- THEN `router` equals the decision model router

#### Scenario: Legacy PUT forwards with a warning
- GIVEN a PUT to `/config/model-auto-mode` that includes a router
- WHEN the handler runs
- THEN the router is stored in `decisionModel` and a response warning mentions deprecation

### PANDO-SP-0005.R8 — pando init template writes the decisionModel block

The `pando init` configuration templates SHALL contain the `[DecisionModel]` and `[DecisionModel.Router]` blocks and SHALL NOT contain a `[ModelAutoMode.Router]` block.

#### Scenario: Template content
- GIVEN the init templates
- WHEN they are rendered
- THEN they define `[DecisionModel.Router]` with provider `ollama` and no `ModelAutoMode.Router`
