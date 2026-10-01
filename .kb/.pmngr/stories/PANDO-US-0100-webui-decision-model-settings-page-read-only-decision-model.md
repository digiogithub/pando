---
id: PANDO-US-0100
type: story
title: "WebUI: \"Decision model\" settings page; read-only \"decision model in use\" row in Model auto mode, persona-selector and Remembrances; filter toggles; context-filter chat notice"
status: in_review
priority: medium
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, webui, settings, i18n]
estimate: 8
created: 2026-10-01T16:19:43Z
updated: 2026-10-01T17:01:06Z
started: 2026-10-01T17:01:06Z
---

## Description

As a WebUI user, I want one settings page for the decision model and clear pointers from every feature that depends on it.

- `web-ui/src/components/settings/DecisionModelSettings.tsx` (new, category `decision-model`, group AI, icon e.g. `BrainCircuit`): move the "Decision provider" section of `ModelAutoModeSettings.tsx` (lines 267-511: provider select with presets, base URL, API key with mask/clear, headers editor, keep-alive, model select/discovery/suggestions/pull job polling, health badge, "Test connection", privacy note) and add `timeoutMs`. Store `decisionModelStore` against `GET/PUT /api/v1/config/decision-model` and `/api/v1/decision-model/router/*`.
- Shared component `DecisionModelInUse` (read-only row: provider/model · health badge · "Configure →" link to the page · warning when the consumer is on and no model is configured · privacy hint when hosted). Used by:
  - `ModelAutoModeSettings.tsx` (replaces the removed section; the Playground stays here since it tests routes).
  - `AgentsSettings.tsx` for `persona-selector` (replaces the current router line; toggle copy becomes "Use decision model").
  - `RemembrancesSettings.tsx` in the context enrichment section, together with the new switches `Filter retrieved context with the decision model`, `Filter injected memories`, threshold slider, max candidates, local-only.
- Chat: render the `Context filter: kept n/m` system message like `RoutingNotice` (new `ContextFilterNotice.tsx` or a variant), collapsible, with the per-source counts.
- i18n keys in `en, es, de, fr, pt, ja, zh`; `settings.categories.decisionModel`.

## Acceptance Criteria

- [ ] Settings navigation shows "Decision model" under AI; deep link `?section=decision-model` works; mobile master-detail unaffected.
- [ ] Model auto mode page no longer contains provider fields; saving it does not send `router`.
- [ ] `DecisionModelInUse` appears on the three pages with live health (polls `/router/health` with the existing cadence) and a working link.
- [ ] Persona-selector toggle on + empty router model → inline warning with link.
- [ ] Remembrances filter toggles persist through the existing remembrances save path and show the threshold validation errors.
- [ ] Chat notice renders for the SSE system message and is hidden when `chatMode` is simple unless `Dropped > 0` (same policy as routing notices).
- [ ] Tests: `DecisionModelSettings.test.tsx`, updated `ModelAutoModeSettings.test.tsx`, `AgentsSettings.test.tsx`, `RemembrancesSettings.test.tsx`, `ContextFilterNotice.test.tsx`; `npm run lint && npm test` green; Playwright E2E smoke for the new page (`reference_webui_e2e_playwright`).

## Notes

- Anchors: `SettingsView.tsx:80-136` (categories map), `ModelAutoModeSettings.tsx`, `AgentsSettings.tsx`, `RemembrancesSettings.tsx`, `RoutingNotice.tsx`, `utils/modelLabel.ts`.
- Use `ui` primitives (`SettingsSection`, `SettingsRow`, `Badge`, `Switch`) and `useDialogs()` for confirmations (no `window.confirm`).
- Spec: "Decision model: settings surfaces (WebUI/TUI/ACP)".
