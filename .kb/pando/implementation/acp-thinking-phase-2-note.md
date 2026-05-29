# ACP thinking Phase 2 note

Task: `task-97dacd0b`

## Decision

- ACP exposes a new session-scoped selector: `thinking_stream_mode`.
- Accepted values are:
  - `off`
  - `grouped`
  - `full`
- The ACP default remains `grouped` to preserve the Phase 1 grouped-streaming behavior when the client does not override it.

## Runtime nuance

- Thinking visibility is snapshotted at the start of each prompt stream by `processAgentEventStream(...)`.
- That keeps the behavior stable for the in-flight prompt and makes selector changes apply to future prompts, which matches ACP session-config expectations.

## Streaming semantics

- `off`: suppress `ThinkingDelta` updates and let the final reasoning block be sent once from the assembled response.
- `grouped`: keep the Phase 1 buffered flush behavior and mark `sentThinkingDeltas` only after a grouped ACP thought update is actually emitted.
- `full`: forward each `ThinkingDelta` as its own ACP thought update and skip the final full reasoning blob to avoid duplication.
