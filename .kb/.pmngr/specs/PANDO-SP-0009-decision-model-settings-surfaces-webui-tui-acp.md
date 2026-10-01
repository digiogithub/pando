---
id: PANDO-SP-0009
type: spec
title: "Decision model: settings surfaces (WebUI, TUI, ACP)"
status: backlog
priority: medium
author: mcp
labels: [decision-model, webui, tui, acp, PANDO-EP-0018]
created: 2026-10-01T17:06:48Z
updated: 2026-10-01T17:11:14Z
requirements:
  R1:
    status: backlog
    trace:
      code:
        - web-ui/src/components/settings/DecisionModelSettings.tsx
        - web-ui/packages/pando-client/src/stores/decisionModelStore.ts
        - web-ui/src/components/settings/SettingsView.tsx
      tests: [web-ui/src/components/settings/DecisionModelSettings.test.tsx]
    verified: {rev: "sha256:87c296b328aeaf8f", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:42Z, by: mcp}
  R2:
    status: backlog
    trace:
      code:
        - web-ui/src/components/settings/DecisionModelInUse.tsx
        - web-ui/src/components/settings/ModelAutoModeSettings.tsx
        - web-ui/src/components/settings/AgentsSettings.tsx
      tests:
        - web-ui/src/components/settings/ModelAutoModeSettings.test.tsx
        - web-ui/src/components/settings/AgentsSettings.test.tsx
  R3:
    status: backlog
    trace:
      code:
        - web-ui/src/components/settings/RemembrancesSettings.tsx
        - web-ui/packages/pando-client/src/stores/servicesSettingsStore.ts
      tests: [web-ui/src/components/settings/RemembrancesSettings.test.tsx]
    verified: {rev: "sha256:0e4634d4b1ec3810", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:42Z, by: mcp}
  R4:
    status: backlog
    trace:
      code:
        - web-ui/src/components/chat/ContextFilterNotice.tsx
        - web-ui/src/components/chat/MessageBubble.tsx
        - web-ui/packages/pando-client/src/services/sse.ts
      tests: [web-ui/src/components/chat/ContextFilterNotice.test.tsx]
    verified: {rev: "sha256:da71aa8ab71f6d2d", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:42Z, by: mcp}
  R5:
    status: backlog
    trace:
      code:
        - internal/tui/page/settings_decision_model.go
        - internal/tui/page/settings.go
        - internal/tui/page/settings_model_auto.go
      tests:
        - internal/tui/page/settings_decision_model_test.go#TestDecisionModelSectionFieldsPerProvider
        - internal/tui/page/settings_decision_model_test.go#TestDecisionModelSaveKeyHeadersAndTimeout
        - internal/tui/page/settings_decision_model_test.go#TestDecisionModelValidationAndConsumerGuard
        - internal/tui/page/settings_decision_model_test.go#TestDecisionModelDiscoveryAndPullActions
        - internal/tui/page/settings_decision_model_test.go#TestDecisionModelInfoRows
        - internal/tui/page/settings_decision_model_test.go#TestRemembrancesDecisionFilterFields
        - internal/tui/components/core/status_context_filter_test.go#TestStatusShowsContextFilterNotice
    verified: {rev: "sha256:3f33d6679d1d8c8d", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R6:
    status: backlog
    trace:
      code:
        - internal/llm/tools/pando_setup.go
        - internal/llm/modelrouter/status.go
      tests:
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelListedInHelp
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelSetShowTest
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelTestReportsUnhealthy
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelModels
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelClearKey
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelValidationAndUsageErrors
        - internal/llm/tools/pando_setup_decision_model_test.go#TestPandoSetupDecisionModelNeedsLoadedConfig
    verified: {rev: "sha256:83175943f4b164a2", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
  R7:
    status: backlog
    trace:
      code:
        - internal/mesnada/acp/decision_model_commands.go
        - internal/mesnada/acp/session_state.go
        - internal/mesnada/acp/slash_commands.go
      tests:
        - internal/mesnada/acp/decision_model_commands_test.go#TestAutoModelDescriptionReadsDecisionModel
        - internal/mesnada/acp/decision_model_commands_test.go#TestDecisionModelCommandAdvertisedAndParsed
        - internal/mesnada/acp/decision_model_commands_test.go#TestDecisionModelCommandShowsStatusWithMaskedKey
    verified: {rev: "sha256:38de6ba32965f952", commit: 954b506d46743949eea3fd700f02295e2ae8da4c, at: 2026-10-01T17:10:33Z, by: mcp}
---

## Purpose

This spec defines how users configure and inspect the shared decision model and the relevance filter from every surface: the WebUI settings page and read-only "in use" rows, the TUI section, the `pando_setup decision-model` command and the ACP session option and `/decision-model` command.

## Scope

In scope:
- `web-ui/src/components/settings/{DecisionModelSettings,DecisionModelInUse,ModelAutoModeSettings,AgentsSettings,RemembrancesSettings}.tsx` and the chat `ContextFilterNotice`
- `internal/tui/page/settings_decision_model.go`
- `internal/llm/tools/pando_setup.go` (`decision-model`)
- `internal/mesnada/acp` (`/decision-model`, Auto option description)

Out of scope: the REST contract (spec "Decision model: configuration, migration and REST").

Epic: PANDO-EP-0018. Stories: PANDO-US-0100, PANDO-US-0102, PANDO-US-0095, PANDO-US-0096.

## Requirements

### PANDO-SP-0009.R1 — WebUI: Decision model settings page

The WebUI SHALL provide a "Decision model" settings category that loads and saves the shared block through `/api/v1/config/decision-model`: provider, provider-specific fields, API key handling, extra headers, timeout, model discovery with Pull for suggested Ollama models, and Test connection.

- The stored key SHALL never be shown in plain text; the page offers keep and clear.
- A saved payload SHALL contain the router and timeout and no model-auto-mode fields.
- Field errors from a 400 SHALL be shown next to the fields.

#### Scenario: Load and save
- GIVEN a stored decision model
- WHEN the page opens and the user saves
- THEN the page called the decision-model endpoint and sent the router and `timeoutMs` only

#### Scenario: Key never shown
- GIVEN a stored key
- WHEN the page renders
- THEN only the masked tail is shown, with keep and clear actions

#### Scenario: Server validation
- GIVEN a PUT that returns field errors
- WHEN the save fails
- THEN the messages appear next to the offending fields

### PANDO-SP-0009.R2 — WebUI: read-only decision model rows and warnings

The WebUI SHALL show, in Model auto mode settings and in the persona-selector agent settings, a read-only "decision model in use" row that links to the Decision model page. Model auto mode settings SHALL have no provider fields and SHALL save without `router` or `timeoutMs`. Each of these views SHALL warn with a link when its option is on and no decision model is configured, and the persona-selector view SHALL show the privacy note only for a hosted router.

#### Scenario: Auto mode has no provider fields
- GIVEN a configured decision model
- WHEN Model auto mode settings render
- THEN the decision model in use and a Configure link are shown and no provider inputs exist

#### Scenario: No model configured
- GIVEN auto mode enabled and no decision model
- WHEN the view renders
- THEN a warning with a link to the Decision model page is shown

#### Scenario: Persona selector privacy note
- GIVEN a hosted router
- WHEN the persona-selector settings render
- THEN the privacy note is shown, and it is absent for a local router

### PANDO-SP-0009.R3 — WebUI: filter controls in Remembrances settings

The WebUI Remembrances settings SHALL show the decision model in use and the filter controls with their defaults: enable toggles for the context filter and the memory filter, threshold, max candidates, max characters per candidate and "allow hosted decision providers". It SHALL warn with a link when a filter is on and no decision model is configured, show a snippet privacy hint for a hosted provider, flag an invalid threshold locally and show a server validation error, and persist the values through the services save path.

#### Scenario: Controls and defaults
- GIVEN default settings
- WHEN the page opens
- THEN the toggles are off and the threshold shows 0.6

#### Scenario: Missing model warning
- GIVEN a filter is on and no decision model is set
- WHEN the page renders
- THEN a warning links to the Decision model page

#### Scenario: Hosted privacy hint
- GIVEN a hosted decision provider
- WHEN the page renders
- THEN the snippet privacy hint is shown

#### Scenario: Invalid threshold
- GIVEN a threshold of 0 or above 1
- WHEN the user edits it
- THEN the field is flagged and a server 400 is shown as an error

### PANDO-SP-0009.R4 — WebUI: context filter notice in the chat

The WebUI chat SHALL render the context filter notice from the SSE `context_filter` payload as a compact line with expandable per-source counts, highlight and explain a partial result, omit router fields that are absent, hide the notice in simple chat mode unless something was dropped, and render a fail-open warning as a plain system notice.

#### Scenario: Compact line and expansion
- GIVEN a notice payload with per-source counts
- WHEN the message renders
- THEN the compact line is shown and expanding it reveals the per-source counts

#### Scenario: Partial result
- GIVEN a payload with `reason=partial:<class>`
- WHEN it renders
- THEN it is highlighted and explained

#### Scenario: Simple chat mode
- GIVEN simple chat mode and a payload that dropped nothing
- WHEN it renders
- THEN nothing is shown

#### Scenario: Through MessageBubble
- GIVEN a streamed `context_filter` event
- WHEN the message is rendered through MessageBubble
- THEN the payload is kept on the message and displayed

### PANDO-SP-0009.R5 — TUI: Decision model section and read-only rows

The TUI settings SHALL provide a Decision model section with provider-dependent fields (provider, base URL, API key, model, keep-alive, headers, timeout), save the key, headers and timeout, validate input, and offer model discovery, pull, test and clear key actions. The Auto mode, persona-selector and Remembrances sections SHALL show the read-only decision model row, and the Remembrances section SHALL expose the relevance filter fields. The context filter summary SHALL reach the status bar as an info message.

#### Scenario: Fields per provider
- GIVEN each provider kind
- WHEN the section is built
- THEN only the fields that apply to that provider are present

#### Scenario: Validation and consumer guard
- GIVEN a negative timeout
- WHEN it is saved
- THEN the save is rejected and nothing is stored
- AND WHEN a feature that uses the decision model is enabled, emptying the model or switching provider (which clears it) is refused

#### Scenario: Remembrances filter fields
- GIVEN the Remembrances section
- WHEN it is built
- THEN the filter toggles, threshold and limits are present

#### Scenario: Status notice
- GIVEN a context filter notice
- WHEN it is shown
- THEN it appears in the status bar

### PANDO-SP-0009.R6 — pando_setup decision-model subcommand

The `pando_setup` tool SHALL offer `decision-model` with `show` (default), `set`, `test`, `models` and `clear-key`, listed in its help. `set` SHALL validate and persist provider, base URL, API key, model and timeout; `show` SHALL mask the key and list the consumers; `test` SHALL report a live health verdict, including an unhealthy one; usage and validation errors SHALL be reported without changing the config, and the command SHALL fail clearly when no config is loaded.

#### Scenario: Set, show, test
- GIVEN a loaded config
- WHEN `decision-model set --provider ollama --model tev1:0.8b` then `show` and `test` run
- THEN the block is saved, `show` prints it with a masked key and `test` prints the health verdict

#### Scenario: Models and clear-key
- GIVEN a stored key
- WHEN `models` and `clear-key` run
- THEN the provider's models are listed and the key is removed

#### Scenario: Invalid input
- GIVEN `set --provider gemini`
- WHEN it runs
- THEN an error is returned and the config is unchanged

### PANDO-SP-0009.R7 — ACP: Auto option description and /decision-model command

The ACP session option description of the Auto model SHALL end with the decision model in use (for example `router: ollama/tev1:0.8b`), read from `decisionModel.router`. ACP SHALL advertise a `/decision-model [test]` local control command in `available_commands_update` that answers without a model turn with the provider, effective URL, masked API key (never the plain key), model, timeout, enabled consumers and, with `test`, a live health verdict.

#### Scenario: Auto description
- GIVEN a decision model `ollama/tev1:0.8b`
- WHEN the session options are built
- THEN the Auto description names that router

#### Scenario: Command advertised and parsed
- GIVEN an ACP session
- WHEN available commands are listed and `/decision-model test` is sent
- THEN the command is advertised and parsed with its argument

#### Scenario: Status without the key
- GIVEN a stored key
- WHEN `/decision-model` runs
- THEN the answer shows a masked key and never the plain one
