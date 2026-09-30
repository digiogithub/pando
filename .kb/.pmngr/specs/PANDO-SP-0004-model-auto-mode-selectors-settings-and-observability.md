---
id: PANDO-SP-0004
type: spec
title: "Model auto mode: selectors, settings and observability"
status: in_review
priority: medium
author: mcp
labels: [model-routing, webui, tui, acp, PANDO-EP-0015]
created: 2026-09-30T20:01:14Z
updated: 2026-09-30T21:06:56Z
started: 2026-09-30T21:06:56Z
requirements:
  R1:
    status: in_review
    trace:
      code:
        - internal/api/handlers_models.go
        - internal/api/handlers_chat.go
        - internal/mesnada/acp/session_state.go
        - internal/mesnada/acp/agent.go
        - internal/tui/components/dialog/models.go
        - internal/tui/tui.go
        - internal/llm/agent/setup_bridge_model.go
        - web-ui/src/components/overlays/ModelSwitcher.tsx
      tests:
        - internal/api/handlers_models_test.go#TestListModelsAutoFirst
        - internal/api/handlers_models_test.go#TestSetActiveModelLeavesAuto
        - internal/mesnada/acp/session_state_test.go#TestAutoModelOptionFirstAndDefault
        - internal/mesnada/acp/agent_test.go#TestResumeKeepsAuto
        - internal/mesnada/acp/session_model_sync_test.go#TestReconcileACPSessionModelAdoptsAgentSideAuto
        - internal/tui/components/dialog/models_test.go#TestModelsDialogAutoFirst
        - internal/tui/components/dialog/models_test.go#TestModelsDialogAutoHiddenWhenDisabled
        - internal/llm/agent/setup_bridge_model_test.go#TestSetupBridgeCurrentModelReportsAutoSession
        - web-ui/src/components/overlays/ModelSwitcher.test.tsx
        - web-ui/e2e/model-auto-mode.spec.ts
  R2:
    status: in_review
    trace:
      code:
        - web-ui/src/components/settings/ModelAutoModeSettings.tsx
        - web-ui/packages/pando-client/src/stores/modelAutoModeStore.ts
        - internal/tui/page/settings_model_auto.go#buildModelAutoModeSection
        - internal/api/handlers_model_auto_mode.go
      tests:
        - web-ui/e2e/model-auto-mode.spec.ts
        - web-ui/src/components/settings/ModelAutoModeSettings.test.tsx
        - internal/tui/page/settings_model_auto_test.go#TestModelAutoModeSection
        - internal/tui/page/settings_model_auto_test.go#TestModelAutoModeSectionParity
        - internal/tui/page/settings_model_auto_test.go#TestModelAutoModePullAction
        - internal/tui/page/settings_model_auto_test.go#TestModelAutoModeSectionRouteCap
        - internal/api/handlers_model_auto_mode_test.go#TestRouterModelsSuggestionsAndPull
  R3:
    status: in_review
    trace:
      code: [internal/api/handlers_model_auto_mode.go]
      tests:
        - internal/api/handlers_model_auto_mode_test.go#TestPlaygroundDraft
        - internal/api/handlers_model_auto_mode_test.go#TestPlaygroundNoLLM
        - tests/model_auto_mode/test_playground_live.py
  R4:
    status: in_review
    trace:
      code:
        - internal/llm/agent/model_auto.go
        - internal/api/handlers_chat.go#dispatchSSEEvent
        - internal/agui/translate.go
        - internal/mesnada/acp/prompt_handler.go
        - internal/tui/tui.go
        - web-ui/src/components/chat/MessageBubble.tsx
      tests:
        - internal/api/handlers_chat_test.go#TestSSEForwardsSystemMessage
        - internal/llm/agent/model_auto_test.go#TestAutoNoticeFormats
        - internal/mesnada/acp/prompt_handler_test.go#TestReplayIncludesRoutingNotice
        - internal/agui/translate_test.go#TestRoutingNoticeCustomEvent
        - internal/tui/components/core/status_auto_test.go#TestStatusAutoLabel
        - web-ui/src/components/chat/RoutingNotice.test.tsx
  R5:
    status: in_review
    trace:
      code:
        - internal/extevents/extevents.go
        - internal/llm/agent/model_auto.go
        - cmd/doctor.go
      tests:
        - internal/extevents/extevents_test.go#TestModelRoutedPayload
        - internal/llm/agent/model_auto_test.go#TestAutoTelemetryPrivacy
        - cmd/doctor_model_auto_test.go#TestDoctorModelAutoMode
  R6:
    status: in_review
    trace:
      code:
        - internal/llm/systemone/systemonetest/server.go
        - tests/model_auto_mode/bench_router.py
        - tests/model_auto_mode/live_providers.py
      tests:
        - internal/llm/systemone/systemonetest/server_test.go#TestFakeServerFixtures
        - tests/model_auto_mode/bench_router.py
        - tests/model_auto_mode/live_providers.py
---

## Purpose

This spec covers the user-facing surfaces of auto mode:

- the "Auto" entry in every model selector
- the settings UI for the decision provider, the router model and the routes, plus the router playground
- the per-turn routing notice, events, telemetry and doctor

## Scope

In scope:
- WebUI: `ModelSwitcher`, settings category, chat SSE
- TUI: model dialog, settings section, status bar
- ACP session config options
- `pando_setup`
- AG-UI
- `extevents`
- telemetry
- `pando doctor`

Epic: PANDO-EP-0015. Stories: PANDO-US-0082, PANDO-US-0083, PANDO-US-0084, PANDO-US-0085.

## Requirements

### PANDO-SP-0004.R1 — "Auto" is the first and default entry in every model selector

When `modelAutoMode.enabled` is true, the model listings SHALL expose the pseudo model `auto` as their first entry. This applies to:

- `GET /api/v1/models`, including `routerHealthy` and the router provider and model
- the WebUI ModelSwitcher
- the TUI model dialog
- ACP session config options and session model state
- `pando_setup` model switching

New sessions SHALL start in Auto when `defaultAuto` is true.

- **Selecting `auto`** SHALL set the Auto flag for its scope: global for WebUI and TUI, per session for ACP, where it is persisted in `acp_session_state`.
- **Selecting a concrete model** SHALL clear the Auto flag and behave exactly as the selector does today.
- `auto` SHALL never be registered in `models.SupportedModels()` and SHALL never reach a provider.
- **When auto mode is disabled,** the `auto` entry SHALL disappear, and sessions that were in Auto SHALL behave as if the coder model had been selected.

#### Scenario: API listing
- GIVEN auto mode is enabled
- WHEN `GET /api/v1/models` is called
- THEN the first item has `id=auto`

#### Scenario: ACP new session default
- GIVEN auto mode is enabled with `defaultAuto=true`
- WHEN an ACP client calls `session/new`
- THEN the model config option's current value is `auto`
- AND `auto` is the first option

#### Scenario: ACP resume keeps Auto
- GIVEN an ACP session set to `auto`
- WHEN it is resumed with `session/resume`
- THEN its model is still `auto`

#### Scenario: Leave Auto
- GIVEN the WebUI is in Auto
- WHEN `PUT /api/v1/models/active` is called with a concrete model X
- THEN the Auto flag is cleared
- AND `agents.coder.model == X`

#### Scenario: Disabled hides entry
- GIVEN auto mode is disabled
- WHEN models are listed in any surface
- THEN no entry has `id=auto`

#### Scenario: TUI dialog order
- GIVEN auto mode is enabled
- WHEN the TUI models dialog opens
- THEN the first row is "Auto"

### PANDO-SP-0004.R2 — Settings UI: provider, base URL, API key, decision-model selector, routes and test connection

The WebUI settings SHALL provide a "Model auto mode" category, and the TUI settings SHALL provide an equivalent section. Both SHALL include:

- **Enable toggles.**
- **A decision provider selector** with the options Ollama, TypeSafe Jev and Custom:
  - For Ollama, the base URL SHALL be read-only and resolved from the Ollama provider.
  - For TypeSafe, the base URL SHALL be prefilled.
  - For Custom, the base URL SHALL be required, and presets SHALL be offered for OpenRouter, LiteLLM and Kev.
- **A masked API key input.**
- **A router model selector fed by the discovery endpoint.** It SHALL show only decision models when the list is filtered, offer "show all" when the list is unfiltered, and accept free text when listing is unsupported.
- **A Pull action** for suggested Ollama decision models that are not installed.
- **A "Test connection" action** that shows the health report.
- **A privacy notice** for remote providers.
- **A route editor** with reordering, id, description, primary model, fallback 1 and fallback 2, and an enable toggle, capped at 25 routes.
- **Inline validation errors.**

#### Scenario: Ollama selector lists only decision models (Playwright)
- GIVEN the fake decision server serves an Ollama `/api/tags` with `tev1:0.8b` and `qwen2.5-coder:0.5b`
- WHEN the user opens Settings → Model auto mode with provider Ollama
- THEN the router model options are exactly `tev1:0.8b`

#### Scenario: Custom provider flow (Playwright)
- GIVEN the user selects Custom, applies the OpenRouter preset and enters an API key
- WHEN they click "Test connection" against the fake server
- THEN the report shows reachable, authorized and the latency
- AND the privacy notice names the host

#### Scenario: Key masked after save
- GIVEN the user saves with API key `sk-live-9876`
- WHEN the page reloads
- THEN the key field shows "key set ••••9876"
- AND the page DOM does not contain `sk-live-9876`

#### Scenario: Provider switch resets model
- GIVEN the router model `tev1:0.8b` is selected on Ollama
- WHEN the provider is switched to TypeSafe
- THEN the model selection is cleared
- AND the list reloads from `/v1/models`

#### Scenario: Route cap
- GIVEN 25 routes exist
- WHEN the user tries to add another
- THEN the add action is disabled and an explanation is shown

### PANDO-SP-0004.R3 — Router playground on unsaved drafts

`POST /api/v1/model-auto-mode/test` SHALL take a sample prompt and a draft `modelAutoMode` block. It SHALL run only the router, never an LLM.

It SHALL return:
- the decision: `routeId`, `reason`, probability, confidence and the per-route probabilities including `none`
- the latency
- the router cost, when the provider reports one
- the candidate chain after filtering

The settings UIs SHALL render this result as probability bars.

#### Scenario: Draft routes are tested before saving
- GIVEN saved config has no routes, and the draft has the routes `implementation` and `planning`
- WHEN the playground routes "add a struct with unit tests"
- THEN the response has `routeId=implementation`
- AND the saved config is unchanged

#### Scenario: No LLM call
- GIVEN fake LLM providers that record calls
- WHEN the playground runs
- THEN no LLM provider receives a request

#### Scenario: Live playground against tev1:0.8b (opt-in)
- GIVEN `PANDO_LIVE_OLLAMA=1` and the 4 starter routes
- WHEN "rename all usages of OverrideAgentModel" is routed
- THEN `routeId=implementation` with p ≥ 0.6

### PANDO-SP-0004.R4 — Per-turn routing notice in every client

For every Auto turn, the agent SHALL emit one status notice through `emitStatus`/`addRunStatusMessage`. The notice SHALL use one of these formats:

| Case | Format |
|---|---|
| Matched | `Auto: <route> → <model> (p=<0.00>, <n> ms via <provider>/<router model>)` |
| No match | `Auto: no confident match (best <route> p=<0.00>) → <coder model>` |
| Router unavailable | `Auto: router unavailable (<error class>) → <coder model>` |
| Failover | `Auto: <model A> failed (<error class>), retrying on <model B>` |

The WebUI chat SSE stream SHALL forward system messages, so this notice SHALL be rendered in the WebUI, the TUI, ACP clients and AG-UI (`pando.system_message`).

#### Scenario: WebUI SSE forwarding
- GIVEN an Auto turn that matched a route
- WHEN the WebUI chat SSE stream is read
- THEN one event of type system_message contains "Auto: implementation →"

#### Scenario: ACP replay
- GIVEN an ACP session with a routed turn
- WHEN the session is loaded and replayed
- THEN the routing notice appears in the replayed updates

#### Scenario: AG-UI
- GIVEN an AG-UI run in Auto
- WHEN the run streams
- THEN a custom event `pando.system_message` carries the routing notice

### PANDO-SP-0004.R5 — ModelRouted event, telemetry and doctor without leaking prompts or keys

For each decision, the system SHALL publish an extension event `ModelRouted{SessionID, RouteID, Model, Probability, Confidence, Reason, LatencyMs, RouterProvider, RouterModel, RouterCostUSD}` and SHALL write one `slog` Info line prefixed `model_auto:`.

Opt-in telemetry SHALL send only counters (routed, no-match, router-unavailable by error class, failover by provider) and the cumulative router cost. It SHALL NOT send prompt text or API keys.

`pando doctor` SHALL report the following for auto mode:
- config validity
- the provider kind
- reachability and authorization
- the Ollama version gate
- router model presence and decision capability
- a warm-up latency sample
- routes that reference unknown or disabled models

#### Scenario: Event payload
- GIVEN a matched turn routed through a gateway that reports `usage.cost`
- WHEN the `ModelRouted` event is captured
- THEN all fields are populated
- AND `RouterCostUSD` is greater than 0

#### Scenario: Telemetry privacy
- GIVEN telemetry is enabled and a prompt contains the marker "SECRET-PROMPT-MARKER"
- WHEN a turn is routed
- THEN no telemetry payload contains the marker or the API key

#### Scenario: Doctor on old Ollama
- GIVEN a fake Ollama that reports version 0.32.14
- WHEN `pando doctor` runs
- THEN the auto mode section fails with "Upgrade Ollama to ≥ 0.35"

#### Scenario: Doctor on unknown route model
- GIVEN a route whose fallback is an unknown model
- WHEN `pando doctor` runs
- THEN the section warns and names that route and model

### PANDO-SP-0004.R6 — Labelled routing benchmark and live provider validation

The repository SHALL include opt-in, live validation scripts under `tests/model_auto_mode/`:

- `bench_router.py`: runs about 30 labelled developer prompts (English and Spanish) across the 4 starter routes plus `none`.
- `live_providers.py`: runs the same set against TypeSafe or a custom gateway when credentials are present.

Both scripts SHALL report accuracy, no-match rate, p50/p95 latency and, for remote providers, cost. They SHALL exit non-zero when accuracy falls below a configured floor.

The shared fake decision server `internal/llm/systemone/systemonetest` SHALL serve the recorded Ollama 0.35 fixtures and the Jev `/v1/models` shapes. It SHALL be used by the Go tests and by the WebUI Playwright run.

#### Scenario: Ollama benchmark floor
- GIVEN Ollama ≥ 0.35 with `tev1:0.8b` and `PANDO_LIVE_OLLAMA=1`
- WHEN `bench_router.py --model tev1:0.8b --min-accuracy 0.8` runs
- THEN it exits 0
- AND it prints the accuracy and p50/p95 latency

#### Scenario: Skips without credentials
- GIVEN no `TYPESAFE_API_KEY` and no `PANDO_LIVE_JEV_BASEURL`
- WHEN `live_providers.py` runs
- THEN it exits 0 with "skipped: no credentials"

#### Scenario: Fake server fixtures
- GIVEN `systemonetest.NewOllama035()`
- WHEN `/api/tags`, `/api/version`, `/api/show` and `/v1/systemone` are requested
- THEN the responses match the recorded 0.35.0 fixtures
