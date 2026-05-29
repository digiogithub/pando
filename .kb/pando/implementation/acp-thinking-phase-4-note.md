# ACP thinking Phase 4 note

Task: `task-845cbf2e`

## Decision

- ACP `reasoning_effort` and `thinking_mode` now flow into a **request-scoped runtime override registry** in `internal/llm/agent`.
- The override is keyed by the Pando session ID and read while building the request provider, so precedence is:
  1. ACP session override
  2. global agent config
  3. provider/default behavior

## Runtime wiring

- ACP pushes the current session overrides immediately before `Run(...)` and `RunGoal(...)`.
- The LLM layer resolves overrides from the request context session ID, so only that session's prompt uses them.
- Closing an ACP session clears any stored runtime overrides for that session ID.

## Provider nuance

- OpenAI and local reasoning-capable models use the session override as `WithReasoningEffort(...)`.
- Copilot reasoning-capable models now also receive the session override through `WithCopilotReasoningEffort(...)`.
- Anthropic reasoning-capable models use:
  - session `thinking_mode` first;
  - otherwise the global configured thinking mode;
  - otherwise the existing Anthropic default of `medium`.

## Follow-on nuance for later phases

- The runtime override registry only carries inference controls (`reasoning_effort`, `thinking_mode`); ACP presentation controls like `thinking_stream_mode` remain ACP-local and should not be threaded into provider creation.
- Later phases should keep model-validation/reset logic in ACP and keep the LLM-layer override lookup narrow and request-scoped.
