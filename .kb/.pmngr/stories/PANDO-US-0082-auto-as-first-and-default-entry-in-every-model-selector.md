---
id: PANDO-US-0082
type: story
title: "\"Auto\" as first and default entry in every model selector: WebUI ModelSwitcher, TUI model dialog, ACP session options, pando_setup"
status: in_review
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, webui, tui, acp, api]
estimate: 5
created: 2026-09-30T19:34:53Z
updated: 2026-09-30T21:00:23Z
started: 2026-09-30T21:00:23Z
---

## Description

As a user, when auto mode is enabled, I want "Auto" to be the first option in the model selector and selected by default. Routing then stays on unless I pick a concrete model.

**Selection semantics:**
- **Pseudo model ID:** `auto`. It is never registered in `models.SupportedModels()` and never reaches a provider.
- **Choosing Auto:** sets the Auto flag for the current scope. The scopes are:
  - WebUI and TUI: the global active selection, plus the per-session flag
  - ACP: per session
- **Choosing a concrete model:** clears the Auto flag for that scope. After that it behaves exactly as today; in WebUI and TUI this means `CoderAgent.Update` and `agents.coder.model`.
- **Where the Auto state lives:** in the Auto flag only, never in `agents.coder.model`. The coder model remains the fallback when no route matches.
- **When auto mode is disabled in config:** the entry disappears. Sessions that were in Auto behave as if the coder model had been selected.

**Surfaces:**
- **Backend:**
  - `GET /api/v1/models` adds an `auto` entry first:
    `{id:"auto", name:"Auto", description:"Routes each prompt with <provider>/<router model>", routerProvider, routerModel, routerHealthy:bool, routerProblems:[…]}`.
  - `PUT /api/v1/models/active` accepts `auto` (`handlers_models.go:382`).
  - `ChatRequest.Model` (`handlers_chat.go:36`) accepts `auto` per request.
- **WebUI:**
  - `ModelSwitcher.tsx` shows Auto first, with a distinct icon and a health dot.
  - The chat input, status bar and main layout show `Auto · <last routed model>`.
- **TUI:**
  - `dialog/models.go` shows Auto first (`setupModels` :554).
  - `tui.go:801` handles `ModelSelectedMsg{auto}`.
  - The status bar (`core/status.go:296`) shows `Auto → <model>`.
- **ACP:**
  - `buildSessionConfigOptions` and `buildSessionModelState` (`acp/session_state.go:120/174`) put `auto` first in the model select.
  - `validateModel` (`acp/agent.go:930`) and `setSessionModel` accept `auto` and persist it in `acp_session_state`.
- **`pando_setup` model switch** (`setup_bridge_model.go:57`): accepts `auto` and reports the routed model in its status.

## Acceptance Criteria

- [ ] When auto mode is enabled, Auto is listed first and preselected for new sessions in:
  - WebUI
  - desktop (same WebUI)
  - TUI
  - ACP clients (Zed, Xcode)

  Tests cover ACP `session/new` and `session/resume`.
- [ ] Switching between Auto and a concrete model works in both directions without a restart. The status bar reflects the current mode.
- [ ] When the router is unhealthy, Auto stays selectable but shows a warning. The hint depends on the provider, and ends with "prompts use <coder model>":
  - Ollama: "Ollama ≥ 0.35 with a decision model (e.g. tev1:0.8b) required"
  - TypeSafe/custom: "decision provider unreachable or unauthorized"
- [ ] AG-UI and the SDK clients accept `auto` as a model wherever they already pass a model.
- [ ] The WebUI changes pass typecheck and build. A Playwright check runs the selector against the shared fake decision server. Per the WebUI e2e memory, it uses `pando app`, not `serve`.

## Notes

- The WebUI "active model" is global today (`setCoderModel`). For parity, keep the Auto flag global in WebUI and TUI, next to the coder model. Per-session Auto is carried by the session override, so ACP sessions stay independent.
- Spec: PANDO-SP-0004.R1.
