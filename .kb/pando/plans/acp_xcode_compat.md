---
created_at: 2026-09-25T14:52:07.313012352Z
updated_at: 2026-09-25T14:52:07.313012352Z
tags:
    - plan
    - acp
    - xcode
    - mcp
---
# Plan: ACP compatibility with Xcode 27 (message ids, reasoning, per-session MCP)

Status: IN PROGRESS (2026-09-25). Tracked in gintrack epic (PANDO project, "ACP Xcode compatibility").

## Evidence (from a colleague's Xcode 27.0 log, pando v1.0.3)
- The LLM *does* get full history (existingMessages 0→2→8→10). The "lost thread" is a render issue in Xcode.
- Live `agent_message_chunk` carries NO `messageId`; only session/load replay does (msg.ID).
- Live prompt echoes `user_message_chunk` with a CONSTANT id `<sessionID>-user` and the full concatenated prompt (Xcode injects `<system-reminder>` + "Project structure" blocks, ~6.5K chars) every turn.
- Xcode showed answer1+answer2+answer3 merged in turn 3 bubble → client groups by messageId.
- No `agent_thought_chunk` at all: Copilot `/responses` path (gpt-6-sol) never sets `Reasoning{Effort,Summary}` and drops reasoning summary stream events.
- `convertMessagesToResponsesInput` drops assistant text when the message also has tool calls.
- Xcode passes `mcpServers: [{name:"xcode-tools", command:".../mcpbridge", args:[], env:[{MCP_XCODE_PID}]}]` in session/new & session/load; Pando ignores them ("per-session MCP is not yet supported").
- Xcode sends `session/cancel` ~1.5s before each new prompt (idle cancel) — out of scope here but note CancelGoal side-effect.

## Phases
### P1 — ACP message ids (internal/mesnada/acp/prompt_handler.go)
- Stop echoing the user prompt as `user_message_chunk` during live `session/prompt` (client already renders it). Keep replay on session/load.
- Every live agent_message_chunk / agent_thought_chunk carries a stable per-assistant-message id, available from the FIRST delta (not only after AgentEventTypeResponse). A new assistant message (after tool results) gets a new id. Must equal the persisted message.ID so load-replay ids match live ids.
- Tests: new ids on first delta; distinct ids across turns / after tool round-trip; no user echo.

### P2 — Copilot Responses API reasoning (internal/llm/provider/copilot.go)
- When model SupportsReasoningEffort: `params.Reasoning = shared.ReasoningParam{Effort: effort, Summary: "auto"}` for both send and stream paths.
- Stream: map `response.reasoning_summary_text.delta` (and `response.reasoning_text.delta` if present) → `EventThinkingDelta`; insert paragraph break between summary parts.
- Tests with httptest SSE server.

### P3 — Responses history fidelity (copilot.go convertMessagesToResponsesInput)
- Assistant message with text + tool calls → emit output_message (text) THEN function_call items. Test.

### P4 — Per-session MCP servers (ACP → agent)
- acp: `SessionMCPServer{Name, Type(stdio|http|sse), Command, Args, Env, URL, Headers}` converted from acpsdk.McpServer (Stdio/Http/Sse). AgentService gets `AttachSessionMCPServers(ctx, sessionID, servers) error` and `DetachSessionMCPServers(sessionID)`.
- NewSession/LoadSession: attach asynchronously (mcpbridge discovery ~6s); Prompt waits for readiness (bounded, e.g. 20s) before running. CloseSession / transport stop → detach.
- Advertise `McpCapabilities{Http:true, Sse:true}` (mcpclient supports both).
- agent package: `session_mcp.go` — persistent mcpclient per (session, server): Initialize once, ListTools, wrap tools as `sessionMcpTool` (name `<server>_<tool>`, same permission + Lua filters + cache interception as mcpTool, reconnect once on transport error). Registry sessionID→tools.
- agent: `toolsForSession(sessionID)` = global tools + session tools (session wins on name clash); used for advertised list, context trimmer, tool lookup, createAgentProvider.
- app.go adapter wires to agent package.
- Tests: fake stdio MCP server (go test helper binary / mcp-go server in-process) → tools visible only for that session; detach closes client.

### P5 — Verify & document
- `go build ./... && go test ./internal/llm/agent ./internal/api ./internal/mesnada/acp ./internal/llm/provider`.
- KB doc pando/fixes/acp_xcode_compat.md.
