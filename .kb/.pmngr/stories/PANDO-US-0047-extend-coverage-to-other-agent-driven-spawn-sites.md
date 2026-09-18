---
id: PANDO-US-0047
type: story
title: Extend coverage to other agent-driven spawn sites
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:28Z
updated: 2026-09-18T08:36:28Z
---

## Description

**As a** security-conscious user **I want** commands the agent reaches indirectly (sub-agent terminals, skill tools, optionally MCP and sub-agent CLIs) sandboxed too **so that** the protection cannot be bypassed through another tool.

### Implementation

- `sandbox.WrapCmd(cmd, purpose)` applied at:
  - `internal/mesnada/acp/client.go:401` (ACP terminals Pando serves, default on)
  - `internal/skills/tool_bridge.go:111` (default on)
  - `internal/mcpclient/client.go:65` (rewrite `resolved.Command`/`Args` when `MCPServer.Sandbox` or `ExtendTo` contains "mcp"; default off)
  - `internal/mesnada/agent/spawner*.go` (opt-in `ExtendTo:"subagents"`, whose policy adds `~/.claude`, `~/.copilot` and similar as writable)
- Lua: a P2 follow-up to wrap the `sh`/`cmd` modules (`internal/luaengine/lua.go`). For now, document it as unsandboxed.
- Never wrap user terminals (`internal/api/terminal_pty.go`, TUI terminal) or `cliassist`.

## Acceptance Criteria

- [ ] Tests show an ACP sub-agent terminal writing outside the workspace is denied under the default policy.
- [ ] An MCP stdio server with `Sandbox=true` still completes the initialize handshake.
- [ ] The docs table lists each spawn site and its coverage.

## Notes

Depends on: PANDO-US-0041/PANDO-US-0042, PANDO-US-0044.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
