---
tags:
    - agui
    - change
    - security
---
# AG-UI context entries travel as a separate data message (2026-11-05)

## What changed
- AG-UI `RunAgentInput.context` entries are no longer prepended to the user's message by
  default. They are rendered by `RunAgentInput.ContextDataMessage` as a JSON array inside a
  `<context_data source="client_application">` envelope whose preamble states the content is
  client-supplied data, not instructions and not user text. JSON encoding (`SetEscapeHTML`)
  escapes `<`, `>`, `&`, so a value cannot close the envelope.
- The data message is attached to the run with the new generic
  `agent.WithEphemeralUserMessages(ctx, texts...)`. The agent inserts each text as an
  in-memory user-role message immediately before the run's user message, keeps it for every
  request of the run (tool iterations, output-limit continuations, steering) and re-inserts
  it when the history is reloaded after a mid-run compaction
  (`loadSessionMessagesFromSummary`). It is never written to the message store, so it is not
  in the transcript, not summarised, and not replayed by later runs.
- `agent.EphemeralUserMessages(ctx)` exposes the attached texts to other `Service`
  implementations (test doubles, proxies).
- New `[AGUI] ContextDelivery` (`config.AGUIConfig.ContextDelivery`, json `contextDelivery`):
  `"message"` (default) or `"inline"` (legacy `<context>` prefix, stored with the user turn).
  Unknown values resolve to `"message"` with a warning (`agui.resolveContextDelivery`).
- The prompt given to `svc.Run` — and therefore the stored user message, the session title
  and `MESSAGES_SNAPSHOT` — is exactly the user's text in the default mode.

## Files and symbols
- `internal/llm/agent/ephemeral_messages.go` (new): `WithEphemeralUserMessages`,
  `EphemeralUserMessages`, `ephemeralUserMessages`, `insertBeforeLastUser`,
  `withEphemeralMessages`.
- `internal/llm/agent/agent.go`: `processGeneration` (history built with
  `withEphemeralMessages`), `loadSessionMessagesFromSummary` (re-insertion on reload).
- `internal/agui/input.go`: `ContextDataTag`, `contextDataPreamble`, `contextDataEntry`,
  `ContextDataMessage`; `ContextBlock` documented as the legacy rendering.
- `internal/agui/deps.go`: `Config.ContextDelivery`, `ContextDeliveryMessage`,
  `ContextDeliveryInline`, `resolveContextDelivery`, `ConfigFromApp`.
- `internal/agui/server.go`: `handleRun` chooses inline prefix or data message.
- `internal/config/config.go`: `AGUIConfig.ContextDelivery`.
- `internal/agui/doc.go`, `docs/agui.md`: new "Client context" sections.

## Why
Client context is data produced by the embedding application (page state, records the user
is looking at). Concatenating it with the user's text made it indistinguishable from the
user's own words, so any text inside a context value carried the same authority as the user
(prompt-injection surface), and storing it in the session made stale context reappear on
every later turn and grow the history. A separate, clearly labelled, run-scoped message
fixes both while keeping the data available to the model.

## Design notes
- User role, not system role: every provider converter (`anthropic`, `openai`, `gemini`,
  `copilot`, `bedrock`/`vertexai` via those) handles `message.User`; none forwards a
  `message.System` entry from the history. Consecutive user turns already occur (a
  compaction summary is replayed as a user message) and are accepted by the providers.
- Context value instead of a new `Service.Run` parameter: keeps `agent.Service` and every
  implementation unchanged (agui invariant I1) and makes the feature reusable by any
  surface.
- Default `"message"`: AG-UI clients re-send `context` on every run, so not persisting it
  loses nothing; `"inline"` is kept for clients that relied on the old replay.

## Verification
- New tests: `TestWithEphemeralUserMessagesDropsBlankTexts`,
  `TestInsertBeforeLastUserPlacesDataBeforeTheTurn`,
  `TestEphemeralMessagesReachTheModelButNotTheStore`,
  `TestReloadAfterCompactionKeepsEphemeralMessages` (agent);
  `TestContextDataMessageEnvelope`, `TestContextDataMessageCannotBeClosedByAValue`,
  `TestContextDataMessageEmpty`, `TestResolveContextDelivery`,
  `TestHandleRunSendsContextAsSeparateDataMessage`,
  `TestHandleRunWithoutContextAttachesNoDataMessage`,
  `TestHandleRunInlineContextDeliveryKeepsLegacyPrefix` (agui).
- `go build ./...`, `go vet ./...`, `go test ./internal/agui ./internal/llm/agent`
  (also `-race` on the agui context tests). Full `go test ./...` results are recorded in the
  change description.
