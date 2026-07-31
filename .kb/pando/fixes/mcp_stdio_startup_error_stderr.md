---
created_at: 2026-07-31T12:44:28.411004461Z
updated_at: 2026-07-31T12:44:28.411004461Z
tags:
    - fix
    - mcp
    - stdio
    - diagnostics
---
# Fix: stdio MCP startup failures now report the child's stderr (2026-07-31)

## Symptom

Reloading a stdio MCP server whose child process exits during startup produced
only `initialize: transport error: transport closed`. The real cause was printed
by the child on stderr (`Either FIGMA_API_KEY or FIGMA_OAUTH_TOKEN is required`)
and went to the log file only, never to the UI.

## Change

`internal/mcpclient/client.go` — `stdioLogDrainingClient` now keeps a mutex-guarded
ring of the last 5 non-JSON-RPC stderr lines (`maxRecentStderrLines`), fed by
`drainStdioLogs` through a `record` callback. `Initialize` and `ListTools` wrap
their error with `explainWithStderr`, appending
`(<server> stderr: line | line)` when the ring is non-empty.

Verified with a throwaway test (removed): the error becomes
`transport error: transport closed (figma2 stderr: Either FIGMA_API_KEY or FIGMA_OAUTH_TOKEN is required (via CLI argument or .env file))`.

`go build ./...` and `go test ./internal/mcpgateway ./internal/api` pass.

## Triggering user error

The workspace config used `FIGMA_API_TOKEN`; `figma-developer-mcp` reads
`FIGMA_API_KEY` (or `FIGMA_OAUTH_TOKEN`).

Related: [[mcp_env_headers_draft_row_lost]], [[mcp_runtime_tool_refresh_and_header_spaces]]
