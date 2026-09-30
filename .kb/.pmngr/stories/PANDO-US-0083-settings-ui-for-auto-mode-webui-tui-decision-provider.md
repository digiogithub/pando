---
id: PANDO-US-0083
type: story
title: "Settings UI for auto mode (WebUI + TUI): decision provider (Ollama/TypeSafe/custom) with base URL + API key, decision-model selector, route editor with 2 fallbacks, threshold and router playground"
status: done
priority: medium
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, webui, tui, config]
estimate: 8
created: 2026-09-30T19:34:53Z
updated: 2026-10-01T07:57:13Z
started: 2026-09-30T21:00:23Z
closed: 2026-10-01T07:57:13Z
---

## Description

As a user, I want to configure auto mode from settings: choose the decision provider and router model, edit my task routes, and test the routes against sample prompts before relying on them.

**WebUI**
- Add a "Model auto mode" category to `SettingsView.tsx:91-111`, in the AI group next to Agents and Persona.
- Add a store to `settingsStore.ts`, following `useTokenOptimizationStore` (:695). It uses `GET/PUT /api/v1/config/model-auto-mode`.
- **General fields:**
  - enabled toggle
  - "new sessions start in Auto" toggle
  - threshold slider
  - timeout
  - history prompts
- **Decision provider card:**
  - A provider segmented control with three options:
    - **Ollama (local)**, the default
    - **TypeSafe Jev**
    - **Custom (Jev-compatible)**
  - **Ollama:**
    - Shows the resolved Ollama base URL, read-only, with a link to the Ollama provider settings.
    - Offers a keep-alive field.
  - **TypeSafe:**
    - The base URL is prefilled with `https://api.typesafe.ai` and can be edited.
    - An API key field (password type) shows "key set ••••1234" when a key is stored, and hints that `$TYPESAFE_API_KEY` is used when empty.
  - **Custom:**
    - Requires a base URL and accepts an optional API key and optional extra headers.
    - Offers presets that only prefill the URL: OpenRouter `https://openrouter.ai/api`, LiteLLM `https://<proxy>/typesafe`, Kev `http://localhost:8009`.
  - **Router model selector:**
    - Populated from `GET /api/v1/model-auto-mode/router/models`.
    - For Ollama it lists only models with the `decision` capability.
    - When the list is `unfiltered`, it has a "show all models" toggle.
    - When listing is `unsupported`, it becomes a free-text input.
    - For Ollama, if a suggested model (`tev1:0.8b`, `tev1`, `nimble`) is not installed, a "Pull" action appears. It reuses `internal/ollamasetup` and shows progress.
  - **"Test connection" button:**
    - Calls `POST /api/v1/model-auto-mode/router/test` with the draft settings.
    - Shows reachable, authorized, version (Ollama ≥ 0.35), model present, is a decision model, and latency.
    - Shows problems together with the fix hint.
  - **Privacy notice:** when TypeSafe or Custom is selected, show "Prompts are sent to <host> for routing" and, when known, the cost per 1M input tokens.
- **Route editor:**
  - A list that can be reordered. Order matters because ties pick the first route.
  - Each row has an ID (auto-slugged), a description textarea, a primary model (`ModelCombobox`), fallback 1 and fallback 2 (optional `ModelCombobox`), and an enable toggle.
  - The list is capped at 25 rows.
  - Validation errors show inline, including a warning when the descriptions exceed the router's context budget.
- **Playground:**
  - A text area and a "Route" button.
  - The button calls `POST /api/v1/model-auto-mode/test` with the unsaved draft.
  - Shows the chosen route, probability bars for each route including `none`, confidence, latency, cost when reported, and the candidate chain after filtering.

**TUI**
- Add `buildModelAutoModeSection` to `internal/tui/page/settings.go`, next to `buildPersonaAutoSelectSection` (:3239).
- It covers:
  - the toggles
  - the provider kind selector
  - base URL and masked API key inputs
  - a router model picker that uses the same discovery endpoint logic through the Go service
  - threshold
  - a route list with add, edit and delete, where the model pickers reuse the models dialog
  - a "test connection" action and a "test prompt" field

## Acceptance Criteria

- [ ] Saving round-trips through the REST API and hot-reloads without a restart.
  - The API key is never shown back in plain text.
  - An empty key field keeps the stored key.
  - Locked fields (enterprise config locks) render read-only.
- [ ] Switching the provider kind resets the model selection and reloads the model list.
- [ ] Neither the playground nor the test endpoint runs an LLM. Both run only the router within its timeout.
- [ ] The UI uses the design-system primitives, tokens and themes (EP-0010).
  - It works at mobile width, using the master-detail pattern.
  - It uses `useDialogs()` instead of `window.confirm` (Wails).
- [ ] Strings are added to the WebUI i18n catalog.
- [ ] Typecheck and build pass. TUI settings tests are updated. A Playwright check covers the Ollama flow with the fake decision server.

## Notes

- **Starter routes:** ship 3–5 suggested routes as a one-click template, never applied automatically. The benchmark showed that concrete wording is essential; for example, "translate" and "commit message" need to be named explicitly.
- **Spec:** "Model auto mode: selectors, settings and observability".
