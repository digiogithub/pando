---
created_at: 2026-09-16T00:00:00.000000000Z
updated_at: 2026-09-16T00:00:00.000000000Z
tags:
    - fix
    - agui
    - mcp
    - permissions
---
# Fix: AG-UI runs deadlocked on direct MCP tools (PANDO-US-0031)

## Symptom

With `[MCPServers.x]` configured and the gateway off (`[ToolDiscovery] Enabled = false`,
`[MCPGateway] Enabled = false`, the configuration that keeps `<server>_<tool>` names so
the `[AGUI] Tools` allow-list can filter them), an `agui-serve` run that called
`x_<tool>` emitted `TOOL_CALL_START` / `ARGS` / `END` and then nothing -- keep-alives
only, forever. `AutoApprove = true` changed nothing, which was the tell.

## Root causes

1. **The MCP tool cache ignored the caller's permission service.** `GetMcpTools` kept a
   package-level `[]tools.BaseTool` and returned it whenever non-empty, dropping its
   `permissions` argument. `NewMcpTool` bakes the service into each `mcpTool`, and
   `app.New` filled that cache at startup with `app.Permissions`. The AG-UI adapter owns
   a separate `permission.Service` on purpose (invariant I3 in `internal/agui/doc.go`: a
   browser run must never raise a TUI prompt), so `mcpTool.Run` blocked in
   `permissions.Request` on a service whose session handler nobody had registered.
2. **The cache could be poisoned for every surface, the TUI included.**
   `promptMcpCatalogListing` called `GetMcpTools(ctx, permission.NewPermissionService())`
   with a throwaway service just to read tool names. Whenever it ran with an empty cache
   (after any `ResetMcpToolsCache`), it repopulated the shared cache bound to a service
   nobody listened to, and the next MCP tool call deadlocked in any mode until restart.
3. **`Request` could not fail closed.** `permissionService.Request` ended with a bare
   `resp := <-respCh` under a comment promising a timeout. An unanswered prompt blocked
   the calling tool's goroutine forever, stranding a pooled AG-UI agent and, with
   `MaxConcurrentRuns`, eventually making the adapter answer 503 to everything.

## What changed

`internal/llm/agent/mcp-tools.go`
- New `mcpToolDescriptor{serverName, tool, mcpConfig}`: the permission-free half of a
  discovered MCP tool. The cache (`mcpToolDescriptors`, guarded by `mcpToolsMu`, with
  `mcpDiscoveryMu` serializing discovery) now holds these, never permission-bound tools.
- `GetMcpTools(ctx, permissions)` wraps the cached descriptors with the CALLER's service
  on every call. Connection reuse and the discovery/handshake code paths are unchanged,
  and an empty discovery result is still not cached (a server that was down is retried).
- `getTools` -> `getToolDescriptors` (no `permissions` parameter).
- New `CachedMcpToolNames()`: `<server>_<tool>` names off the cache, no connections, no
  permission service; nil when discovery has not run.
- `ResetMcpToolsCache()` keeps its meaning.
- `mcpTool.Run` now calls `permissions.RequestWithContext(ctx, ...)` with the tool's own
  context.

`internal/llm/agent/agent.go`
- `promptMcpCatalogListing` reads `CachedMcpToolNames()` instead of calling
  `GetMcpTools` with a throwaway service; it no longer triggers discovery.

`internal/permission/permission.go`
- `permission.Service` gains `RequestWithContext(ctx, opts) bool`. `Request(opts)`
  delegates with `context.Background()`, so the ~61 existing call sites are untouched.
- Both the pending-response wait and the (blocking) session-handler call now select on
  `ctx.Done()` and return **false** with a `logging.Warn` when the context ends. The
  handler runs on its own goroutine with a buffered result channel so it cannot be
  leaked. No built-in deadline was added: a TUI prompt may legitimately sit unanswered.

## Verification

- `go build ./...`, `go vet ./...` clean.
- `internal/llm/agent/mcp_tools_permissions_test.go` (new):
  `TestGetMcpToolsBindsTheCallersPermissionService`,
  `TestPromptCatalogListingDoesNotPoisonTheCache`,
  `TestPromptCatalogListingDoesNotDiscover`,
  `TestPermissionDeniedShortCircuitsTheMcpCall`.
- `internal/permission/permission_test.go` (added):
  `TestRequestWithContextFailsClosedWhenTheContextEnds`,
  `TestRequestWithContextFailsClosedOnABlockedSessionHandler`,
  `TestRequestDelegatesToRequestWithContext`.
- `internal/agui/mcp_permission_test.go` (new), driving a real in-process MCP server
  (`modelcontextprotocol/go-sdk` v1.4.1 over streamable HTTP) through the real
  `mcpclient`, `GetMcpTools`, permission service, HITL handler and run lifecycle, with
  only the model faked: `TestMCPToolAsksTheAdapterForApprovalAndResumes` (prompt ->
  `{"approved":true}` -> `TOOL_CALL_RESULT` + `RUN_FINISHED{outcome:"success"}`),
  `TestMCPToolWithAutoApproveNeverInterrupts`,
  `TestMCPToolPermissionFailsClosedWhenTheRunEnds` (RUN_ERROR, short deadline),
  `TestAGUIAllowListStillFiltersDirectMCPToolNames` (PANDO-US-0011 still holds).
- Counter-check: reverting just the two lines of the fix (binding a throwaway service in
  `GetMcpTools` and going back to `Request`) makes the three `internal/agui` MCP tests
  hang until the 120 s test timeout -- the exact reported symptom.
- `go test -race ./internal/permission ./internal/agui ./internal/api` pass.
  `go test ./internal/llm/agent` passes; under `-race` that package fails on a
  PRE-EXISTING race (`config.SetForTests` in `setup_bridge_model_test.go` vs a goroutine
  leaked by `resume_test.go`'s agent run, read at `agent.go:1514`), reproduced unchanged
  at HEAD 710a39281 with all of this work removed.
