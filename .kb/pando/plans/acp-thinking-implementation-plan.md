# ACP Thinking Visibility and Configuration — Implementation Plan

## Objective

Enable ACP clients to:

1. see model thinking during generation without flooding the client with token-by-token updates, using grouped thinking blocks;
2. configure thinking behaviour from the ACP client with selectors that adapt to the selected model;
3. apply those ACP-side settings to the actual model runtime, not only to the UI.

## Design Principles

- Separate inference controls from presentation controls.
  - Inference: `reasoningEffort`, `thinkingMode`
  - ACP presentation: `thinking_stream_mode`
- Preserve backwards compatibility.
  - If no ACP-specific override exists, keep current behaviour.
- Make options model-aware.
  - Only show controls that are valid for the selected model.
- Deliver value early.
  - First make thinking visible in ACP with grouped streaming.
  - Then expose configuration.
  - Then wire configuration into the provider runtime.
- Keep ACP settings session-scoped.
  - ACP users should be able to change model/thinking for the session without mutating the global agent configuration.

---

## Phase 0 — Discovery and Contract Validation

### Goal
Confirm ACP client semantics and lock down the internal contract before touching runtime behaviour.

### Tasks
- Verify how the target ACP client handles `UpdateAgentThoughtText(...)`.
  - Determine whether it appends chunks or replaces the full thought content.
- Verify that dynamic `ConfigOptions` rerender correctly when the selected model changes.
- Review the exact flow in:
  - `internal/mesnada/acp/prompt_handler.go`
  - `internal/mesnada/acp/session_state.go`
  - `internal/mesnada/acp/agent.go`
- Build a capability matrix for:
  - `CanReason`
  - `SupportsReasoningEffort`
  - provider-specific `ThinkingMode` support

### Deliverable
- A short implementation note confirming:
  - whether ACP thought updates should send deltas or accumulated text;
  - which models/providers should expose which ACP config options.

### Risk
Low.

---

## Phase 1 — Grouped Thinking Streaming for ACP

### Goal
Make thinking visible in ACP while avoiding one message per token.

### Strategy
Add a grouped-thinking aggregator in ACP that buffers `ThinkingDelta` events and flushes them in blocks.

### Tasks
- Update `internal/mesnada/acp/prompt_handler.go`.
- Extend `streamPromptResponse(...)` with local grouped-thinking state:
  - pending thinking buffer;
  - last flush timestamp;
  - helper `flushThinking(...)`.
- Handle `AgentEventTypeThinkingDelta` by:
  - buffering the delta;
  - flushing only when thresholds are reached.
- Flush when any of these occurs:
  - elapsed time threshold (for example 250–500ms);
  - character threshold (for example 300–600 chars);
  - a major event arrives (`ToolCall`, `ToolResult`, `Response`, `Error`).
- Before `processAgentResponse(...)`, flush pending thinking.
- Mark `sentThinkingDeltas = true` when a grouped flush is actually emitted, so the final full reasoning blob is not duplicated.

### Files
- `internal/mesnada/acp/prompt_handler.go`

### Acceptance Criteria
- ACP shows thinking during generation in grouped blocks.
- The number of thinking updates is much lower than the number of raw thinking deltas.
- The final response does not duplicate already-streamed grouped thinking.

### Risk
Low to medium.

---

## Phase 2 — ACP Config Option for Thinking Visibility

### Goal
Allow ACP users to choose how thinking is streamed to the client.

### New Session Config Option
- `thinking_stream_mode`

### Proposed Values
- `off` — only show final reasoning, no streaming thinking updates
- `grouped` — show thinking in periodic grouped blocks
- `full` — pass through every thinking delta when supported and tolerated by the client

### Recommended Default
- `grouped`

### Tasks
- Extend ACP session state in `internal/mesnada/acp/session.go` with `thinkingStreamMode string`.
- Add a select config option in `buildSessionConfigOptions(...)` in `internal/mesnada/acp/session_state.go`.
- Extend `SetSessionConfigOption(...)` in `internal/mesnada/acp/agent.go` to parse and store `thinking_stream_mode`.
- Update `streamPromptResponse(...)` in `internal/mesnada/acp/prompt_handler.go` to honour:
  - `off`
  - `grouped`
  - `full`

### Files
- `internal/mesnada/acp/session.go`
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent.go`
- `internal/mesnada/acp/prompt_handler.go`

### Acceptance Criteria
- ACP session config includes a thinking visibility selector.
- Changing the selector updates session behaviour immediately for future prompts.
- `off`, `grouped`, and `full` each produce distinct streaming behaviour.

### Risk
Low.

---

## Phase 3 — Model-Aware ACP Config Options

### Goal
Show only thinking-related controls that are valid for the currently selected model.

### New/Conditional Options
- `reasoning_effort`
  - shown only for models with `SupportsReasoningEffort`
  - values: `low`, `medium`, `high`
- `thinking_mode`
  - shown only for Anthropic reasoning-capable models
  - values: `disabled`, `low`, `medium`, `high`
- `thinking_stream_mode`
  - shown for all reasoning-capable models, or always if desired for ACP consistency

### Tasks
- Update `buildSessionConfigOptions(...)` in `internal/mesnada/acp/session_state.go`.
- Resolve the selected model using `currentModel`.
- Inspect model metadata from `models.SupportedModels`.
- Conditionally append config options based on:
  - `CanReason`
  - `SupportsReasoningEffort`
  - provider type
- Ensure `sendSessionConfigOptionsUpdate(...)` re-renders valid options when model changes.
- Automatically clear incompatible session overrides when the model changes.

### Files
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent.go`
- `internal/mesnada/acp/session.go`

### Acceptance Criteria
- Selecting a Claude reasoning model shows `thinking_mode`.
- Selecting a reasoning-effort model shows `reasoning_effort`.
- Selecting a non-reasoning model hides both invalid controls.
- Config options update dynamically after model changes.

### Risk
Low.

---

## Phase 4 — Session-Scoped ACP Inference Overrides

### Goal
Make ACP thinking controls affect the actual provider/runtime behaviour for the current session.

### Runtime Semantics
Session overrides should take precedence in this order:

1. ACP session override
2. global agent config
3. built-in defaults

### New Session State Fields
- `reasoningEffort string`
- `thinkingMode string`
- `thinkingStreamMode string` (already added in Phase 2)

### Tasks
- Extend ACP session state in `internal/mesnada/acp/session.go` with session-scoped inference overrides.
- Extend `SetSessionConfigOption(...)` in `internal/mesnada/acp/agent.go` with:
  - `reasoning_effort`
  - `thinking_mode`
- Validate these values against the selected model.
- Introduce a runtime override mechanism for LLM invocation.
  - Recommended: a single session override structure, e.g. `SessionLLMOverrides`.
- Update provider creation in `internal/llm/agent/agent.go` so ACP session overrides take precedence over global config.
- Ensure Anthropic receives `ThinkingMode` overrides and OpenAI/Copilot receive `ReasoningEffort` overrides where supported.

### Files
- `internal/mesnada/acp/session.go`
- `internal/mesnada/acp/agent.go`
- `internal/llm/agent/agent.go`
- related service interfaces if a session override object needs to be threaded through

### Acceptance Criteria
- Setting `reasoning_effort=high` in ACP changes provider params for supported OpenAI/Copilot-style models.
- Setting `thinking_mode=high` in ACP changes provider params for supported Anthropic models.
- Switching models automatically invalidates or clears incompatible inference overrides.
- Sessions without ACP overrides keep existing behaviour.

### Risk
Medium.

---

## Phase 5 — Central Validation and Normalisation

### Goal
Make model-specific ACP thinking behaviour robust and predictable.

### Tasks
- Introduce helper functions such as:
  - `supportsACPReasoningEffort(model models.Model) bool`
  - `supportsACPThinkingMode(model models.Model) bool`
  - `supportsACPThinkingStreaming(model models.Model) bool`
- Normalise accepted values for:
  - `reasoning_effort`
  - `thinking_mode`
  - `thinking_stream_mode`
- Reset incompatible values automatically when the selected model changes.
- Define smart defaults:
  - Anthropic reasoning model → `thinking_mode = medium`
  - reasoning-effort model → `reasoning_effort = medium`
  - ACP thinking visibility → `grouped`

### Files
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent.go`
- helper file in ACP or LLM layer
- optionally share logic with `internal/config/config.go`

### Acceptance Criteria
- Invalid ACP config option combinations are rejected or normalised consistently.
- Model change always leaves the session in a valid state.
- Default values are stable and predictable.

### Risk
Low.

---

## Phase 6 — Optional Persistence Across ACP Session Resume

### Goal
Preserve ACP thinking preferences when sessions are resumed or reloaded.

### Persisted State
- selected model
- `reasoningEffort`
- `thinkingMode`
- `thinkingStreamMode`

### Tasks
- Extend ACP session persistence to include the new fields.
- Update load/resume flows to restore them.
- Ensure resumed sessions rebuild `ConfigOptions` with the persisted values.

### Files
- `internal/mesnada/acp/session.go`
- ACP session load/resume code paths
- related serialization helpers

### Acceptance Criteria
- Closing and resuming a session preserves ACP thinking-related settings.
- Resumed sessions expose the same config option values they had before.

### Risk
Medium-low.

---

## Phase 7 — UX and Tuning Refinement

### Goal
Polish the ACP user experience and tune grouped streaming thresholds.

### Tasks
- Tune grouped thinking thresholds using real usage feedback:
  - time window
  - character window
- Improve config option descriptions, for example:
  - `off`: show only the final reasoning summary
  - `grouped`: show reasoning in periodic blocks
  - `full`: show every reasoning chunk; may be noisy
- Add debug logging for:
  - grouped thinking flushes
  - applied thinking stream mode
  - applied ACP inference overrides

### Files
- `internal/mesnada/acp/prompt_handler.go`
- `internal/mesnada/acp/session_state.go`
- related logging points

### Acceptance Criteria
- Grouped thinking feels responsive without being noisy.
- ACP config labels are clear and self-explanatory.
- Debug logs make behaviour easy to inspect during development.

### Risk
Low.

---

## Recommended Implementation Order

### Optimal MVP
1. Phase 1 — grouped ACP thinking streaming
2. Phase 2 — ACP thinking visibility selector
3. Phase 3 — model-aware ACP config options

This yields immediate visible value:
- ACP can show thinking live;
- the user can control visibility;
- the UI adapts to the selected model.

### Full Implementation
4. Phase 4 — session-scoped runtime inference overrides
5. Phase 5 — validation and normalisation
6. Phase 6 — persistence across session resume
7. Phase 7 — UX tuning and debug support

---

## Key Risks and Mitigations

### Risk 1 — ACP thought updates are replace, not append
Mitigation:
- validate in Phase 0;
- if replace semantics are required, send accumulated grouped text instead of grouped deltas.

### Risk 2 — Changing models leaves stale incompatible config
Mitigation:
- clear or normalise invalid session overrides whenever model changes;
- rebuild config options immediately.

### Risk 3 — No clean runtime entry point for session overrides
Mitigation:
- introduce a single explicit session override structure instead of scattered setter logic.

---

## Final Recommendation

The most effective implementation path is:

1. grouped thinking visibility in ACP;
2. ACP control over thinking visibility;
3. model-aware ACP config options;
4. real session-scoped runtime overrides for inference controls.

This sequence delivers early UX improvements while keeping the more invasive provider/runtime work isolated to later phases.