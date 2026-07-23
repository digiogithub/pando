---
created_at: 2026-07-23T07:39:11.294510015Z
updated_at: 2026-07-23T07:39:11.294510015Z
tags:
    - fix
    - config
    - init
    - evaluator
    - providers
---
# Fix: `pando init` config fails to start in initial state (no providers)

## Report
Reporters said `pando init` generates a config that cannot start Pando. Two
observations: (1) gopls listed as an LSP even in projects without Go files; (2)
many default models/providers pre-configured that may not exist — on a truly
fresh machine with NO provider configured, startup could fail. Same must hold
for the automatic setup done before the TUI/WebUI config panel is used.

## Root cause (real crash)
`config.Validate()` had two hard-error paths reachable on a fresh install with
no usable provider, making `config.Load` return an error so Pando never starts:

1. **Evaluator** — the generated `DefaultConfigTemplate` sets `[Evaluator] Enabled = true`
   with no `Model`. `applyDefaultValues` calls `ensureEvaluatorDefaultModel`, which
   seeds the evaluator model from the coder agent's model. With no provider,
   `ensureAgentDefaults`/`setDefaultModelForAgent` resolve no coder model, so the
   evaluator model stays empty and `Validate` returned
   `fmt.Errorf("evaluator.model is required when evaluator is enabled")`.
2. **Agent** — `validateAgent` returned `fmt.Errorf("no valid provider available for agent %s", name)`
   when an agent's model referenced a provider that is present but unusable
   (disabled / no key / no OAuth) and no fallback provider existed. The two sibling
   branches in the same function only warned; this one aborted.

## LSP / gopls — NOT a bug
`DefaultConfigTemplate` leaves the `[LSP.gopls]` block commented out. LSP presets
(`internal/config/lsp_presets.go`) are a lazy catalogue: `initLSPClients`
(`internal/app/lsp.go`) only eagerly starts servers with `Autostart=true`; no
preset sets it. gopls is started on demand only when a `.go` file is edited
(`EnsureLSPForFile`). So gopls is never configured/started in a Go-less project.
The "gopls configured" perception comes from presets shown as a catalogue, not
from an eager/default config entry. No code change made here.

## Changes
`internal/config/config.go` — `Validate()`:
- Evaluator: instead of erroring, when `Evaluator.Enabled && Model == ""`, warn and
  set `cfg.Evaluator.Enabled = false` in memory (config file keeps `Enabled = true`).
  Once a provider + coder model are configured, `ensureEvaluatorDefaultModel` seeds
  and persists `Evaluator.Model`, and the evaluator is active again on the next load.
- Agent: the `no valid provider available for agent` branch in `validateAgent` now
  warns and leaves the agent model empty (matching the two sibling branches) instead
  of returning an error.

## Providers/models in the template
The template only pre-declares `[Providers.anthropic]` with `UseOAuth=true` and an
empty key (reasonable primary for a Claude Code fork). `Validate` marks it disabled
when no credentials exist — harmless. No agent models are written by the template;
`setProviderDefaults` only sets default agent models when a provider is actually
available (env key / OAuth / running Ollama), so no unavailable model is configured.

## Verification
- New regression tests in `internal/config/config_test.go`:
  - `TestValidateDisablesEvaluatorWithoutModel` — Validate succeeds and disables the
    evaluator when it has no model.
  - `TestValidateAgentWithoutProviderDoesNotError` — Validate succeeds when an agent's
    model has no usable provider.
- `go test ./internal/config` (pass), `go build ./...` (ok),
  `go test ./internal/llm/agent ./internal/api` (pass).
- Note: a Load-based no-provider test is not hermetic on dev machines running Ollama
  (auto-detected as a real provider), so the guards target `Validate()` directly.
