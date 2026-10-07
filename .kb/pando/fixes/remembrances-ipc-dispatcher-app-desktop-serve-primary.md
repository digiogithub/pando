---
created_at: 2026-10-07T14:08:11.241592088Z
updated_at: 2026-10-07T14:08:11.241592088Z
tags:
    - fix
    - ipc
    - dbproxy
    - remembrances
    - sessions
    - ollama
---
# Fix: app/desktop/serve primaries did not accept remembrances writes (v1.2.13)

## Symptom
Telemetry, debug_id 7402-2673-1745-4679 (v1.2.11, mode `app`, several instances at once): 21x `remembrances session index failed: replace session events: dbproxy: INTERNAL (ReplaceSessionEvents): ipc: RPC error -32000: dbproxy: METHOD_NOT_FOUND (ReplaceSessionEvents): unknown write method "ReplaceSessionEvents"` in about 40 min. v1.2.9 (debug_id 5197-…) showed the same error.

## Cause
Only `App.SetupIPC` (used by the TUI in `cmd/root.go`, by ACP, and by failover promotion) called `dbproxy.RegisterRemembrancesDispatcher`. The primaries started by `pando app` (cmd/app.go), `pando desktop` (cmd/desktop.go) and `pando serve` (cmd/serve.go) registered the DB write handlers but not the remembrances dispatcher. Every KB, event and code-index write forwarded by a secondary (another window, the TUI, a mesnada subagent) therefore hit the `default:` case in `internal/ipc/dbproxy/handlers.go`, which returns METHOD_NOT_FOUND. The secondary embedded the whole session every pass and then failed to store it. Since nothing was stored, incremental reuse ([[pando/fixes/code-index-project-id-dedupe-and-incremental-session-indexing.md]]) could never kick in, and Ollama kept recomputing.

## Changes
- `internal/app/app.go`: new `App.RegisterRemembrancesWriteDispatcher()` (nil-safe), extracted from `SetupIPC`, which now calls it.
- `cmd/app.go`, `cmd/desktop.go`, `cmd/serve.go`: call `pandoApp.RegisterRemembrancesWriteDispatcher()` in the primary block next to `dbproxy.RegisterHandlersWithCoordinator`.
- `internal/ipc/dbproxy/errors.go`: `IsMethodNotFound(err)`. It matches the structured `WriteError` and also the code inside the message, because the error is flattened to text when it crosses IPC.
- `internal/app/remembrances_indexer.go`: on METHOD_NOT_FOUND the session scheduler pauses all session indexing for 30 min (`sessionIndexUnsupportedPause`) without retrying, and logs a warning to update or restart the primary. This covers secondaries talking to an older primary.

## Verification
- `go build ./...`, `go vet`.
- Tests: `TestIsMethodNotFound` (internal/ipc/dbproxy/errors_method_test.go) and `TestSessionIndexSchedulerPausesOnMethodNotFound`, both also with `-race`.
- `go test ./internal/app ./internal/ipc/... ./internal/rag/... ./cmd ./internal/api` pass.
