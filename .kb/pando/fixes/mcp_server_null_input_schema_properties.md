---
created_at: 2026-10-27T14:56:30.201597723Z
updated_at: 2026-10-27T14:56:30.201597723Z
tags:
    - fix
    - mcp
    - mesnada
---
# Fix: re-exported MCP tools emitted `"inputSchema.properties": null`

## Date
2026-10-27

## Problem
`pando mcp-server` re-exports tools of connected MCP servers. A parameterless upstream tool (Firebase MCP `firebase_get_project`, schema `{"type":"object"}` with no `properties`) was emitted as `{"type":"object","properties":null}`. Strict clients (Claude Code) validate the whole `tools/list` result and rejected it ("tools.57.inputSchema.properties: Invalid input: expected record, received null"), so ALL Pando tools were lost. VS Code/Copilot tolerated it. Report: user download `pando-bug-report-null-input-schema-properties.md` (Pando v1.3.3, Windows).

## Root cause
`mcp.Tool.InputSchema.Properties` stays nil when the upstream omits the key. `mcpTool.Info()` (`internal/llm/agent/mcp-tools.go`) and `sessionMCPTool.Info()` (`internal/llm/agent/session_mcp.go`) copied it into `ToolInfo.Parameters`; `internal/mesnada/server/tools.go` `getToolDefinitions` put it under `"properties"` unchanged.

## Fix
- `internal/mesnada/server/tools.go`: new `toolInputSchema(info)` used by both native tools and remembrances tools; nil Parameters -> `map[string]any{}` (central guard for every emitted tool).
- `mcp-tools.go` / `session_mcp.go`: `Info()` normalises nil Properties to an empty map at the source (also protects LLM provider tool schemas).
- Test: `TestGetToolDefinitionsNilParametersEmitEmptyProperties` in `internal/mesnada/server/server_pando_tools_test.go` asserts JSON `{"type":"object","properties":{}}`.

## Verification
`go test ./internal/mesnada/server/ ./internal/llm/agent/` pass.
