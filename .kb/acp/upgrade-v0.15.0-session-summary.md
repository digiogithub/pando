# Upgrade to acp-go-sdk v0.15.0 — Completion Summary

## Work completed this session

### 1. Example client updated to v0.15.0
- **File**: `examples/acp-client/go/main.go` and `go.mod`
- Migrated `KillTerminalCommandRequest` → `KillTerminalRequest`, method renamed to `KillTerminal`
- `PromptResponse` no longer carries a `.Message` field — content streams exclusively via `SessionUpdate`
- `ContentBlock` is now a struct (access via `.Text` field), not an interface
- Added `SessionUpdate` method required by the v0.15.0 `Client` interface
- Replaced deprecated `filepath.HasPrefix` with `strings.HasPrefix`
- Used `acpsdk.NewRequestPermissionOutcomeSelected()`/`NewRequestPermissionOutcomeCancelled()` constructors
- Builds cleanly

### 2. MCP Proxy Types Evaluation
- `McpConnect`, `McpOverAcpMessage`, `InitializeProxyRequest`, `SuccessorMessage`, `McpDisconnectNotification` exist as schema types in v0.15.0 but are **not wired into any Client or Agent interface methods**
- They are forward-looking placeholders — no implementation is needed for Pando at this time

### 3. Deepened CloseSession Logic
- **File**: `internal/mesnada/acp/agent.go`
- `CloseSession` now additionally calls `permissionService.RemoveAutoApproveSession()` and `permissionService.UnregisterSessionHandler()` to clean up any lingering permission state when a session is closed
- `ResumeSession` already properly re-registers session and connection state

## Build & Test Status
- Pando builds cleanly
- `go test ./internal/mesnada/acp/...` passes
- Example client builds cleanly (standalone module)