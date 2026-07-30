---
created_at: 2026-07-30T14:57:35.791147699Z
updated_at: 2026-07-30T15:08:36.789493651Z
tags:
    - plan
    - tools
    - agent
    - models
    - pando_setup
---
# Plan: dynamic model switching from `pando_setup` (2026-07-30)

Related: [[pando/features/pando_setup_internal_tool.md]]

## Goal

Let the agent (a) discover the models available across configured provider accounts and
(b) switch the model **mid agent-loop**, when the user asks for it, without cancelling the
run and without touching global configuration.

The capability is **opt-in: disabled by default**, and an upgrade to a more expensive model
requires an explicit two-step confirmation.

## Current state (verified 2026-07-30)

Already present, no work needed:

- `pando_setup models` — `internal/llm/tools/pando_setup.go:194` (`runSetupModels`), with
  `--provider / --account / --search / --detail / --limit`. Model **discovery is done**.
- `pando_setup providers` — account list + credential state.
- Per-session, request-scoped model override:
  `SessionLLMOverrides.Model` in `internal/llm/agent/session_overrides.go:23`,
  setter `SetSessionLLMOverrides` (`session_overrides.go:50`), consumed in
  `createAgentProvider` (`internal/llm/agent/agent.go:2397`) through
  `sessionLLMOverridesForContext(ctx)` keyed by `prompt.SessionIDKey`.
  It copies the `config.Agent` map value, so it never mutates global config and is safe for
  concurrent sessions running different models.
- `pando_setup` already resolves the session id via `GetContextValues(ctx)`
  (`pando_setup.go:906`, `:1021`) — the bridge pattern for session-scoped commands exists.
- Per-model cost metadata already exists on `models.Model`: `CostPer1MIn`, `CostPer1MOut`,
  `CostPer1MInCached`, `CostPer1MOutCached` (used by `TrackUsage`, `agent.go:1810`), and
  `pando_setup models --detail` already prints it.

Two real gaps:

1. No `pando_setup` command (and no `SetupBridge` method) to *set* the session model.
2. `requestProvider` is built once, **before** the loop (`agent.go:1025`) and reused on every
   iteration (`agent.go:1050`, `:1059`). Changing the override mid-run has no effect until the
   next user turn.

### Explicitly rejected approach

Do **not** route this through `agent.Update()` (`agent.go:1831`). It refuses while
`IsBusy()` (`"cannot change model while processing requests"`), writes global config via
`config.UpdateAgentModel`, and replaces the shared `a.provider` — a data race against other
sessions in the same process (ACP multi-session, warm delegation instances).

---

## Phase 1 — Read side of the override + merge-safe setter

`SetSessionLLMOverrides` replaces the whole struct, so a naive write from the bridge would
wipe `Persona` / `PersonaScoped` / `ReasoningEffort` / `ThinkingMode` set by ACP or the Web UI.

- Add exported `SessionLLMOverridesFor(sessionID string) SessionLLMOverrides` in
  `internal/llm/agent/session_overrides.go` (the existing lookup is ctx-based and private).
- Add `SetSessionModelOverride(sessionID string, model models.ModelID)` that loads, mutates only
  `Model`, and stores back. Empty `model` clears just that field (and deletes the entry when the
  result `isEmpty()`, matching current semantics).
- Tests: extend `internal/llm/agent/session_overrides_concurrency_test.go` — set persona then
  model, assert persona survives; clear model, assert persona still set.

## Phase 2 — Configuration gate (default **false**)

Ships before the command is usable, so the feature can never be reached until switched on.

- `InternalToolsConfig` (`internal/config/config.go:648`) gains:
  ```go
  // SetupModelSwitchEnabled lets the agent change the session's model at runtime
  // through pando_setup. Disabled by default: it changes billing and behaviour
  // mid-run, so it must be opted into explicitly.
  SetupModelSwitchEnabled bool `json:"setupModelSwitchEnabled,omitempty" toml:"SetupModelSwitchEnabled"`
  ```
  Note this deliberately breaks the inverted `…Disabled` convention used by
  `AskUserQuestionDisabled` (`config.go:688`): those features are ON by default, this one is OFF,
  so a positive flag is what makes the zero value mean "disabled".
- Optional companion knob for Phase 5:
  `SetupModelSwitchMaxPerRun int` (0 = use the built-in default of 3).
- When disabled: the read form (`model` with no argument) still works and reports the active
  model; any write form returns a clear tool error naming the setting to enable. The command must
  never silently no-op.
- Surface it in the settings panels alongside the other `InternalTools` toggles.

## Phase 3 — `SetupBridge` method + `pando_setup model` command

- `SetupBridge` (`internal/llm/tools/pando_setup.go:75`) gains:
  ```go
  // CurrentModel reports the model in use for the session and whether it comes
  // from a session override or the agent's configured default.
  CurrentModel(sessionID string) (SetupModelState, error)
  // SetSessionModel switches the model for this session only. An empty id clears
  // the override and returns to the agent default. confirmed carries the user's
  // approval for a cost increase (see Phase 4).
  SetSessionModel(sessionID, modelID string, confirmed bool) (SetupModelState, error)
  ```
  with `SetupModelState{ID, Name, Provider, Account, ContextWindow, CostIn, CostOut, CostKnown,
  Overridden bool, Source string}`.
- New command in `setupCommands()`:
  `model [<model-id>] [--clear] [--confirm]` — no argument prints the active model plus a hint to
  run `models` for the catalogue; with an id it switches. Usage text must state that the switch is
  **session-scoped, in-memory, reverts on restart**, and that a cost increase needs `--confirm`.
- `setupBridge` impl (`internal/llm/agent/setup_bridge.go`) validates:
  1. feature enabled (Phase 2);
  2. `models.SupportedModels[id]` exists;
  3. account resolvable and not disabled — reuse the same resolution as
     `createAgentProvider` (`config.ResolveProviderAccountByID` / `ResolveProviderAccountForType`);
  4. cost gate (Phase 4) and the guards/caps of Phases 5–6.
  Then calls `agent.SetSessionModelOverride`.
- Nil-bridge path keeps working (`config`/`providers`/`models` already tolerate it).
- Tests in `internal/llm/tools/pando_setup_test.go` with the existing fake bridge; bridge-side
  tests in `setup_bridge_test.go`.

At the end of Phase 3 the switch takes effect **on the next user turn** — already useful, and a
safe checkpoint to ship independently.

## Phase 4 — Cost-escalation confirmation handshake

Switching to a pricier model changes what the user pays, so it is never done on the agent's own
authority.

**Cost availability detection.** Cost is unknown when the target (or current) model reports
`CostPer1MIn == 0 && CostPer1MOut == 0`. That is genuinely common — local models, `ProviderLocal`,
`__mock`, and catalogue entries the models.dev enrichment could not fill (see
[[feature_modelsdev_catalog]]). Treat it as three cases:

| Case | Behaviour |
| --- | --- |
| Both costs known, target **not** more expensive | Switch immediately, report old → new price. |
| Both costs known, target more expensive | Require confirmation. |
| Either cost unknown | Require confirmation, and say the comparison could not be made. |

**Comparison rule.** More expensive = `CostPer1MIn` **or** `CostPer1MOut` of the target strictly
exceeds the current model's. Comparing a blended figure would let a big output-price jump hide
behind a cheaper input price. Cached rates are reported but not used for the gate.

**Two-call handshake.** First call without `--confirm` performs **no state change** and returns a
plain-text request the agent relays to the user, e.g.:

```
Switching from copilot.gpt-5-mini ($0.25 in / $2.00 out per 1M) to
anthropic.claude-opus-5 ($5.00 in / $25.00 out per 1M) increases the cost of
this session. The model was NOT changed.
Ask the user to confirm, then repeat the call with:
  pando_setup model anthropic.claude-opus-5 --confirm
```

The second call, with `--confirm`, applies it. Requirements:

- The pending proposal is stored per session (`sessionID` → `{modelID, proposedAt}`) so
  `--confirm` only validates against the model that was actually quoted. A `--confirm` for a
  different id, or with no pending proposal, is rejected and starts a fresh confirmation.
- Expire the proposal (e.g. 10 minutes) and drop it on session end, so a stale `--confirm` can
  never apply an old quote.
- Downgrades and equal-cost switches skip the handshake entirely; `--confirm` on them is accepted
  and ignored.
- The confirmation text must state explicitly that nothing changed yet — otherwise the model
  reports success to the user and keeps running on the old model.
- The prompt/system text for the tool must say the confirmation has to come **from the user**, not
  be self-answered by the model. This is a soft guard; the hard guards are the config gate
  (Phase 2) and the per-run cap (Phase 6).

Consider routing the confirmation through the existing `AskUserQuestion` tool
([[project_ask_user_question_tool_plan]]) as a follow-up: it gives a real blocking prompt in
TUI/Web UI instead of relying on the model to ask. Out of scope for the first pass — the
text handshake works on every surface, ACP included.

## Phase 5 — Apply mid-loop (the substantive part)

In `processGeneration` (`agent.go:1050`), at the top of each `for` iteration:

- Compare `requestProvider.Model().ID` with `SessionLLMOverridesFor(sessionID).Model`.
- On difference: rebuild with `a.prepareProvider(promptCtx, content, personaContent)` (it already
  reads the override from ctx), then re-run `a.ensureHistoryFitsBeforeSend(...)` with the new
  provider — that helper (`agent.go:1128`) already compacts when the new context window is
  smaller, which is the main hazard of switching to a smaller model.
- Emit an `AgentEventTypeSystemMessage` (same pattern as auto-compaction, `agent.go:1085`) so
  TUI/Web UI/ACP show the swap, including the new price when known.
- `streamAndHandleEvents` already stamps `Model: requestProvider.Model().ID` on the assistant
  message (`agent.go:1388`), and `TrackUsage` bills per event with `requestProvider.Model()`
  (`agent.go:1733`) — cost attribution after the swap is correct with no extra work.
- The swap point must be **after** tool results are appended and before the next
  `streamAndHandleEvents`, i.e. the same "safe boundary" already used by `drainSteeringInto`, so
  the history is in a valid `tool_call`/`tool_result` shape.

## Phase 6 — Compatibility guards, history sanitation, runaway protection

Blockers that make a naive swap fail at the provider API:

- **Reasoning blocks.** `message.ReasoningContent` (`internal/message/content.go:38`) and
  `ThoughtSignature` (`:106`) persisted by the previous model are rejected when replayed to a
  different model/provider. Add `sanitizeHistoryForModelSwitch(msgs, newModel)` next to the
  existing `sanitizeToolCallHistory` (`agent.go:974`): strip `ReasoningContent` and
  `ThoughtSignature` from assistant messages when the provider or model family changes.
- **Attachments.** `SupportsAttachments` is only checked when entering `Run` (`agent.go:819`).
  Refuse the switch (clear error text back to the agent, no state change) when the history holds
  image parts and the target model does not support them.
- **Tool-call support**: refuse a target model that cannot do tool calls while a tool loop is in
  flight.
- **Cap swaps per run** (default 3, `SetupModelSwitchMaxPerRun`); beyond that the tool refuses for
  the rest of the run. Counter resets when a new run starts, not per session.
- Refusals are returned as tool errors, so the model can pick a different target and retry.

Phase 5 without Phase 6 is unsafe — ship them together.

## Phase 7 — Surfaces, docs, tests

- Verify the swap is reflected in TUI, Web UI, and ACP (`CurrentModelID` in
  `internal/app/app.go:2392` reads the *global* agent model — it must report the session override
  when one is set, otherwise the ACP model picker shows a stale value).
- Decide persistence: the override lives in a `sync.Map` and is lost on restart. Recommendation:
  keep it in-memory for now (matches ACP/Web UI behaviour today) and document it in the command
  usage text rather than adding a session column.
- Tests to add specifically for this plan: cost comparison table (cheaper / pricier / unknown),
  handshake (no state change without `--confirm`, `--confirm` for a different id rejected,
  expiry), config gate off blocks writes but not reads, per-run cap.
- `go test ./internal/llm/agent ./internal/llm/tools ./internal/api`.
- On completion, replace this plan's status with a summary document under
  `pando/features/pando_setup_dynamic_model_switch.md`.

## Sequencing

Phases 1–4 are independently shippable: the switch is opt-in, cost-gated, and applies on the next
user turn. Phases 5+6 add the mid-loop application and must land together.
