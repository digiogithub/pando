---
id: PANDO-US-0066
type: story
title: P4 Per-session MCP servers from ACP session/new and session/load (xcode-tools)
status: done
parent: PANDO-EP-0012
author: mcp
labels: [acp, mcp]
created: 2026-09-25T14:52:19Z
updated: 2026-09-25T15:05:41Z
started: 2026-09-25T14:53:28Z
closed: 2026-09-25T15:05:41Z
---

## Description
Xcode passes `xcode-tools` (stdio `mcpbridge` + MCP_XCODE_PID env). Pando ignores client MCP servers. Implement persistent per-session MCP clients whose tools are visible only to that session.

## Acceptance Criteria
- acpsdk.McpServer (stdio/http/sse) converted and attached async on NewSession/LoadSession; Prompt waits bounded for readiness; detach on CloseSession/transport stop.
- McpCapabilities http+sse advertised.
- agent: session tool registry, toolsForSession used for advertised list, trimmer, lookup, provider build.
- Tests with an in-process/fake MCP server.
