# ACP thinking Phase 5 note

## Decision

ACP now reconciles thinking-related session settings through a single normalization path whenever:

1. a session is created or loaded;
2. the selected model changes;
3. an ACP thinking config option changes;
4. runtime overrides are about to be forwarded to the LLM layer.

## Why

Before this phase, ACP could display `medium` as the current value for `thinking_mode` or `reasoning_effort` while the session still stored an empty override and the runtime would fall back to global agent config. That made ACP state look valid but left runtime behavior dependent on hidden global settings.

Phase 5 materializes ACP defaults into the session state itself:

- Anthropic reasoning models default `thinking_mode` to `medium`
- reasoning-effort models default `reasoning_effort` to `medium`
- ACP thinking visibility defaults to `grouped`

This keeps the ACP UI, stored session state, and request-time overrides aligned.

## Compatibility rules

- Unsupported inference overrides are cleared automatically on model change.
- `thinking_stream_mode` remains available as an ACP presentation control, but inference controls are now strictly model-gated.
- Explicit valid user selections still win over defaults.
