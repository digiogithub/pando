---
created_at: 2026-07-16T22:55:12.316339959Z
updated_at: 2026-07-16T22:55:12.316339959Z
---
# Fix: evaluator.model auto-seeded from coder agent model

## What changed
- `internal/config/config.go`: new `ensureEvaluatorDefaultModel()`, called from `applyDefaultValues()` right after `ensureAgentDefaults()`.
- Logic: if `cfg.Evaluator.Model` is empty, copy `cfg.Agents[AgentCoder].Model` (and its matching provider) into `cfg.Evaluator.Model`/`cfg.Evaluator.Provider`, then persist via `updateCfgFile` so the choice survives reload.
- If evaluator.model is already set (default-seeded previously, or user picked another model explicitly via `UpdateEvaluator`/config edit), the function is a no-op — a later user selection always wins over the coder-derived default.

## Why
User request: on startup with a fresh/template config, if no evaluator model has been chosen yet, default it to whatever model is configured for the coder agent and persist that to config, so the self-improvement judge has a usable model without manual setup. Once the user selects a different evaluator model, that choice must stick.

## Verification
- `go build ./...` clean.
- `go test ./internal/config/...` passes (no existing test broken).
- No new tests added (behavior is a startup-only default seed guarded by "already set" check; existing config test suite covers `applyDefaultValues`/`ensureAgentDefaults` wiring).
