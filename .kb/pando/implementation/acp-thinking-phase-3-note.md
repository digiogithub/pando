# ACP thinking Phase 3 note

Task: `task-5c1084cb`

## Decision

- ACP now builds thinking-related config options from the **selected model's registry metadata** in `internal/llm/models.SupportedModels`.
- `thinking_mode` is shown only for Anthropic models with `CanReason`.
- `reasoning_effort` is shown only for models with both `CanReason` and `SupportsReasoningEffort`.
- `thinking_stream_mode` remains visible across models for ACP UI consistency, while the model-specific inference controls are the ones hidden dynamically.

## Model-switch behavior

- Both ACP model-switch entry points now rebuild config options from the newly selected model:
  - `session/set_config_option` with `model`
  - `session/set_model` / `session/set_model_unstable`
- Session-scoped transient values for hidden controls are cleared immediately when a model no longer supports them:
  - switching away from Anthropic reasoning models clears `thinking_mode`
  - switching away from reasoning-effort-capable models clears `reasoning_effort`

## Follow-on nuance for later phases

- The session now stores `reasoningEffort` and `thinkingMode` as transient ACP overrides even though Phase 3 only uses them for UI/config-state validity.
- Phase 4 should consume these stored session values as the ACP-side runtime override source instead of re-deriving values from visible option defaults.
