---
id: PANDO-US-0102
type: story
title: "TUI: \"Decision model\" settings section (provider, URL, key, model discovery/pull/test, health); info rows and filter toggles in Model auto mode, persona-selector and Remembrances; status notice"
status: in_review
priority: medium
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, tui, settings]
estimate: 5
created: 2026-10-01T16:20:29Z
updated: 2026-10-01T16:56:14Z
started: 2026-10-01T16:56:14Z
---

## Description

As a TUI user, I want the same decision model settings and pointers the WebUI has.

- `internal/tui/page/settings_decision_model.go` (new): `buildDecisionModelSection(cfg)` registered in `buildSections` under group "AI" before model auto mode. Fields: provider (select with presets), base URL, API key (masked, action `clear`), headers (custom), keep-alive (ollama), model (select fed by `discoverModelAutoModels` → renamed `discoverDecisionModels`, suggestions and `action:decision_model_pull:<model>`), timeoutMs, info rows for effective URL and health, action "Test connection" (moved `testModelAutoConnection`). `saveDecisionModel(field)` calls `config.UpdateDecisionModel`.
- `settings_model_auto.go`: remove provider fields; add info rows `Decision model: <provider>/<model>` and `Health: …` plus a hint "configure in Decision model section"; keep routes/threshold/playground.
- `settings.go:1694-1721` (persona-selector agent): reuse the same two info rows via a helper `decisionModelInfoRows(cfg)`; `settings_persona_health.go` takes `DecisionModelConfig`.
- `buildRemembrancesSection` (`settings.go:2389`): new fields under context enrichment — filter enabled, memory filter enabled, threshold, max candidates, local-only — plus the info rows; saved through the remembrances save path.
- Status/chat: the `Context filter: kept n/m` status message shows in the chat status line like routing notices (`internal/tui/components/core/status.go`).
- Hot reload: `settings.go:183` handler pattern for the `decisionModel` section; `ConfigChangeEvent` with `Section:"decisionModel"` rebuilds the three sections.

## Acceptance Criteria

- [ ] Section visible, navigable, fields save and reload; invalid values show the `FieldError` message inline.
- [ ] Discovery, pull with progress, clear key and test connection work from the new section (`settings_decision_model_test.go`, adapted `settings_model_auto_test.go`, `TestPersonaSelectorDecisionModelFields`).
- [ ] Model auto mode and persona-selector sections contain no editable router fields; info rows reflect config changes without restart.
- [ ] Remembrances filter fields persist and validate.
- [ ] Status notice test in `internal/tui/components/core` (`status_auto_test.go` sibling).
- [ ] `go test ./internal/tui/...` green.

## Notes

- Anchors: `settings_model_auto.go:105-470`, `settings.go:183, 326, 590-601, 989-1003, 1606-1721, 2389, 3912, 4266`, `settings_persona_health.go`.
- Keep key names `decisionModel.router.*` in `settings.Field.Key` so `saveField` routing stays a prefix match.
- Spec: "Decision model: settings surfaces (WebUI/TUI/ACP)".
