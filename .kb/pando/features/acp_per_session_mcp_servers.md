---
created_at: 2026-09-25T15:01:15.301052776Z
updated_at: 2026-09-25T15:01:15.301052776Z
tags:
    - feature
    - acp
    - mcp
    - xcode
---
# Feature: per-session MCP servers from ACP session/new, session/load, session/resume (PANDO-US-0066)

Date: 2026-09-25. Part of [[pando/plans/acp_xcode_compat.md]] (epic PANDO-EP-0012, P4).

## Problem
ACP clients pass MCP servers in `session/new` / `session/load` (Xcode 27 sends `xcode-tools`: stdio `mcpbridge` + `MCP_XCODE_PID` env; discovery takes ~6s). Pando logged "per-session MCP is not yet supported — ignoring".

## Design
- **agent package** `internal/llm/agent/session_mcp.go`: registry `sessionMCPRegistry` (sync.Map session id -> set) guarded by `sessionMCPMu`.
  - `AttachSessionMCPServers(ctx, sessionID, map[string]config.MCPServer, permission.Service) error`: connect + Start (SSE/HTTP; idempotent) + `mcpclient.Handshake` + ListTools per server; partial success kept, errors joined; replaces previous set (old clients closed).
  - `DetachSessionMCPServers(sessionID)`, `SessionTools(sessionID)`, `sessionMCPToolNames`.
  - ONE persistent client per (session, server) on a registry-owned `context.Background()` (not the request ctx). On a transport error (`isMCPTransportError`: EOF, closed pipe, "transport closed", ...) the client is dropped, reconnected once and the call retried. Detached servers never reconnect.
  - Tools `sessionMCPTool` named `<server>_<tool>` (sanitized to [A-Za-z0-9_-], duplicates suffixed `_2`, capped at 64 chars); same permission request, Lua filters and session-cache interception as global `mcpTool`.
  - Timeouts when the server declares none: discovery 45s, tool call 15min (IDE builds/tests run via tool calls; run ctx still cancels).
  - Factory var `newSessionMCPClient` (tests use mcp-go in-process server).
  - `(*agent).toolsForSession(sessionID)` = `currentTools()` + session tools (session tool replaces same-name global); agents with no tools (title/summarizer) stay tool-less. Used in processGeneration context trimmer, streamAndHandleEvents advertised list + tool lookup, prepareProvider (`createAgentProvider`; fast path skipped when the session has MCP tools so the system prompt names them). `promptMcpCatalogListing` includes session tool names.
  - `mcp-tools.go`: `runTool` split; new `invokeMCPTool` returns the raw CallTool error unreported so the persistent path can retry before `mcpOperationError`.
- **acp package** `internal/mesnada/acp/session_mcp.go`: `SessionMCPServer{Name, Type(stdio|http|sse), Command, Args, Env map, URL, Headers map}`, `ConvertACPMCPServers`, `ToConfig()` (http -> streamable-http, env map -> sorted "K=V"), `SessionMCPServerConfigs`. `attachSessionMCPServers` runs in a goroutine and stores a readiness channel on `ACPServerSession` (`SetMCPReady`/`MCPReady` in session.go); successive attaches of a session are serialized; if the session was closed while connecting, the result is detached. `waitSessionMCPReady` (20s bound, ctx-aware) runs in `Prompt` before the agent run (after slash commands). Detach in `CloseSession` and `ReleaseSessionMCPServers()` deferred in `StdioTransport.Run`.
- `AgentService` interface gained `AttachSessionMCPServers` / `DetachSessionMCPServers`; implemented by `appACPAgentAdapter` (internal/app/app.go) and `acpAgentAdapter` (cmd/root.go) with a new `permissions` field (app Permissions), and by the test mock.
- `NewPandoACPAgent` advertises `McpCapabilities{Http: true, Sse: true}`.

## Tests
- internal/llm/agent/session_mcp_test.go: TestAttachSessionMCPServersExposesToolsToThatSessionOnly, TestToolsForSessionOverridesByName, TestSessionMCPToolUsesPersistentClient (single Initialize across 2 calls, reconnect-once retry, permission denial), TestDetachSessionMCPServersClosesClient, TestAttachSessionMCPServersReplacesAndKeepsPartialSuccess, TestSessionMCPToolNameIsSanitizedAndCapped.
- internal/mesnada/acp/session_mcp_test.go: TestConvertACPMCPServers, TestPandoACPAgent_AdvertisesHTTPAndSSEMCP, TestPandoACPAgent_NewSessionAttachesMCPAndPromptWaits, TestPandoACPAgent_PromptMCPWaitIsBounded.

## Verification
`go build ./...`, `go vet` (agent, acp, app, cmd), `go test ./internal/llm/agent ./internal/mesnada/acp ./internal/api ./internal/app` all green; new tests also pass with `-race`.

## Limitations
- HTTP ACP transport has no per-connection teardown hook; servers there are released only by CloseSession.
- Session tools bypass the MCP gateway/tool_search (always advertised directly).
- Only the last text content of a tool result is returned (same as global MCP tools); `IsError` results not flagged.
