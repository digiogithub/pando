---
id: PANDO-US-0031
type: story
title: "AG-UI runs deadlock on direct MCP tools: GetMcpTools caches the app's permission service"
status: in_review
priority: critical
parent: PANDO-EP-0002
milestone: PANDO-M-0001
author: claude
labels: [agui, mcp, permissions, bug]
estimate: 3
created: 2026-09-15T17:05:04Z
updated: 2026-09-16T00:00:00Z
---

## Description

As an embedding host running `pando agui-serve` with an `[MCPServers]` entry and the MCP gateway off (`[ToolDiscovery] Enabled = false`, `[MCPGateway] Enabled = false`, so tools keep their `<server>_<tool>` names and the `[AGUI] Tools` allow-list can see them), I want an MCP tool call from an AG-UI run to go through the adapter's own permission service, so that the run either asks the browser (`pando_permission_request`) or completes, instead of hanging forever.

Found on 2026-09-15 by git-in-track's live check (GIT-T-0118, Pando v0.705.1 at c539b603): the run emits `TOOL_CALL_START gintrack_list_items` + `TOOL_CALL_ARGS` + `TOOL_CALL_END` and then nothing — keep-alives only, at 150 s and 300 s, reproduced three times through the companion proxy and identically direct on the adapter port. `AutoApprove = true` changes nothing, which is the tell.

Cause: `GetMcpTools` (`internal/llm/agent/mcp-tools.go:253-256`) keeps the tools in a package-level slice and returns it whenever it is non-empty, ignoring the `permissions` argument. `app.New` fills that cache at startup with `app.Permissions` (`internal/app/app.go:731-741`). The AG-UI pool builds its agents through `CoderAgentTools(...)` with its own `p.perms` — the service `installPermissionPolicy` registers the session handler on (`internal/agui/hitl.go:49-62`) — but the cached `mcpTool` instances still hold `app.Permissions`, so `mcpTool.Run` blocks in `permissions.Request` (`mcp-tools.go:156`) on `resp := <-respCh` waiting for a TUI that `agui-serve` does not have (`internal/permission/permission.go:129-192`). With the gateway on, tools are reached through `mcp_call_tool` and the problem is masked (and no per-tool approval is asked either).

Do NOT fix it by making the AG-UI adapter reuse `app.Permissions`: invariant I3 (`internal/agui/doc.go`) says a browser run must never raise a TUI prompt. Do NOT drop the cache's connection reuse: one MCP client per server per process is right; only the permission binding must be per caller.

## Acceptance Criteria

- [ ] `GetMcpTools` (or its replacement) returns tool instances bound to the `permissions` service the caller passed; the MCP client connections stay cached and shared.
- [ ] With `[MCPServers.x]` configured, gateway off and `AutoApprove = false`, an AG-UI run that calls `x_<tool>` emits `pando_permission_request`, and a trailing `{"approved":true}` tool message lets the run finish with `TOOL_CALL_RESULT` and `RUN_FINISHED{outcome:"success"}` — covered by a test in `internal/agui` with an in-process MCP server.
- [ ] With `AutoApprove = true` the same run completes without an interrupt.
- [ ] A permission request that no client answers within the adapter's timeout fails closed with a `RUN_ERROR`, never a hang; regression test asserts the run ends.
- [ ] The `[AGUI] Tools` allow-list keeps applying to the directly registered `<server>_<tool>` names (regression test from PANDO-US-0011 still passes).

## Notes

Blocks git-in-track GIT-US-0069 / GIT-T-0118 (the generated `.pando.toml` turns the gateway off on purpose; until this lands the only configuration that completes a turn is gateway on plus `tool_search`/`mcp_call_tool` in the allow-list, which is coarse and asks no per-tool approval). Size S. Evidence: git-in-track `docs/.pmngr/comments/GIT-T-0118/20260915T170313Z-mcp.md`.
