---
created_at: 2026-07-16T22:55:12.617574502Z
updated_at: 2026-07-22T09:28:58.017702804Z
tags:
    - fix
    - evaluator
    - config
---
# Fix: evaluator.model not persisted when Coder model changed at runtime

## Problem (2026-07-22)
`ensureEvaluatorDefaultModel()` (seeds `evaluator.model`/`evaluator.provider` from the
coder agent's model when evaluator.model is empty) was only invoked from
`applyDefaultValues()`, i.e. once at `config.Load()` (app startup). Selecting a Coder
model afterwards in TUI/WebUI/API settings goes through `UpdateAgentModel` ->
`setAgentModel(..., persist=true)`, which never called that seed function, so any
evaluator-model auto-fill only ever reflected whatever coder model existed at boot and
never updated/persisted to the `.toml` file when the user picked a new Coder model
later in the running session.

## Fix
`internal/config/config.go`: `setAgentModel` now calls `ensureEvaluatorDefaultModel()`
right after successfully persisting an agent-model change, but only when
`agentName == AgentCoder`. `ensureEvaluatorDefaultModel` itself is unchanged — it is
still a no-op whenever `cfg.Evaluator.Model` is already set, so an explicit evaluator
model choice still always wins over the coder-derived default.

## Verification
- `go build ./...` clean.
- `go test ./internal/config/...` passes.
