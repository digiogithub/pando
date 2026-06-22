---
created_at: 2026-06-21T21:13:27.408388388Z
updated_at: 2026-06-21T21:13:27.408388388Z
tags:
    - change
    - mesnada
    - delegation
    - phase7
    - concurrency
    - acp
    - persona
    - model
---
# Change: Delegation Phase 7.1 — Concurrency hardening (per-session model/persona)

Implemented 2026-06-21. Status: DONE, verified. First sub-phase of the Phase 7
re-plan `pando/plans/delegation_phase7_warm_instance_replan.md`. Independently
valuable (benefits the ACP server + multi-session WebUI today) and a prerequisite
for warm per-project instance reuse (7.2–7.5).

## Problem (the blocker)
A single process serving several concurrent agent loops (ACP server; future warm
delegation instance) was UNSAFE because the ACP `Prompt` handler applied
per-session model/persona by mutating GLOBAL shared agentService state on every
prompt:
- `internal/mesnada/acp/agent.go` Prompt() called `agentService.SetModelOverride(modelID)`
  (→ `config.OverrideAgentModel(AgentCoder, …)`, a global config mutation) and
  `agentService.SetActivePersona(name)` (→ global `activePersonaName`).
Two concurrent sessions with different model/persona clobbered each other.
Thinking/effort were already per-session via `SetSessionLLMOverrides(sessionID,…)`
— the correct pattern this change extends to model + persona.

## Solution — thread model + persona through the per-session override map
Extend the existing request-scoped `SessionLLMOverrides` (read from the run
context, keyed by session ID) to also carry the model and persona, and apply them
in the provider-build / persona-resolution paths instead of global state.

### Files touched
- `internal/llm/agent/session_overrides.go`: added fields `Model models.ModelID`,
  `Persona string`, `PersonaScoped bool` to `SessionLLMOverrides`; `isEmpty()`
  helper; `SetSessionLLMOverrides` now normalizes/stores them and deletes the entry
  only when ALL fields are zero.
- `internal/llm/agent/agent.go` `createAgentProvider`: resolves
  `sessionLLMOverridesForContext(ctx)` near the top and, when `.Model != ""` and the
  model is supported, overrides the LOCAL `agentConfig.Model` copy (request-scoped;
  never mutates `config.Get()`). Reused the same `sessionOverrides` for the existing
  reasoning/thinking opts (removed the duplicate fetch).
- `internal/llm/agent/agent.go` `runInternal`: builds `promptCtx` (carrying
  `prompt.SessionIDKey`) BEFORE persona resolution so per-session lookups work; uses
  it for `getPersonaContent` and the new `effectiveActivePersona` status message.
- `internal/llm/agent/persona_selector.go`: `getPersonaContent` now checks a
  per-session override first — when `PersonaScoped`, an explicit `Persona` name wins,
  else auto-selection is used and the package-global `activePersonaName` is ignored
  for that session. Added `personaManager()` and `effectiveActivePersona(ctx)`
  helpers. Clean mode is still handled upstream via `isCleanModeContext`.
- `internal/mesnada/acp/types_interfaces.go`: added `Model`, `Persona`,
  `PersonaScoped` to the ACP-package `SessionLLMOverrides` (interface boundary).
- `internal/mesnada/acp/thinking_options.go`: new `sessionLLMOverridesFor(session)`
  builds the full per-session override (model + reasoning/thinking + persona,
  `PersonaScoped` always true) from an `*ACPServerSession`.
- `internal/mesnada/acp/prompt_handler.go` (`processPromptWithAgent`) and
  `internal/mesnada/acp/goal_commands.go` (`processGoalPrompt`): now build the
  override via `sessionLLMOverridesFor(acpSession)` before `Run`/`RunGoal`; updated
  the applied-settings log line ("Applying ACP session overrides …").
- `internal/mesnada/acp/agent.go` `Prompt`: REMOVED the global
  `SetModelOverride`/`SetActivePersona` block (replaced by the per-session path).
- `internal/app/app.go` and `cmd/root.go`: the two `SetSessionLLMOverrides`
  adapters now forward the new Model/Persona/PersonaScoped fields.

### Behavior preserved
- Clean mode → empty persona (via existing `cleanModeContextKey`).
- ACP "no explicit persona" → auto-select (PersonaScoped + empty name), matching the
  old "clear global → auto-select" behavior, without depending on global state.
- TUI/API global model + persona setters (`SetModelOverride`/`SetActivePersona`,
  `config.OverrideAgentModel`) are unchanged and still used for the single primary
  session; the per-session override only takes precedence when present.

## Verification
- `go build ./...` OK.
- New `internal/llm/agent/session_overrides_concurrency_test.go`:
  `TestSessionOverridesConcurrentIsolation` (50× two parallel sessions with distinct
  model+persona, asserts each resolves its own via `sessionLLMOverridesForContext`,
  `effectiveActivePersona`, `getPersonaContent`; a global active persona set to the
  OTHER value must not leak) and `TestSetSessionLLMOverridesEmptyDeletes` — both
  PASS under `-race`.
- Updated `TestPandoACPAgent_ProcessPromptWithAgent_LogsAppliedThinkingSettings`
  expectation for the new log line.
- `go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp` all pass.
- Pre-existing, UNRELATED `-race` failures observed but not introduced by this work:
  `TestGoalRunnerRunTimesOutWithConfiguredDuration` (races on the test's injected
  `current` clock var) and ACP `TestTerminalLifecycle` /
  `TestPandoACPAgent_SetConnection_Backfills…` (race on a `bytes.Buffer` in terminal
  I/O plumbing). All pass without `-race`.

## Next (Phase 7.2+)
Delegating/capturing ACP client, warm-target spawn routing (reuse-then-autostart,
no-activeID `EnsureInstance`), Projects-panel integration, config UI + e2e — see the
re-plan doc.
