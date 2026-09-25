---
created_at: 2026-09-25T15:05:30.743189118Z
updated_at: 2026-09-25T15:05:30.743189118Z
tags:
    - fix
    - acp
    - xcode
    - mcp
    - copilot
---
# Fix: ACP compatibility with Xcode 27 (EP-0012) — COMPLETE 2026-09-25

Plan: [[pando/plans/acp_xcode_compat.md]]. gintrack: PANDO-EP-0012 (US-0064, US-0065, US-0066).
Part docs: [[pando/fixes/acp_xcode_message_ids.md]], [[pando/fixes/copilot_responses_api_reasoning_summaries.md]], [[pando/features/acp_per_session_mcp_servers.md]].

## Symptom
Xcode 27 native ACP client: Pando answers merged across turns (looked like lost context), no "Thinking" bubble, Xcode's `xcode-tools` MCP ignored. The LLM history itself was intact.

## Changes
1. **Message ids (US-0064)** — `AgentEvent.MessageID` (agent + acp mirror) set in `processEvent` for thinking/content/tool events; app adapter copies it; `processAgentEventStream` adopts it from the first delta. Live `user_message_chunk` echo with constant `<session>-user` id removed (replay on session/load unchanged). Live ids == persisted message ids == replay ids.
2. **Copilot Responses reasoning (US-0065)** — `responsesReasoningParam` sends `reasoning.effort`; `responsesReasoningSummaryOpts` adds `reasoning.summary:"auto"` via `option.WithJSONSet` (vendored openai-go v0.1.0-beta.2 only has deprecated `generate_summary`, deliberately NOT sent). Stream maps `response.reasoning_summary_text.delta` / `response.reasoning_text.delta` → `EventThinkingDelta`, "\n\n" between summary parts. One retry without Reasoning on 400 mentioning reasoning+summary. `convertMessagesToResponsesInput` now emits assistant text before its function_call items. Gap: non-streaming send does not surface reasoning (ProviderResponse has no thinking field).
3. **Per-session MCP (US-0066)** — `internal/llm/agent/session_mcp.go` (persistent client per session+server, reconnect-once, `<server>_<tool>` names, `toolsForSession` at trimmer/advertised list/lookup/provider build), `internal/mesnada/acp/session_mcp.go` (convert acpsdk.McpServer stdio/http/sse, async attach on NewSession/LoadSession/ResumeSession, Prompt waits ≤20s, detach on CloseSession + stdio transport stop). `McpCapabilities{Http,Sse}=true`. Implemented in `appACPAgentAdapter` (app.go) and `acpAgentAdapter` (cmd/root.go). Default per-call timeout 15 min (Xcode builds/tests via tools), discovery 45s.

## Verification
`go build ./...`, `go vet` on touched packages, `go test -count=1 ./internal/llm/agent ./internal/mesnada/acp ./internal/api ./internal/app ./internal/llm/provider ./internal/mcpclient ./internal/mcpgateway` all ok. New tests pass with `-race`. Pre-existing race in `internal/llm/agent` package under -race (goroutine leaked by TestRunResetsResurrectionCount reading config vs later `config.SetForTests`) reproduces without the new tests — not introduced here.
Not verified live against Xcode (no local Xcode).

## Notes / follow-ups
- A user with a global `xcode` MCP server configured now also gets `xcode-tools_*` per session → duplicated tools.
- Xcode sends `session/cancel` before every prompt; Pando's Cancel also cancels the goal (/goal won't survive in Xcode).
- HTTP ACP transport releases session MCP only on CloseSession.
