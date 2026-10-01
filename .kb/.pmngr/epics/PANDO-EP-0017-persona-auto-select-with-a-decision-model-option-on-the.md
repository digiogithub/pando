---
id: PANDO-EP-0017
type: epic
title: "Persona auto-select with a decision model: option on the `persona-selector` agent to classify with the System One / Jev router of model auto mode, keeping the agent's LLM as fallback"
status: in_review
priority: high
author: mcp
labels: [persona, model-routing, ollama, config, agent, webui, tui, acp]
created: 2026-10-01T14:31:10Z
updated: 2026-10-01T15:00:47Z
started: 2026-10-01T15:00:47Z
---

## Description

**Today.** Automatic persona selection (`personaAutoSelect.enabled`) runs a generative LLM call per user prompt:

- `PersonaSelector.SelectPersonaContent` (`internal/llm/agent/persona_selector.go:249`) builds a text prompt listing every persona as `- name: first line of the .md`, truncates the user prompt to 600 bytes, and sends it to the provider configured under `agents["persona-selector"]` (`config.AgentPersonaSelector`).
- The reply is free text. Pando lowercases it, strips quotes and punctuation, and matches it against the persona names. Anything else means "no persona".
- It is called from `getPersonaContent` (`persona_selector.go:142`), which `processGeneration` invokes once per user prompt (`internal/llm/agent/agent.go:1334`). The result is injected into the system prompt.

**Problems with the current design.**

- **Latency and cost.** A full chat-completion round trip on a generative model precedes every turn. There is no timeout specific to this call.
- **No calibrated signal.** The answer is a string. There is no probability, so there is no threshold, and a hallucinated or misspelled name silently becomes "no persona".
- **Poor criteria.** The only description of a persona is the first line of its Markdown file.
- **Unstable.** A short follow-up such as "yes, do it" may select a different persona or none. Each change rewrites the system prompt and breaks the provider prompt cache.
- **Not hot-reloadable.** The selector is built once in `internal/app/app.go:852`. Enabling it needs a restart.
- **Invisible.** The choice is only written to the debug log.

**What this epic changes.** The `persona-selector` agent stays, and gains one option: **use a decision model**. When the option is on, the persona is chosen with the same mechanism as model auto mode (PANDO-EP-0015): one `choice` question sent to a decision model through the System One / Jev protocol, `POST {baseURL}/v1/systemone`. The agent's own LLM model becomes the fallback.

1. **One option on the agent.** The configuration of the `persona-selector` agent gets a toggle, "Use decision model (from model auto mode)". It is off by default, so existing installations behave as today until the user turns it on.
2. **Router taken from model auto mode.** There is no second router configuration. The decision provider, base URL, API key and router model are read from `modelAutoMode.router`. The backends are therefore the same three: Ollama 0.35 or later (local), TypeSafe Jev (hosted), and any Jev-compatible gateway (OpenRouter, LiteLLM, Vercel AI Gateway, Kev).
3. **Classifier.** The criteria are the available personas plus `none`. Each criterion carries a natural-language description of when that persona applies, read from the persona file. The decision model returns a probability per persona. The winner is applied only when `probabilities[choice]` reaches the threshold and the choice is not `none`.
4. **LLM agent as fallback.** When the decision model cannot answer (no router configured, unreachable, unauthorized, model missing, Ollama older than 0.35, timeout, malformed response), the turn falls back to the existing LLM selection with the model configured on the `persona-selector` agent.
5. **Sticky persona.** When the decision model does answer, but with `none` or a probability below the threshold, the session keeps the persona of its previous turn. The LLM fallback is not called in that case: the classifier gave a valid answer.
6. **One call when both features are on.** The Jev request accepts 1 to 64 questions. When model auto mode is also active for the turn, Pando sends one request with two questions (`task` and `persona`) instead of two requests.

### Codebase anchors (exploration 2026-10-01)

- **Current selector:** `internal/llm/agent/persona_selector.go` (`PersonaSelector`, `NewPersonaSelector`, `SelectPersonaContent`, `getPersonaContent`, `extractPersonaTitle`).
- **Call site:** `internal/llm/agent/agent.go:1334`, before model auto mode routes the turn (`beginAutoTurn`, `internal/llm/agent/model_auto.go:244`).
- **Startup wiring:** `internal/app/app.go:834-875` (persona manager, selector, restore of the persisted active persona).
- **Agent config:** `AgentPersonaSelector` (`internal/config/config.go:79`, :94, :1893, :1929) and the per-agent config struct in the same file; init templates (`internal/config/init.go`, `cmd/init.go`); `cmd/schema/main.go`.
- **Feature config, unchanged:** `PersonaAutoSelectConfig{Enabled, PersonaPath}` (`config.go:844`), `UpdatePersonaAutoSelect` (:6199), `internal/config/persona_active.go`.
- **Persona manager:** `internal/mesnada/persona/persona.go` (`Manager`, `ListPersonas`, `GetPersona`, `HasPersona`). Built-ins in `internal/mesnada/persona/builtin/`: `assistant`, `qa`, `software-engineer`, `system-engineer`.
- **Per-session scope:** `SessionLLMOverrides.Persona`, `PersonaScoped` and `Prompt` (`internal/llm/agent/session_overrides.go`). ACP and AG-UI sessions that are persona-scoped with no explicit persona also go through the auto-selector.
- **Reusable pieces from EP-0015:**
  - `internal/llm/systemone`: client, `DecisionProvider`, Ollama and remote providers, typed errors, health cache, `systemonetest` fake server.
  - `internal/llm/modelrouter`: `Engine.Route` (`engine.go:155`), `BuildState` and token-aware truncation (`state.go`), `classify`, `RouterHealth`, `ProviderFor`, warm-up on reload. The engine is currently tied to `config.ModelAutoModeConfig` and to model candidates.
  - `config.DecisionRouterConfig` (`internal/config/model_auto_mode.go`) with `EffectiveProvider`, `EffectiveBaseURL`, `EffectiveAPIKey`.
  - Notice plumbing: `emitRoutingNotice`, `announceRouting`, `warnAutoOnce` (`model_auto.go`), SSE system-message forwarding, AG-UI custom event, `extevents`.
- **UI:**
  - Agents settings: `web-ui/src/components/settings/AgentsSettings.tsx` and the TUI agents section in `internal/tui/page/settings.go`.
  - Persona auto-select section: `buildPersonaAutoSelectSection` and `savePersonaAutoSelect` (`settings.go:3314`, :5834).
  - Selectors: `web-ui/src/components/shared/PersonaSelector.tsx`, `web-ui/src/components/layout/Header.tsx`, `internal/tui/components/dialog/persona.go`.
  - REST: `/api/v1/personas`, `/api/v1/personas/active` (`internal/api/routes.go:229`, `handlers_personas.go`).

### Protocol facts that constrain the design

- A `choice` question takes 2 to 26 criteria. One slot goes to `none`, so at most **25 personas** can be offered per question.
- Ollama caps the request body at 64 KiB and never truncates. `tev1:0.8b` loads with `num_ctx 2050`. Persona descriptions must be short, and the state must be truncated with token awareness (`modelrouter.BuildState`).
- `confidence` is entropy concentration, not calibrated correctness. The decision uses `probabilities[choice]`.
- Hosted providers receive the prompt text. Ollama keeps it local.

## Acceptance Criteria

- [ ] **Agent option.** The `persona-selector` agent configuration has a boolean option (working name `useDecisionModel`), default off. It is validated, persisted, hot-reloaded, lock-aware, and exposed through REST and the JSON schema. It is only meaningful for this agent and is not shown for the others.
- [ ] **No second router configuration.** The decision provider is read from `modelAutoMode.router`, whether or not `modelAutoMode.enabled` is on. No router, base URL, API key or model field is added for personas. Threshold (0.60) and timeout (the model auto mode defaults: 1500 ms for Ollama, 3000 ms for remote) are constants in code.
- [ ] **Option off.** Behaviour is the current LLM selection, unchanged.
- [ ] **Option on, decision model answers.** One `/v1/systemone` request per user prompt. The winning persona is applied when `p(choice) ≥ threshold` and the choice is not `none`. The LLM model of the agent is not called.
- [ ] **Sticky behaviour.** On `none` or a probability below the threshold, the turn uses the persona applied in the previous turn of the same session, or `assistant` when there is none. The LLM fallback is not called. The system prompt is byte-identical between two consecutive turns that resolve to the same persona.
- [ ] **Fallback to the LLM agent.** When the decision call fails (no router model configured, unreachable, 401/403, model missing, Ollama older than 0.35, timeout, 4xx/5xx, malformed response), the turn runs the existing LLM selection with the agent's model. One warning per session and error class is shown. If the agent has no usable model either, the sticky or default persona is used and the prompt is never blocked.
- [ ] **Fallback health.** After a failure the decision model is tried again on later prompts. The cached router health (about 60 s) is used so an unreachable router does not add its timeout to every turn.
- [ ] **Persona descriptions.** Each persona offered to the classifier has a description of at most 500 characters, taken from a `description:` front-matter field in the persona `.md`, or from the first heading or line of the file when there is none. The four built-in personas ship with a written description. The front matter is never injected into the system prompt. The LLM fallback uses the same descriptions.
- [ ] **Classifier engine.** The decision logic of `modelrouter` is generalised so the same code serves model routes and personas: build the `choice` question plus `none`, truncate the state to the model's context budget, apply the threshold, classify errors. The persona engine returns the persona name, probability, per-persona probabilities, reason, latency, token count and cost. It holds no per-session state.
- [ ] **One decision per prompt.** There are no persona decisions per tool iteration, for system-initiated runs (resumed delegations, compaction, title generation) or for subagents.
- [ ] **Combined request.** When model auto mode is also active for the turn, a single `/v1/systemone` request carries both questions and both decisions are taken from its response. A test asserts the request count.
- [ ] **Priority is unchanged.** Per-session explicit persona, then the manually selected active persona, then auto-selection. A manually selected persona triggers neither a decision call nor an LLM call.
- [ ] **More than 25 personas.** The first 25 in name order are offered to the decision model and one warning is logged. The cut is deterministic and documented.
- [ ] **Hot reload.** Changing the agent option, the agent's model, `personaAutoSelect`, or `modelAutoMode.router` takes effect on the next prompt without a restart.
- [ ] **Agents settings UI (WebUI and TUI, at parity).** The `persona-selector` agent shows:
  - the toggle "Use decision model (from model auto mode)";
  - a read-only line with the router in use (provider and model) and its health, and a link to the model auto mode settings;
  - a warning when the toggle is on and no router model is configured;
  - a privacy note when the provider is hosted;
  - the agent's model selector, labelled as the fallback when the toggle is on.
- [ ] **Persona selector.** In the WebUI `PersonaSelector`, the TUI persona dialog and ACP session options, the "Auto" entry shows the persona currently applied to the session.
- [ ] **Observability.**
  - A notice `Persona: <name> (p=…, … ms)` is shown in every client (WebUI, desktop, TUI, ACP, AG-UI) **only when the applied persona changes**. It says when the LLM fallback made the choice.
  - A `PersonaRouted` external event and telemetry counters are emitted. They carry the persona name, the source (`decision` or `llm`), reason, probability, latency and cost, and never the prompt text.
  - `pando doctor` reports whether the option is on, whether the router is usable, whether the fallback agent has a model, and how many personas are offered.
- [ ] **Specs and tests.** The gintrack specs of this epic have every requirement traced to tests. Unit tests use the `systemonetest` fake server, including the fallback path. A live check runs against Ollama 0.35 with `tev1:0.8b`, and a benchmark over a labelled prompt set (built-in personas) reports accuracy and p50/p95 latency of the decision model against the LLM selector.
- [ ] **Docs.** User documentation, config schema and the KB summary are updated.

## Notes

- **Decisions (user, 2026-10-01).**
  - The `persona-selector` agent is kept and acts as the fallback.
  - The decision model is enabled by an option in that agent's configuration.
  - The router comes from the model auto mode configuration. Persona auto-select gets no router settings of its own.
- **Assumption to confirm: when the fallback fires.** The LLM agent is called only when the decision model fails to answer. A `none` or low-probability answer keeps the previous persona and does not call the LLM. The alternative, calling the LLM on low probability too, would add a generative call to most short follow-ups.
- **Assumption to confirm: default off.** The option is opt-in so that upgrading changes nothing. It could default to on when `modelAutoMode.router` already has a model.
- **Why sticky.** Model auto mode sends unmatched prompts to the coder model. Doing the same with personas would flip the system prompt on every "ok, continue", which breaks the provider prompt cache and changes the agent's behaviour mid-task. The model auto mode benchmark showed short follow-ups scoring `none` with p = 0.71 to 0.93, so this case is frequent.
- **Order inside a turn.** Persona content is resolved at `agent.go:1334`, before `beginAutoTurn`. The combined request needs both decisions to be taken at one point, before the system prompt is built and before `prepareProvider`.
- **Persona-scoped sessions.** ACP and AG-UI sessions with `PersonaScoped` and no explicit persona go through the auto-selector today. They keep doing so, with the sticky state kept per session id.
- **Mesnada subagent personas are out of scope.** `mesnada.orchestrator.personaPath` and the persona passed to `spawn_agent` are chosen by the caller and are not classified.
- **Related known bug.** `pando/analysis/webui-settings-persistence-bugs-2026-09-30.md` describes the active persona not surviving a restart. `SetAndPersistActivePersona` exists now. This epic must keep an explicit saved "Auto" choice as "auto-select", not as `assistant`.
- **Suggested stories, in implementation order:**
  1. Config: the decision-model option on the `persona-selector` agent, validation, persistence, REST, schema, hot reload.
  2. Persona descriptions: front matter in `persona.Manager`, built-in descriptions, 25-persona cap, shared with the LLM prompt.
  3. Engine: extract a generic choice classifier from `modelrouter`, add the persona engine, resolve the router from `modelAutoMode.router`.
  4. Agent integration: decision path, LLM fallback, sticky state per session, eligibility rules, hot reload.
  5. Combined request with model auto mode.
  6. Agents settings UI (WebUI and TUI), selectors and observability: notice on change, event, telemetry, doctor.
  7. Docs, live validation, benchmark against the LLM selector, spec coverage, KB summary.
- **Suggested specs:** agent option and persona descriptions; classifier engine and combined request; agent integration, fallback and sticky behaviour; settings, selectors and observability.
- **References:** PANDO-EP-0015 and its specs PANDO-SP-0001 to PANDO-SP-0004; KB `pando/analysis/model-auto-mode-systemone-router.md` and `pando/features/model-auto-mode-implementation.md`; docs.ollama.com/api/systemone.
