---
created_at: 2026-09-25T15:33:03.716807871Z
updated_at: 2026-09-25T15:33:03.716807871Z
tags:
    - fix
    - mcp
    - xcode
    - sandbox
    - gateway
---
# Fix: Xcode repeated "Allow 'pando-gateway' to access Xcode?" (EP-0013) — COMPLETE 2026-09-25

Plan: [[pando/plans/xcode_mcpbridge_prompt.md]]. Follows [[pando/fixes/acp_xcode_compat.md]]. gintrack PANDO-EP-0013 (US-0067, US-0068).

## Root cause
Pando ran Xcode's `mcpbridge` inside the seatbelt sandbox (Sandbox.ExtendTo covers mcp). SBPL `(allow process-info* (target same-sandbox))` hides the parent pando process → Xcode shows "Unknown Path / Not code signed" and cannot remember Allow → re-prompts on every new mcpbridge connection. Connections were frequent: gateway re-discovery on every process start (Xcode relaunches `pando acp`), direct mcpTool client-per-call, duplicated global `xcode` + per-session `xcode-tools`.

## Changes
- **Config** (`internal/config/config.go`, new `mcp_xcode.go`): `MCPServer.NoSandbox` (toml `NoSandbox`), `IsXcodeMCPBridge()` (basename mcpbridge, or `xcrun [--sdk X] mcpbridge`), `SandboxExempt()`.
- **US-0067 sandbox exemption**: `mcpclient.newSandboxedCommandFunc(serverName, config.MCPServer)` never wraps exempt servers (NoSandbox beats Sandbox=true with a Warn; Info log when mcpbridge auto-detected). ACP `SessionMCPServer.ToConfig()` sets NoSandbox for stdio (IDE-provided servers trusted).
- **US-0068 fewer connections**:
  - Migration `20260925000001_add_mcp_server_fingerprints.sql` (`mcp_server_fingerprints`). `mcpgateway/fingerprint.go` `ConfigFingerprint` (raw Type/Command/Args/Env/URL/Headers/Auth). `DiscoverAll` skips servers with cached catalog + same fingerprint; `RefreshServer`/`DiscoverServer` always rediscover; `DeleteServer` clears fingerprint. Removed tools are not pruned on config change (keeps usage stats).
  - Session dedupe: `toolsForSession` drops global mcpbridge `mcpTool`s when the session has its own bridge; `mcpTool.Run` refuses them; gateway `session_exclusion.go` hook filters catalog (`listCatalogExcluding`) and `Gateway.CallTool` refuses excluded servers.
  - `internal/llm/agent/mcp_bridge_client.go`: one persistent process-wide client per global bridge (name+fingerprint), also used for direct-mode discovery; closed by `ResetMcpToolsCache`. Bridge calls default 15 min timeout.

## Verification
`go build ./...`; vet on touched packages; `go test -count=1` mcpgateway, llm/agent, app, api, db, mcpclient, config, mesnada/acp, sandbox, llm/provider all ok; new tests pass with -race. Not verified on a Mac with Xcode.

## Gaps
- Direct-mode discovery not cached across processes (1 bridge connection per process start, then reused).
- Global bridge still discovered before any session (first run / config change in gateway mode; every start in direct mode).
- tool_search may still list global bridge entries in a bridge session (calls refused).
- Gateway pool and direct persistent client may each hold one connection.
- Workaround for users: remove the global `xcode` MCP server (Xcode passes `xcode-tools` per session).
