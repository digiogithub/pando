---
created_at: 2026-09-29T22:04:52.507464155Z
updated_at: 2026-09-29T22:04:52.507464155Z
tags:
    - fix
    - evaluator
    - config
    - webui
---
# Fix: self-improvement settings not saved from WebUI/desktop (2026-09-30)

## Symptom
In `pando desktop` (WebUI settings > Self-improvement) enabling the evaluator and choosing a judge model (e.g. `copilot.gpt-6-luna`) appeared to save, but after reopening the app the section showed as unconfigured and the model was the old one.

## Root cause
The legacy fields of `config.EvaluatorConfig` (`Enabled`, `Model`, `Provider`, `AlphaWeight`, `BetaWeight`, `ExplorationC`, `MinSessionsForUCB`, `CorrectionsPatterns`, `MaxTokensBaseline`, `MaxSkills`, `JudgePromptTemplate`, `Async`) had only `toml` tags. `GET /api/v1/config/evaluator` therefore emitted `"Enabled"`, `"Model"`, ... while the WebUI (`EvaluatorSettingsConfig`, camelCase) read `enabled`/`model`, so the form always rendered defaults (disabled, empty model).
On save the store sent `{...EVALUATOR_DEFAULTS, ...serverData, ...patch}`, i.e. both `model` (user choice) and `Model` (stale server value). Go's `encoding/json` matches keys case-insensitively and the later key wins, so the stale capitalised value overwrote the user's choice and the file kept the old model. Pre-existing bug, not introduced by [[PANDO-EP-0014]]; the fields added during the epic already had json tags.

## Fix
- `internal/config/config.go`: added camelCase `json` tags matching the toml names to every `EvaluatorConfig` field.
- `internal/config/evaluator_json_test.go`: `TestEvaluatorConfigJSONKeysMatchWebUI` asserts camelCase keys, no capitalised keys, and decoding of a WebUI payload.
- Audit: only `CLIAssistConfig` (`Model`, `Timeout`) also lacks json tags; it is not exposed over the API or the WebUI, left unchanged.

## Verification
- `go build ./...`; `go test -race ./internal/config ./internal/api ./internal/evaluator ./internal/app ./cmd` pass.
- End-to-end: isolated `pando serve` on a copy of the project `.pando.toml`; GET returned camelCase, PUT with `copilot.gpt-6-luna`, restart, GET returned `enabled=true model=copilot.gpt-6-luna provider=copilot`.

Related: [[pando/changes/evaluator-observability-doctor.md]], [[pando/docs/self-improvement-system-analysis.md]].
