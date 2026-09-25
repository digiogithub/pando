---
id: PANDO-EP-0013
type: epic
title: "Xcode mcpbridge repeated \"Allow pando-gateway\" prompt: sandbox exemption and fewer MCP connections"
status: done
priority: high
author: mcp
labels: [mcp, xcode, sandbox]
created: 2026-09-25T15:26:19Z
updated: 2026-09-25T15:33:03Z
closed: 2026-09-25T15:33:03Z
---

## Description
Xcode shows "Allow 'pando-gateway' to access Xcode? Path: Unknown Path, Not code signed" every few minutes. Cause: Pando runs Xcode's `mcpbridge` inside the seatbelt sandbox (`(allow process-info* (target same-sandbox))` in internal/sandbox/sbpl.go) so mcpbridge cannot resolve the parent agent's path/signature; Xcode cannot remember the approval and re-asks on every new connection. Pando opens many: gateway re-discovery on every process start (Xcode relaunches `pando acp` often), per-call spawn in direct MCP tools, duplicated global `xcode` + per-session `xcode-tools`.
Plan: KB `pando/plans/xcode_mcpbridge_prompt.md`. Shared helpers already added: `config.MCPServer.NoSandbox`, `IsXcodeMCPBridge()`, `SandboxExempt()` (internal/config/mcp_xcode.go).

## Acceptance Criteria
- mcpbridge, NoSandbox servers and ACP-client-provided servers never run sandboxed.
- Startup does not reconnect to a server whose catalog is cached and config unchanged.
- A session with client-provided Xcode bridge does not also use the global mcpbridge server.
- Tests; build/targeted tests green.
