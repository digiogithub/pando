---
created_at: 2026-09-25T15:26:19.016386176Z
updated_at: 2026-09-25T15:26:19.016386176Z
tags:
    - plan
    - mcp
    - xcode
    - sandbox
---
# Plan: stop Xcode's repeated "Allow pando-gateway to access Xcode?" prompt

Status: IN PROGRESS 2026-09-25. Follows [[pando/fixes/acp_xcode_compat.md]].

## Root cause
- Dialog name `pando-gateway` = clientInfo sent by internal/mcpgateway (registry.go discovery, clientpool.go) to the global MCP server `xcode` (Xcode's `mcpbridge`).
- Log: `sandbox.applied backend=seatbelt purpose=mcp` right before discovering `xcode`. SBPL `(allow process-info* (target same-sandbox))` (internal/sandbox/sbpl.go:141) prevents sandboxed mcpbridge from reading the parent pando's path/code signature → Xcode shows "Unknown Path / Not code signed" and can't persist Allow.
- Every new mcpbridge connection re-prompts: gateway DiscoverAll on every process start (app.go gw.Initialize), Xcode relaunches `pando acp` frequently, direct mcpTool spawns a client per call, per-session `xcode-tools` duplicates the global `xcode`.

## Phase 1 — sandbox exemption (story A)
- Shared (done by lead): `config.MCPServer.NoSandbox` (toml NoSandbox), `IsXcodeMCPBridge()` (command basename mcpbridge, or xcrun + first non-flag arg mcpbridge), `SandboxExempt()`.
- mcpclient.New / newSandboxedCommandFunc: exempt servers never wrapped (even if ExtendTo covers mcp); explicit Sandbox=true + exempt → NoSandbox wins? (mcpbridge: exempt always; NoSandbox && Sandbox both set → log warn, NoSandbox wins).
- ACP session servers (internal/mesnada/acp/session_mcp.go ToConfig) set NoSandbox=true (the IDE client is trusted, its servers commonly need IDE IPC).
- Tests.

## Phase 2 — fewer connections (story B)
- Gateway startup: skip DiscoverServer when the registry already has tools for the server and a stored config fingerprint matches; otherwise discover and store fingerprint. Explicit RefreshServer always rediscovers.
- Session dedupe: when a session has an attached server that IsXcodeMCPBridge, hide global tools of servers that IsXcodeMCPBridge in toolsForSession (direct tools) and in gateway catalog/proxy for that session if feasible.
- Direct mcpTool per-call spawn for mcpbridge: reuse a persistent pooled client (at least for IsXcodeMCPBridge servers).
