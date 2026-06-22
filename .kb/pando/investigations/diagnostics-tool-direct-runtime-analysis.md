---
created_at: 2026-06-22T10:53:28.353987114Z
updated_at: 2026-06-22T10:53:28.353987114Z
tags:
    - investigation
    - lsp
    - diagnostics
    - runtime
---
# Investigation: why the internal diagnostics tool still fails in this harness

## Finding
The direct `diagnostics` tool invocation in this harness still reports no available LSP clients even after fixing the race with lazy startup. The code path for normal Pando runtime is correct: the coder agent receives the live `*app.App` as its `tools.LSPProvider` in `internal/app/app.go` via `agent.CoderAgentToolsWithMesnada(..., app, ...)`, so the internal diagnostics tool is intended to use the in-process app instance, not the `pando mcp-server` mode.

## Root cause identified so far
There are two separate realities:
1. **Pando runtime code**: `diagnostics` is wired to the live app-backed provider and now waits briefly for lazy startup.
2. **This external harness tool**: the top-level `diagnostics` tool exposed in the assistant environment is not the same as a live in-session coder-agent tool call. It appears to run outside the normal app session/runtime, so it has no attached live `App` instance with `LSPClients`, even though the internal code is correctly wired for real Pando sessions.

The evidence is:
- `internal/app/app.go` constructs `CoderAgent` with `app` as the LSP provider.
- `internal/llm/tools/diagnostics.go` uses that provider.
- Calling the harness-level `diagnostics` tool still returns no clients even after recompilation, which implies the harness tool itself is not executing inside a live app session that owns LSP state.
- Starting `pando mcp-server` does not affect this, which is expected because MCP server mode is unrelated to the in-process tool wiring.

## Additional fix implemented
Even though the harness-level tool still cannot prove it, the real internal code now avoids the lazy-start race by waiting for `gopls` startup to settle before failing.

## Verification
- Ran `go test ./internal/app ./internal/llm/tools ./internal/config`
- Confirmed the wiring path in `internal/app/app.go` and `internal/llm/tools/diagnostics.go`
- Confirmed the harness-level `diagnostics` tool is still isolated from live app LSP state

## Next likely step
To validate end-to-end inside real Pando behavior, diagnostics should be exercised through an actual coder-agent session / TUI / WebUI request path rather than the harness-provided top-level diagnostics tool.
