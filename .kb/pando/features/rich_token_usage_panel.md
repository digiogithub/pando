---
created_at: 2026-07-16T19:17:09.176831518Z
updated_at: 2026-07-16T20:09:26.280695273Z
tags:
    - pando
    - feature
    - tokens
    - reasoning
    - provider
    - tui
    - webui
---
# Feature: richer token-usage panel (TUI sidebar + WebUI status bar)

P1 of [[pando/plans/token_panel_context_window_plan]] (P0 was the Copilot
context-window fetch fix, see [[pando/fixes/copilot_context_window_fetch]]).

## What changed

Session-level token usage now carries a cache read/write and reasoning-token
breakdown plus cost, end to end: provider SDK response → DB → session
service → agent event bus → TUI sidebar / WebUI status bar, plus the WebUI
SSE stream.

### Provider layer (reasoning tokens — real per-provider wiring, not guessed)
Investigated how opencode sources reasoning tokens per provider
(`packages/llm/src/protocols/*.ts` in the opencode repo) and cross-checked
against pando's own provider SDKs. Wired `TokenUsage.ReasoningTokens` only
where the provider's raw API response actually exposes it — left at 0
(zero value, not fabricated) everywhere it doesn't:

- `internal/llm/provider/openai.go` `usage()` (line ~546) —
  `completion.Usage.CompletionTokensDetails.ReasoningTokens` (OpenAI Chat
  Completions SDK type already has this field).
- `internal/llm/provider/gemini.go` `usage()` (line ~535) —
  `int64(resp.UsageMetadata.ThoughtsTokenCount)`. `vertexai.go` reuses
  `geminiClient.usage()` directly, so VertexAI inherits this for free.
- `internal/llm/provider/copilot.go` — three call sites, all OpenAI-shaped
  since Copilot is OpenAI-API-compatible:
  - `usage()` (Chat Completions path, line ~994) —
    `completion.Usage.CompletionTokensDetails.ReasoningTokens`.
  - `sendWithResponsesAPI` (line ~795) —
    `resp.Usage.OutputTokensDetails.ReasoningTokens`.
  - `streamWithResponsesAPI` (line ~870) —
    `completedResp.Usage.OutputTokensDetails.ReasoningTokens`.
- `internal/llm/provider/anthropic.go` `usage()` (line ~744) — **left
  unchanged, no reasoning field added**. Confirmed both in opencode's own
  Anthropic protocol handler (explicit comment: "Extended thinking tokens
  are not broken out by Anthropic — billed as part of output_tokens") and
  in the `anthropic-sdk-go` v1.4.0 `Usage` struct (no reasoning/thinking
  field exists). `azure.go` and `bedrock.go` wrap `openaiClient` /
  `anthropicClient` respectively with no own usage code, so Azure inherits
  the OpenAI fix and Bedrock inherits Anthropic's "always 0" (Bedrock here
  is one of Anthropic's own backends).

Net effect: `ReasoningTokens` is now non-zero for real on any
reasoning-capable OpenAI/Gemini/Copilot/Azure model; it stays exactly 0 for
Anthropic/Bedrock, which is correct/real (those APIs fold thinking tokens
into `output_tokens` and never report them separately) rather than a fake
placeholder value.

### DB / persistence, agent/event layer, TUI, WebUI
Unchanged from the original P1 pass — see the rest of this document's
history / [[pando/plans/token_panel_context_window_plan]] for the full
plumbing chain (migration `20260716191017_add_session_token_usage_detail.sql`,
`session.Session`, `TrackUsage`, `TokenUsageInfo`, TUI `sidebarCmp.usageSection()`,
WebUI `StatusBar.tsx` cost badge, SSE `token_usage` payload, etc.). That
plumbing already threaded `provider.TokenUsage.ReasoningTokens` through
end-to-end; only the provider-layer source of the value itself was pending,
and is now real per provider instead of always-zero.

## Verification
- `go build ./...` — clean.
- `go test ./internal/llm/provider/... ./internal/llm/agent ./internal/api ./internal/llm/models ./internal/session ./internal/db/...`
  — all pass.
- No manual live-account test with a reasoning-capable model in this
  session (still a manual follow-up, same caveat as the rest of P1).
