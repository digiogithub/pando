# ACP thinking Phase 0 note

Task: `task-93d5995c`

## Validated contract

- **ACP thought/message streaming is append-like, not replace-like.**
  - The ACP SDK exposes `agent_message_chunk` / `agent_thought_chunk` helpers, not replacement helpers.
  - The SDK example agent emits multiple `UpdateAgentMessageText(...)` calls to continue one message.
  - Pando already treats both text and thought chunks as append-style: `processAgentResponse(...)` skips sending the final full blob when streaming deltas were already sent because otherwise the client would see duplicate text/reasoning.
  - For later phases, streamed thought updates should therefore send **grouped delta chunks**, not the full accumulated reasoning each flush.

- **ACP config option refresh is a full-list replacement contract.**
  - `acp-go-sdk` documents `UpdateConfigOptions(...)` as replacing the full visible config-option set.
  - Pando already sends config refreshes from:
    - `SetSessionMode(...)`
    - `SetSessionModel(...)`
    - `SetSessionConfigOption(...)`
  - `SetSessionConfigOption(...)` also returns rebuilt `ConfigOptions` in-band.
  - Conclusion: server-side ACP wiring is already compatible with model-aware config-option rerendering.

## Current ACP thinking behavior

- `internal/mesnada/acp/prompt_handler.go` has a handler for `AgentEventTypeThinkingDelta`.
- But `internal/llm/agent/agent.go` currently **does not forward** `ThinkingDelta` events to the ACP-facing `eventCh`; it only appends reasoning into the assembled assistant message and publishes the delta on pubsub.
- Result: ACP currently receives the **final accumulated reasoning blob** through `AgentEventTypeResponse`, not live thought streaming.
- Phase 1 therefore needs both:
  - grouped flush logic in ACP; and
  - an upstream decision on how ACP will receive thought deltas without reintroducing tool-event starvation.

## Capability matrix

- Registry snapshot from `internal/llm/models`: **26** static model entries set `CanReason: true`; **0** set `SupportsReasoningEffort: true`.

| Model family / provider | Current project evidence | Reasoning-visible (`CanReason`) | `SupportsReasoningEffort` | Runtime control path today | ACP option exposure safe in Phase 3 |
| --- | --- | --- | --- | --- | --- |
| Anthropic static reasoning models (`claude-3.7-sonnet`, Claude 4 Sonnet/Opus/4.1/4.5 Sonnet/4.5 Opus/4.6 Sonnet/4.6 Opus) | Static registry sets `CanReason: true`; Anthropic provider accepts `ThinkingMode`; `defaultAnthropicThinkingMode(...)` defaults reasoning models to `medium` | Yes | No static entries set this true | `WithAnthropicThinkingMode(...)` is wired; Anthropic also consumes `ReasoningEffort` internally for thinking budget | **Expose `thinking_mode` + `thinking_stream_mode`** |
| OpenAI reasoning families | OpenAI provider applies `ReasoningEffort` when `model.CanReason`, but current model metadata for dynamically fetched OpenAI models does not populate capability bits | Metadata usually unavailable in ACP today | No entries set true | Provider path exists for reasoning effort, but ACP cannot detect support from current model list alone | **Do not expose yet unless capability metadata/inference is added** |
| Copilot reasoning families | Copilot provider can apply `ReasoningEffort` only when both `CanReason` and `SupportsReasoningEffort` are true | Metadata unavailable in current fetched-model path | No entries set true | Copilot-specific reasoning option exists, but current provider construction does not thread it from session/global config | **Do not expose yet** |
| Azure O-series static models (`o1`, `o1-mini`, `o3`, `o3-mini`, `o4-mini`) | Static registry sets `CanReason: true` | Yes | No | `internal/llm/agent/agent.go` does not currently wire Azure reasoning controls into provider creation | **At most `thinking_stream_mode`; no inference controls yet** |
| Gemini / Antigravity reasoning-capable static models | Static registry sets `CanReason: true` on multiple Gemini and Antigravity models | Yes | No | No ACP-specific inference control path for `reasoning_effort` or `thinking_mode` | **At most `thinking_stream_mode`** |
| Local reasoning-capable models | Local model conversion sets `CanReason: true` | Yes | No | Local uses OpenAI-style reasoning-effort path, but no metadata advertises support | **Do not expose until capability inference is defined** |
| Dynamic fetched models in general | `FetchedModel` has no capability fields; `RefreshProviderModels*` registers models without `CanReason` / `SupportsReasoningEffort` enrichment | Usually unknown | Unknown/false | Refresh works for model selection, not capability-aware ACP options | **Hide model-specific reasoning controls unless capability enrichment is added** |

## Durable decisions for later phases

1. **Thought streaming should use append-style grouped deltas.**
2. **`sendSessionConfigOptionsUpdate(...)` should continue sending the full rebuilt option list after model changes.**
3. **Phase 3 should rely on real model capability metadata, not model-name heuristics, wherever possible.**
4. **Dynamic provider-discovered models are currently the main blocker for accurate ACP reasoning-control exposure.**

## Files inspected

- `internal/mesnada/acp/prompt_handler.go`
- `internal/mesnada/acp/session_state.go`
- `internal/mesnada/acp/agent.go`
- `internal/mesnada/acp/session.go`
- `internal/llm/agent/agent.go`
- `internal/llm/models/*.go`
- `internal/llm/provider/{anthropic,openai,copilot,provider}.go`
- `internal/api/handlers_models.go`
- ACP SDK module: `github.com/madeindigio/acp-go-sdk`
