# ACP Implementation Plan for Pando

**Date:** 2026-03-30  
**Objective:** Connect VS Code (and other ACP editors) to Pando as a native ACP agent via stdio.

---

## Problem Diagnosis

**Root cause:** All ACP-compatible editors (VS Code, Zed, JetBrains, avante.nvim) launch the agent as a subprocess and communicate via **stdio (JSON-RPC / ndJSON over stdin/stdout)**. The `runACPServer()` function in `cmd/root.go:377` returns: `"ACP stdio transport not yet implemented"`.

### Current state of ACP implementation in Pando

| File | Status | Description |
|---------|--------|-------------|
| `internal/mesnada/acp/types.go` | ✅ Active | Base types (ACPAgentConfig, ACPSession, etc.) |
| `internal/mesnada/acp/client.go` | ✅ Active | MesnadaACPClient (Pando as ACP client) - Complete |
| `internal/mesnada/acp/client_connection.go` | ✅ Active | Wrapper for client callbacks |
| `internal/mesnada/acp/permissions.go` | ✅ Active | Permissions queue |
| `internal/mesnada/acp/transport_http.go` | ✅ Active | HTTP/SSE transport |
| `internal/mesnada/acp/agent_interface.go` | ✅ Active | ACPAgent interface |
| `internal/mesnada/acp/agent_simple.go` | ✅ Active | SimpleACPAgent (for tests) |
| `internal/mesnada/server/acp_handler.go` | ✅ Active | HTTP handler for Mesnada server |
| `internal/mesnada/acp/transport_stdio.go.disabled` | ❌ Disabled | **Stdio transport — REQUIRED FOR EDITORS** |
| `internal/mesnada/acp/agent_adapter.go.disabled` | ❌ Disabled | agent.Service → AgentService adapter |
| `internal/mesnada/acp/session.go.disabled` | ❌ Disabled | ACPServerSession type |
| `internal/mesnada/acp/server_fase3.go.disabled` | ❌ Disabled | PandoACPAgent (real agent, partially implemented) |
| `cmd/root.go:runACPServer()` | ❌ Stub | Returns error, implementation commented out |

### Comparison with opencode (TypeScript)

opencode implements ACP via `@agentclientprotocol/sdk` with:
- `AgentSideConnection` + `ndJsonStream` over stdin/stdout
- `ACP.Agent` class with all protocol methods
- Event subscription for real-time streaming
- `loadSession`, `listSessions`, `forkSession`, `resumeSession`
- `SetSessionModel`, `SetSessionMode`
- `writeTextFile` on the client when approving edit permissions
- Client MCP servers passed to opencode
- Usage updates with tokens and cost

---

## Implementation Phases

### Phase 1: Enable Stdio Transport (ROOT CAUSE)
**Fact:** `acp_plan_phase1_stdio_transport`  
**Priority:** CRITICAL — without this no editor can connect

- Rename `transport_stdio.go.disabled` → `transport_stdio.go`
- Rename `agent_adapter.go.disabled` → `agent_adapter.go`
- Rename `session.go.disabled` → `session.go`
- Rename `server_fase3.go.disabled` → `server_fase3.go`
- Implement `runACPServer()` in `cmd/root.go` using the commented implementation
- Inject real dependencies: `agent.Service`, `session.Service`, `message.Service`

**Result:** `pando acp` works and accepts editor connections

---

### Phase 2: PandoACPAgent Core
**Fact:** `acp_plan_phase2_pandoacpagent_core`  
**Priority:** HIGH

- Complete `Initialize()`: v1 protocol, basic capabilities
- Complete `NewSession()`: create real Pando session, map IDs
- Complete `Prompt()`: call `agentService.Run()`, blocking response
- Complete `Cancel()`: cancel agent + session context
- Stub `SetSessionMode()`: save mode, apply in Phase 5

**Result:** Can send a prompt and receive a response (without streaming)

---

### Phase 3: Event Subscription and Streaming
**Fact:** `acp_plan_phase3_event_streaming`  
**Priority:** HIGH — without streaming UX is poor

- Modify `Prompt()` to send updates while the agent runs
- Map Pando events → ACP session updates:
  - Text → `agent_message_chunk`
  - Tools → `tool_call` / `tool_call_update`
  - TodoWrite → `plan` entries
  - Reasoning → `agent_thought_chunk`
- Maintain `AgentSideConnection` reference in `ACPServerSession`
- Map Pando tool names → ACP `ToolKind`

**Result:** Real-time streaming to editor (text + tool calls + plan)

---

### Phase 4: Extended Session Capabilities
**Fact:** `acp_plan_phase4_session_capabilities`  
**Priority:** MEDIUM

- Enable `LoadSession: true` in capabilities
- Implement `LoadSession()`: load existing Pando session
- Implement `unstable_listSessions()`: list sessions
- Implement `unstable_forkSession()`: fork session
- Implement `unstable_resumeSession()`: resume session
- Add `model`, `modeId`, `variant` fields to `ACPServerSession`
- Verify/implement `sessionService.Fork()` in Pando

**Result:** Clients can load history, list and fork sessions

---

### Phase 5: Model and Mode Selection
**Fact:** `acp_plan_phase5_model_mode_selection`  
**Priority:** MEDIUM

- Complete `SetSessionMode()`: apply code/ask/architect mode in `agentService.Run()`
- Implement `SetSessionModel()`: change model per session (providerID/modelID)
- Return `availableModels` and `availableModes` in `NewSession`/`LoadSession` via `_meta`
- Pass model override to `agentService.Run()` for each session

**Result:** Editor can change model and mode from its UI

---

### Phase 6: Advanced Capabilities
**Fact:** `acp_plan_phase6_advanced_capabilities`  
**Priority:** LOW-MEDIUM

- `writeTextFile` callback: write diffs on client when approving edits
- Client MCP servers: register in Pando per session
- Usage updates: send tokens and cost after each prompt
- Image support in prompts (`PromptCapabilities.Image = true`)
- Embedded context (`EmbeddedContext = true`)
- Enable `McpCapabilities{Http: true, Sse: true}`

---

### Phase 7: Config, Docs and Tests
**Fact:** `acp_plan_phase7_config_docs_testing`  
**Priority:** MEDIUM (after Phase 1-3)

- `[acp]` section in `.pando.toml`
- Improve flags in `pando acp` (--cwd, --debug, --auto-permission)
- Document configuration for VS Code, Zed, JetBrains, avante.nvim
- Complete integration tests in `test/e2e/acp_integration_test.go`
- Enable `.github/workflows/acp-test.yml`

---

## Priority order for functional MVP

1. **Phase 1** → without this nothing works
2. **Phase 2** → without this there is no prompt
3. **Phase 3** → without this poor UX (no streaming)
4. **Phase 7 (partial)** → basic tests and docs
5. Phases 4, 5, 6 → incremental improvements

## Key files

- `cmd/root.go:375-410` — `runACPServer()` to unlock
- `internal/mesnada/acp/server_fase3.go.disabled` — `PandoACPAgent` to complete
- `internal/mesnada/acp/transport_stdio.go.disabled` — transport to enable
- `internal/mesnada/acp/session.go.disabled` — ACP server session
- `internal/mesnada/acp/agent_adapter.go.disabled` — agent.Service adapter
- `internal/llm/agent/agent.go` — `agent.Service` to inject
