---
created_at: 2026-10-01T15:00:47.273434039Z
updated_at: 2026-10-01T15:00:47.273434039Z
tags:
    - feature
    - persona
    - model-routing
---
# Persona auto-select with a decision model — implementation (PANDO-EP-0017, 2026-10-01)

Builds on [[pando/features/model-auto-mode-implementation.md]] and [[pando/analysis/model-auto-mode-systemone-router.md]] (EP-0015). Status: implemented, uncommitted, epic in review. Implemented with Sonnet subagents, reviewed by an independent read-only pass, review findings fixed.

## What changed

The `persona-selector` agent keeps its LLM model but gains an option `useDecisionModel` (default off). When on, the persona for each user prompt is chosen by the System One / Jev decision model configured in `modelAutoMode.router` (Ollama >= 0.35, TypeSafe Jev, custom gateway). There is no separate router configuration for personas. The agent's LLM is the fallback.

Decision rules (option on):
- matched (`p(choice) >= 0.60`, not `none`): apply and remember the persona for the session.
- `none` / low probability / no personas: sticky — keep the session's previous persona, else `assistant`, else none. No LLM call.
- router error / no router model: LLM fallback (bounded 20 s). If the LLM is unavailable or answers none: sticky/default. Router skipped for 60 s after a failure (keyed by provider + base URL + model).
- cancelled caller context: no cooldown, no warning, no fallback, no event.
- when model auto mode is also active for the turn: one `/v1/systemone` request with questions `task` and `persona` (`RouteWithPersona`); cost and input tokens are attributed to the model decision only.
- non-eligible runs (task/title agents, system-initiated runs, delegated children): no call; use the session's remembered persona, else inherit the parent session's.

Option off: the LLM selector runs for every run exactly as before the epic. Two accepted differences: the persona list now uses `Manager.Description` and includes built-in personas; clean-mode runs skip the (previously discarded) call.

Priority unchanged: per-session explicit persona > manual active persona > auto.

## Files / symbols

- Config: `config.Agent.UseDecisionModel`, `PersonaSelectorUsesDecisionModel()`, `UpdateAgentUseDecisionModel(name, on)` (`internal/config/config.go`); cleared with a warning on other agents. REST `GET/PUT /api/v1/config/agents` field `useDecisionModel` (persona-selector only). Schema: `cmd/schema/main.go`, `pando-schema.json` (hand-edited, not regenerated).
- Personas: `internal/mesnada/persona/persona.go` — YAML front matter `description:` stripped at load, `Description(name)`, `Descriptions()`, `MaxDescriptionLen = 500`; robust to invalid YAML, BOM, horizontal rules. Built-in personas have written descriptions.
- Engine: `internal/llm/modelrouter/persona.go` — `RoutePersona`, `RouteWithPersona`, `PersonaEngineFor`, `PersonaCap`, `PersonaThreshold`, `ReasonNoPersonas`, `ReasonNoRouter`; safe key mapping; 25-persona cap with one warning. `engine.go` refactored around `ask`/`answerFor`/`applyTaskAnswer`. `systemone.Client.DecideLenient`; `systemonetest.Server.SetQuestionDecision`.
- Agent: `internal/llm/agent/persona_decision.go` (sticky LRU map capped at 4096, cooldown, fallback, notices, `PersonaRoutingInfo`, `LastPersonaRouting`, `AppliedAutoPersona`, `ForgetSessionPersona`, `SetPersonaManagerRefresher`), `persona_selector.go` (`NewLazyPersonaSelector`, lazy provider with 30 s error retry), `model_auto.go` (`beginAutoTurn` accepts a precomputed decision), `agent.go` (`processGeneration` persona step before enrichment).
- App: `internal/app/app.go` — always-installed lazy selector, persona manager loader with hot reload and retry (`personaLoadDue`), `startSessionPersonaCleanup` on session delete.
- Observability: external event topic `persona_route` (`extevents.PersonaRouted`), Info log `persona_auto: decision`, notice `Persona: <name> (p=…, … ms)` only when the applied persona changes, `pando doctor` check (`doctorPersonaAutoSelect`). No prompt text anywhere.
- Selectors: `GET /api/v1/personas/active?sessionId=` adds `auto`, `decisionModel`, `applied`, `source`. WebUI `PersonaSelector.tsx` shows `Auto (<persona>)` and a router-health warning; TUI persona dialog `SetAutoApplied`; ACP persona option gained an `auto` entry (new, untested on real Zed/Xcode clients).
- Settings UI: WebUI `AgentsSettings.tsx` (toggle, router line + health, warning, privacy note, "Fallback model" label, `pando:settings-category` event to jump to Model auto mode); TUI `personaSelectorDecisionFields` + `settings_persona_health.go`.

## Verification

- `go build ./...`, `go vet`, `go test ./internal/... ./cmd/... ./pkg/...` all pass. WebUI `tsc` clean, vitest 42/42.
- `-race` passes for agent, modelrouter, persona, app, agui. `-race` on `internal/api` and `internal/mesnada/acp` reports races located in test helper code (log into `bytes.Buffer`); those test files are unmodified by this work; not confirmed against the base commit.
- Live, Ollama 0.35.0 + `tev1:0.8b` (`PANDO_LIVE_OLLAMA=1 go test ./internal/llm/modelrouter/ -run TestPersonaLiveOllama -v`): 5/5 on built-in personas (p 0.67–1.00), "yes, do it" → `none`; 63–75 ms per decision, 151 ms combined request.

## Not done / follow-ups

- No gintrack stories/specs were created for the epic; no requirement traces.
- No benchmark against the LLM selector baseline; no user docs update.
- ACP `auto` persona option not verified in Zed/Xcode. WebUI e2e not run.
- `newPersonaManagerLoader` and the session-delete cleanup hook have no direct tests.
