---
created_at: 2026-10-29T15:56:44.940279133Z
updated_at: 2026-10-29T15:56:44.940279133Z
tags:
    - investigation
    - copilot
    - max_tokens
    - betterstack
---
# Sessions "freeze" while saving a plan: coder output capped at 8192 tokens (2026-10-29)

Reporter: debug_id 5197-1123-6710-9997 (Nazareth, Pando Desktop v1.5.0/v1.5.1, Copilot provider, claude-opus-5.5 / claude-sonnet-5.5). See [[reference-nazareth-debug-id]].

## Evidence (Better Stack source 2751484)
- 17 occurrences 2026-10-28 16:28 to 2026-10-29 14:07, always the same sequence:
  `Dropping tool call truncated by max_tokens {tool: kb_add_document, input_len: 109-173}` then
  `Copilot stream completed {finishReason: max_tokens, toolCallCount: 0}` then `Result max_tokens` and `Request completed`.
- Example 13:58:34 session c3a158ae: `Event: Complete inputTokens=164091 outputTokens=8192`. 8192 = coder Auto budget.
- Resume of the session regenerates the same big plan, hits the same cap: loop.

## Root cause
- `config.AutoBudgetByRole(AgentCoder)` = 8192, `roleCeilingTokens(AgentCoder)` = 16384 (`internal/config/config.go`). A plan written via `kb_add_document`/`write` larger than ~8k tokens cannot fit.
- `provider.dropTruncatedToolCalls` (`internal/llm/provider/tool_args.go`) correctly discards the cut call, but the agent has no handling for `FinishReasonMaxTokens`: turn ends silently, UI looks frozen.
- Note: input_len of the dropped call is only ~150 bytes although 8192 tokens were generated; with Anthropic models over Copilot, the Anthropic monkeypatch in `copilot.go` only copies its own assembled tool calls into the accumulator on `finish_reason == "tool_calls"`, so on `length` the openai accumulator's (interleaved-delta) view is used. Irrelevant for the outcome (args are truncated anyway) but logged sizes are misleading.

## VS Code Copilot extension comparison (project vscode_copilot_extension)
- `chatMLFetcher.ts`: `max_tokens = chatEndpoint.maxOutputTokens`, which is `capabilities.limits.max_output_tokens` from /models (`chatEndpoint.ts:247`, default 4096). Claude models go through `messagesApi.ts` with that `max_tokens`. So the extension uses the full model output limit, not 8k.

## Fix options (not implemented yet)
1. Coder Auto budget = model's reported max output (Copilot `limits.max_output_tokens`), with a sane cap (e.g. 32k), raise coder ceiling.
2. Agent: on `max_tokens` with a dropped tool call, append a synthetic note and continue (bounded) telling the model to split the write; surface a visible notice instead of silent stop.
3. Workaround now: `[Agents.coder] MaxTokens = 16384` (ceiling clamps higher values).

## Unrelated noise seen
MCP jira server `npm 404 map-remote@*`; figma MCP needs auth; one `read tcp ... can't assign requested address` to api.githubcopilot.com (12:27).
