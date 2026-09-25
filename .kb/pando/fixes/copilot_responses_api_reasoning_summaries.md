---
created_at: 2026-09-25T15:01:38.153089825Z
updated_at: 2026-09-25T15:01:38.153089825Z
tags:
    - fix
    - copilot
    - provider
    - acp
    - reasoning
    - responses-api
---
# Fix: Copilot Responses API reasoning summaries + assistant text history fidelity

Date: 2026-09-25
Story: PANDO-US-0065 (P2+P3 of epic PANDO-EP-0012, see [[pando/plans/acp_xcode_compat.md]])

## Problem

In `internal/llm/provider/copilot.go`, the OpenAI Responses API path (used for
GPT-5+ Copilot models via `isResponsesAPIModel()`, e.g. `copilot.gpt-6-sol`):

1. `sendWithResponsesAPI` / `streamWithResponsesAPI` (~809/~894) never set a
   `Reasoning` request parameter, so `c.options.reasoningEffort` was ignored
   and no reasoning summary was ever requested. ACP clients (Xcode) therefore
   never received `agent_thought_chunk` events for these models.
2. `streamWithResponsesAPI`'s SSE loop only handled `response.output_text.delta`
   and `response.completed`; any reasoning-summary stream events were silently
   dropped.
3. `convertMessagesToResponsesInput` (~700) emitted only `function_call` items
   for an assistant message that also had tool calls, and completely dropped
   the assistant's text content in that case — multi-tool-call turns lost the
   model's prose from conversation history on every subsequent request.

## Important finding: vendored openai-go SDK mismatch

The task briefing assumed `shared.ReasoningParam{Effort, Summary: shared.ReasoningSummaryAuto}`
(the field name used by current OpenAI Responses API docs). The vendored SDK
actually pinned in this repo (`github.com/openai/openai-go@v0.1.0-beta.2`) is
older and does **not** have a `Summary` field at all. Its `shared.ReasoningParam`
only has:
- `Effort ReasoningEffort` (`json:"effort"`)
- `GenerateSummary ReasoningGenerateSummary` (`json:"generate_summary"`, values
  `"concise"` or `"detailed"` — no `"auto"`)

Similarly, `responses.ResponseStreamEventUnion` in this SDK version has **no**
typed variant for `response.reasoning_summary_text.delta`,
`response.reasoning_summary_part.added`, or `response.reasoning_text.delta` —
its `AsAny()`/documented type list does not include them. However, the union
struct is decoded generically via `apijson.UnmarshalRoot`, and it has a shared
`Delta string \`json:"delta"\`` and `Type string \`json:"type"\`` field used by
every variant, so events with those unlisted "type" values still decode
correctly and `event.Delta`/`event.Type` are populated as expected — they just
have no dedicated `.As...()` accessor. Confirmed via
`ssestream.Stream[T].Next()`: it takes the direct-unmarshal branch whenever
`Event().Type == "" || strings.HasPrefix(Event().Type, "response.")`, which
covers these reasoning event names.

## Changes (internal/llm/provider/copilot.go)

- New `responsesReasoningParam()` helper: returns
  `shared.ReasoningParam{Effort: ..., GenerateSummary: shared.ReasoningGenerateSummaryDetailed}`
  and `ok=true` when `c.providerOptions.model.SupportsReasoningEffort`, else
  `ok=false` (no Reasoning param sent at all). Used by both
  `sendWithResponsesAPI` and `streamWithResponsesAPI` to keep them in sync.
- New `isReasoningSummaryRejection(err) bool`: detects a 400 from Copilot whose
  body mentions both "reasoning" and "summary" (covers `generate_summary`
  too, since it contains the substring "summary"). Both request paths retry
  once with `params.Reasoning` cleared when this triggers, rather than failing
  the whole request — defensive fallback for the "be defensive" requirement.
- `streamWithResponsesAPI`: SSE switch now also handles
  `response.reasoning_summary_part.added` (inserts a `"\n\n"` `EventThinkingDelta`
  separator before every part after the first) and
  `response.reasoning_summary_text.delta` / `response.reasoning_text.delta`
  (each non-empty `event.Delta` → `ProviderEvent{Type: EventThinkingDelta}`),
  in addition to the existing `response.output_text.delta` / `response.completed`.
- `convertMessagesToResponsesInput`: assistant branch now always emits the
  text `output_message` item first (when `msg.Content().String() != ""`),
  then iterates `msg.ToolCalls()` unconditionally to emit `function_call`
  items — text and tool calls are no longer mutually exclusive.
- `sendWithResponsesAPI` (non-streaming): **not** changed to surface reasoning
  summary text into `ProviderResponse`, because `ProviderResponse` (in
  `provider.go`, out of this task's file ownership) has no `Thinking`/reasoning
  field. The Responses API does return `resp.Output` items of type
  `"reasoning"` with a `Summary []ResponseReasoningItemSummary`, but there is
  currently nowhere in `ProviderResponse` to put that text without changing
  `provider.go`. Left as a known gap — the primary goal (Xcode receiving live
  `agent_thought_chunk`) is met via the streaming path, which is what ACP uses
  for interactive turns.

## Tests (internal/llm/provider/copilot_responses_test.go, new file)

- `TestConvertMessagesToResponsesInputKeepsAssistantTextAndFunctionCallInOrder`
  and `TestConvertMessagesToResponsesInputMultipleToolCallsAfterText`: assert
  the text `output_message` item comes first, followed by all `function_call`
  items, for an assistant message carrying both.
- `TestSendWithResponsesAPISetsReasoningWhenSupported`: httptest server
  captures the POST body; asserts `reasoning.effort == "high"` and
  `reasoning.generate_summary` present when `SupportsReasoningEffort == true`.
- `TestSendWithResponsesAPIOmitsReasoningWhenUnsupported`: asserts no
  `reasoning` key in the body when `SupportsReasoningEffort == false`.
- `TestStreamWithResponsesAPIReasoningSummaryBeforeContent`: SSE fixture with
  two reasoning-summary parts + a content delta + `response.completed`;
  asserts `EventThinkingDelta` arrives before `EventContentDelta`, the
  `"\n\n"` separator appears only between the two parts (not before the
  first), and `EventComplete.Response.Content == "Hello"`.
- `TestStreamWithResponsesAPISingleReasoningPartHasNoSeparator`: single
  reasoning part → no leading/spurious separator.
- Test client: `newTestCopilotClient` builds a `*copilotClient` directly
  (bypassing `newCopilotClient`'s network credential/model-API checks) with
  `sourceToken` left empty so `refreshBearerToken()` short-circuits via
  `auth.IsCopilotAPIToken` returning false — no real network calls in tests.

## Verification

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./internal/llm/provider ./internal/api` — all pass (new tests:
  6/6 pass).
- `go test ./internal/llm/agent ./internal/api` (repo's verified command) —
  pass.
- `gofmt -l` clean on both touched files.

## Scope note

Per task assignment, only `internal/llm/provider/copilot.go` and the new
`internal/llm/provider/copilot_responses_test.go` were touched — no changes
to `provider.go`, `internal/mesnada/acp/*`, `internal/llm/agent/*`, or
`internal/app/app.go`, which other concurrent agents own for the rest of
epic PANDO-EP-0012.
