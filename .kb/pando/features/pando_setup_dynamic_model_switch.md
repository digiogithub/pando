---
created_at: 2026-07-30T15:16:17.628171266Z
updated_at: 2026-07-30T15:51:31.84761459Z
tags:
    - feature
    - tools
    - agent
    - models
    - pando_setup
    - config
---
# Feature: `pando_setup model` — session model switching, phases 1-7 (2026-07-30)

Implements the whole plan [[pando/plans/pando_setup_dynamic_model_switch.md]] (phases 1-7).

Builds on [[pando/features/pando_setup_internal_tool.md]].

## What was built

The agent can read the model its session runs on and — only when the user asks and the
capability is enabled in configuration — switch it for that session alone, **including in the
middle of a running turn**: the switch takes effect on the very next request of the same run, so
a task can start on a cheap model and finish on a stronger one. Any switch to a more expensive
model, or to/from a model with no published price, is quoted first and applied only on a second,
confirmed call.

### Phase 1 — merge-safe per-session model override

`internal/llm/agent/session_overrides.go`:

- `SessionLLMOverridesFor(sessionID) SessionLLMOverrides` — exported, context-free read. The
  existing lookup was `sessionLLMOverridesForContext`, private and ctx-bound, so the bridge could
  not use it. `sessionLLMOverridesForContext` now delegates to it.
- `SetSessionModelOverride(sessionID, models.ModelID)` — read-modify-write of the `Model` field
  only. Writing the whole struct (as `SetSessionLLMOverrides` does) would drop the persona and
  inference settings ACP or the Web UI installed for the session.
- `sessionLLMOverridesMu` serializes writers so that read-modify-write cannot lose a concurrent
  full-struct write. Reads stay lock-free through the `sync.Map`.

### Phase 2 — configuration gate, default `false`

`internal/config/config.go`:

- `InternalToolsConfig.SetupModelSwitchEnabled bool` — deliberately a **positive** flag, unlike
  the neighbouring `AskUserQuestionDisabled`: that feature is ON by default, this one is OFF, so
  the zero value has to mean disabled. Accessor `config.SetupModelSwitchEnabled()`.
- `InternalToolsConfig.SetupModelSwitchMaxPerRun int` + `config.SetupModelSwitchMaxPerRun(fallback)`
  — the per-run cap of phase 6 (0 = built-in default of 3).

Reading the model works regardless of the flag; every write form is refused with an error naming
the setting. `model --clear` counts as a write and is gated too.

### Phase 3 — bridge + `model` command

`internal/llm/tools/pando_setup.go`:

- DTOs `SetupModelState` and `SetupModelSwitch`; `SetupBridge` gains `CurrentModel` and
  `SetSessionModel(sessionID, modelID, confirmed)`.
- Command `model [<model-id>] [--confirm] [--clear]`; `confirm`/`clear` added to
  `setupBooleanFlags` so `--confirm` does not swallow the next word.
- All decisions live in the bridge, the tool only renders. Unknown prices render as
  `price unknown`, never `$0.00`.

`internal/llm/agent/setup_bridge_model.go` (new): `effectiveSessionModel`,
`configuredAgentModel`, `resolveSetupModel` (mirrors `createAgentProvider`'s validation: model in
`models.SupportedModels`, provider account resolvable and enabled, Antigravity exception).
`SessionInfo` now reports the effective model instead of the global one.

### Phase 4 — cost-escalation handshake

- Price known when `CostPer1MIn > 0 || CostPer1MOut > 0`; unknown is legitimate (local models,
  `__mock`, entries models.dev could not enrich — [[pando/features/modelsdev_catalog.md]]).
- Confirmation required when either price is unknown, or the target's **input or output** price
  exceeds the current one. A blended figure would let a large output-price jump hide behind a
  cheaper input price.
- First call stores a `setupModelProposal{Model, QuotedAt}` and changes nothing. `--confirm` is
  honoured only through `consumeSetupModelProposal`: a live quote for that exact model id,
  expiring after 10 minutes, consumed on use. So `--confirm` without a prior quote re-quotes
  instead of applying, a quote for one model never authorizes another, and a stale confirmation
  cannot resurrect an old price.

### Phase 5 — applying the switch mid-loop

`internal/llm/agent/model_switch.go` (new) + `processGeneration` in `agent.go`:

- `applyPendingModelSwitch` runs at the top of every loop iteration, which is the same safe
  boundary `drainSteeringInto` uses: tool results are already appended, so the history has a valid
  `tool_call`/`tool_result` shape. It compares the session's effective model with the model the
  current `requestProvider` was built for, and rebuilds through `prepareProvider` when they differ.
- After a rebuild it sanitizes the history (phase 6) and re-runs `ensureHistoryFitsBeforeSend`
  with the new provider, which compacts when the new context window is smaller.
- It **never aborts the run** (hence no error return): a rebuild that fails, or that comes back on
  the wrong model, drops the override, reports `⚠ Could not switch to …` and continues on the
  model already in use. Without dropping the override, every following iteration would retry the
  same broken switch.
- Success emits `⇄ Model switched to <id> ($x in / $y out per 1M)` as an
  `AgentEventTypeSystemMessage` on both the broker and the run channel, the way auto-compaction
  does. Cost attribution needed no work: `TrackUsage` already bills per event with
  `requestProvider.Model()`.

**Pre-existing bug fixed along the way.** `prepareProvider`'s fast path returned the pre-built
`a.provider` whenever there was no skill manager, no persona and no session policy — ignoring the
per-session model override entirely. That silently answered on the agent's configured model
(affecting ACP/Web UI per-session models too, not just this feature) and would have made the
mid-loop switch retry forever. The fast path now also requires the session model override to be
empty. `applyPendingModelSwitch` additionally threads `prompt.SessionIDKey` into the context it
passes, and treats "rebuilt provider is still on another model" as a failure.

### Phase 6 — guards, sanitation, runaway protection

- `sanitizeHistoryForModelSwitch` strips `ReasoningContent` parts and clears `ThoughtSignature`
  on tool calls in assistant messages, which the new model's API would reject. It copies the
  messages it changes: the slices are shared with what was loaded from the database, which must
  keep the original reasoning. Tool calls themselves are preserved, or the following tool results
  would be orphaned.
- `modelSwitchRunState` (per session, installed by `beginModelSwitchRun` / removed by
  `endModelSwitchRun` around the loop) carries what the bridge cannot see: whether the live
  history contains images, and how many switches this run has already used. `historyHasImages`
  covers user attachments (`BinaryContent`, `ImageURLContent`) and tool-result images, and is
  refreshed on every iteration — a screenshot taken mid-run makes a text-only model unusable from
  that point on.
- Attachment guard: switching to a model without `SupportsAttachments` while images are in the
  history is refused with an explanation, so the agent can pick another model instead of hitting a
  provider rejection.
- Per-run cap (default 3): checked **before** quoting, so an exhausted budget refuses immediately
  instead of asking the user to approve a switch that will be rejected, and consumed **after**
  confirmation, so quotes and guard refusals do not eat it.
- Outside a run neither guard applies: there is no history in flight to be incompatible with.
- **Not implemented**: the plan's "refuse a model that cannot do tool calls". `models.Model` has
  no tool-call capability field, and inventing one to satisfy the plan would be worse than leaving
  the check out.

### Phase 7 — surfaces and persistence

The switch changed which model answers, but every surface still read the *configured* agent model,
and the ACP layer actively undid the switch.

- `agent.SessionModelID(sessionID)` (effective model: override, else configured) and
  `agent.SessionModelOverrideID(sessionID)` (only a runtime switch, else `""`) are exported in
  `setup_bridge_model.go`. The two are not interchangeable: a surface that keeps its own notion of
  the selected model must use the second one, or it would overwrite a deliberate choice with an
  unrelated global default.
- **TUI**: the status bar (`internal/tui/components/core/status.go`, both the context-window figure
  and the model badge) and the chat info sidebar's context window
  (`internal/tui/components/chat/sidebar.go`) now resolve the model through `SessionModelID` for
  the selected session instead of `config.Get().Agents[AgentCoder].Model`. Per-message model labels
  were already correct — `streamAndHandleEvents` stamps `requestProvider.Model().ID`.
- **ACP**: `AgentService` gains `SessionModelOverrideID(sessionID) string`, implemented by both
  adapters (`cmd/root.go`, `internal/app/app.go`). `reconcileACPSessionModel`
  (`internal/mesnada/acp/thinking_options.go`) adopts a runtime switch as the ACP session's model.
  It is called at the start of a prompt/goal run and again after the run ends, and when it changes
  anything a `session_config_options` update is sent so the client's model picker refreshes.
  - This fixes a real bug, not just a stale label: `processPromptWithAgent` rebuilds the *whole*
    override struct from the ACP session on every prompt, so without reconciliation the model the
    ACP session still held would have been handed back to the agent and would have silently
    reverted the switch the user had confirmed.
  - The reverse direction is handled too: `setSessionModelValue` now pushes the client's pick to
    the agent immediately, so an explicit model change in the editor clears the runtime override
    instead of being undone by the next reconciliation.
  - Consequence, on purpose: the adopted model reaches `session_persistence`, so an ACP client that
    restores a session keeps the switched model. The command's usage text says so.
- **Web UI**: nothing to change. Its model selector is the *global* default (`PUT
  /api/v1/models/active`), which the session switch deliberately does not touch, and the API sets
  no per-session overrides, so a switch survives across turns there without extra work. The swap is
  visible through the `⇄ Model switched to …` system message and the per-message model label.
- **Persistence decision**: the override stays in memory (as recommended in the plan) — no session
  column, no migration. The usage text states it, with the ACP session-restore exception noted.
- `pruneExpiredSetupModelProposals` sweeps unconfirmed quotes at the end of each run, so a session
  that asked for a switch and never confirmed does not leave an entry behind for the life of the
  process.

## Files touched

- `internal/llm/agent/session_overrides.go` — exported reader, model-only setter, writer mutex.
- `internal/llm/agent/setup_bridge_model.go` — **new**: bridge model commands, validation, cost
  gate, proposal store, compatibility + budget checks.
- `internal/llm/agent/model_switch.go` — **new**: run state, mid-loop swap, history sanitation.
- `internal/llm/agent/agent.go` — loop swap point, run-state lifecycle, `prepareProvider` fast-path
  fix.
- `internal/llm/agent/setup_bridge.go` — `SessionInfo` reports the effective model.
- `internal/config/config.go` — `SetupModelSwitchEnabled`, `SetupModelSwitchMaxPerRun`, accessors.
- `internal/llm/tools/pando_setup.go` — DTOs, bridge methods, `model` command, renderers, flags.
- `internal/mesnada/acp/types_interfaces.go`, `thinking_options.go`, `prompt_handler.go`,
  `goal_commands.go`, `agent.go` — `SessionModelOverrideID`, model reconciliation both ways.
- `cmd/root.go`, `internal/app/app.go` — adapter implementations.
- `internal/tui/components/core/status.go`, `internal/tui/components/chat/sidebar.go` — surfaces
  read the session's model.
- Tests: `internal/llm/agent/setup_bridge_model_test.go` (**new**),
  `internal/llm/agent/model_switch_test.go` (**new**),
  `internal/llm/agent/session_model_surface_test.go` (**new**),
  `internal/mesnada/acp/session_model_sync_test.go` (**new**),
  `internal/llm/tools/pando_setup_test.go`.

## Known gaps

- No settings-panel toggle for `SetupModelSwitchEnabled`; it is TOML-only, like the other
  `[InternalTools]` flags, which none of the panels expose either.
- No tool-call capability guard (see phase 6).

## Verification

- `go build ./...` — clean; `go vet` clean on every touched package.
- `go test ./internal/...` — everything passes.
- `gofmt -l` on the touched packages reports only files already unformatted before this change
  (`aliases.go`, `lua_tools.go`, `remembrances_kb.go`, `remembrances_code_test.go`).
