---
id: PANDO-US-0067
type: story
title: "A: Never sandbox Xcode mcpbridge, NoSandbox servers or ACP-client MCP servers"
status: done
parent: PANDO-EP-0013
author: mcp
labels: [sandbox, mcp]
created: 2026-09-25T15:26:27Z
updated: 2026-09-25T15:31:44Z
started: 2026-09-25T15:26:27Z
closed: 2026-09-25T15:31:44Z
---

## Acceptance Criteria
- mcpclient stdio spawn skips the sandbox when `srv.SandboxExempt()`, even if ExtendTo covers mcp; NoSandbox beats Sandbox=true (warn).
- ACP `SessionMCPServer.ToConfig()` sets NoSandbox=true for stdio.
- Tests for IsXcodeMCPBridge, exemption, ToConfig.
