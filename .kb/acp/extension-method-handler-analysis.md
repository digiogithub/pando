# ExtensionMethodHandler — Analysis & Implementation Memo

## Context

`acp-go-sdk` v0.15.0 introduced `ExtensionMethodHandler`, an optional interface that allows ACP agents and clients to handle custom JSON-RPC methods whose names start with `_`. Pando's `PandoACPAgent` currently does **not** implement this interface — unknown extension methods simply return `NewMethodNotFound`.

## SDK Interface

```go
// extensions.go (v0.15.0)
type ExtensionMethodHandler interface {
    HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error)
}
```

### How the SDK dispatches (agent side)

```go
func (a *AgentSideConnection) handleWithExtensions(ctx context.Context, method string, params json.RawMessage) (any, *RequestError) {
    if isExtensionMethodName(method) {
        h, ok := a.agent.(ExtensionMethodHandler)
        if !ok {
            return nil, NewMethodNotFound(method)
        }
        resp, err := h.HandleExtensionMethod(ctx, method, params)
        if err != nil {
            return nil, toReqErr(err)
        }
        return resp, nil
    }
    return a.handle(ctx, method, params)
}
```

### Outbound primitives (both sides)

| Primitive | Direction | Description |
|-----------|-----------|-------------|
| `AgentSideConnection.CallExtension(method, params) (json.RawMessage, error)` | Agent → Client | Send a `_`-prefixed request, expect a response |
| `AgentSideConnection.NotifyExtension(method, params) error` | Agent → Client | Send a `_`-prefixed notification (fire-and-forget) |
| `ClientSideConnection.CallExtension(method, params) (json.RawMessage, error)` | Client → Agent | Same as above, reversed |
| `ClientSideConnection.NotifyExtension(method, params) error` | Client → Agent | Same as above, reversed |

Method names are validated: must be non-empty and start with `_`.

## What Pando is currently missing

1. **Inbound**: Pando ignores extension methods from clients. If a client (e.g. Zed, multicoder) sends `_customPandoFeature`, the SDK dispatches to `handleWithExtensions`, the type assertion fails, and `NewMethodNotFound` is returned.

2. **Outbound**: Pando never calls `CallExtension` or `NotifyExtension` on the `AgentSideConnection`. It only uses the standard `SessionUpdate` notification channel.

## Why implement it

### Use cases for inbound extensions (`Client → Agent`)

| Method | What it enables |
|--------|-----------------|
| `_pando.setPersona` | Let clients switch persona without going through `SetSessionMode` with mode+persona encoding |
| `_pando.openUsage` | Trigger Copilot/Claude usage page open from the ACP client |
| `_pando.listProviders` | Expose provider list to clients that want to display auth state |
| `_pando.getSessionMeta` | Return Pando-specific session data that doesn't fit in standard ACP types |
| `_pando.reindexProject` | Trigger code reindexing from the client |

### Use cases for outbound extensions (`Agent → Client`)

| Method | What it enables |
|--------|-----------------|
| `_pando.lspDiagnostic` | Stream LSP diagnostics to the ACP client |
| `_pando.sessionRestored` | Notify the client that a loaded session is fully ready |
| `_pando.agentBusy` | Toggle a busy/idle indicator on the client |

## Implementation plan

### Phase 1: Minimal inbound handler

**File**: `internal/mesnada/acp/agent.go`

```go
// Add to PandoACPAgent:

// extensionHandlers maps "_"-prefixed method names to handler functions.
extensionHandlers map[string]func(ctx context.Context, params json.RawMessage) (any, error)

// HandleExtensionMethod implements acpsdk.ExtensionMethodHandler.
func (a *PandoACPAgent) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
    a.sessionsMu.RLock()
    defer a.sessionsMu.RUnlock()

    handler, ok := a.extensionHandlers[method]
    if !ok {
        return nil, fmt.Errorf("unknown extension method: %s", method)
    }
    return handler(ctx, params)
}
```

Initialize the registry in `NewPandoACPAgent`:

```go
extensionHandlers: map[string]func(ctx context.Context, params json.RawMessage) (any, error){
    "_pando.setPersona":  a.handleExtensionSetPersona,
    "_pando.openUsage":   a.handleExtensionOpenUsage,
    // ... register more as needed
},
```

### Phase 2: Outbound helpers

Add convenience methods to `PandoACPAgent` that wrap `a.conn.NotifyExtension`:

```go
func (a *PandoACPAgent) notifyClient(ctx context.Context, sessionID acpsdk.SessionId, method string, params any) {
    if a.conn == nil { return }
    if err := a.conn.NotifyExtension(ctx, method, params); err != nil {
        a.logger.Printf("[ACP AGENT] Extension notification %s failed: %v", method, err)
    }
}
```

### Phase 3: Client-side (`MesnadaACPClient`)

Implement `ExtensionMethodHandler` on `MesnadaACPClient` for symmetry:

```go
func (c *MesnadaACPClient) HandleExtensionMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
    // Handle _pando.* notifications from the agent
    return nil, nil // no-op for now
}
```

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| Extension method namespace collisions with other ACP agents | Use `_pando.` prefix consistently |
| Breaking changes in extension method signatures | Version the extension methods (e.g. `_pando.v1.setPersona`) or keep them additive-only |
| Security: arbitrary method dispatch | Validate and sanitize all params; only register known, safe methods |

## Effort estimate

| Phase | Files | LOC | Effort |
|-------|-------|-----|--------|
| Phase 1 (inbound handler + 2 methods) | `agent.go` | ~80 | Small (1-2h) |
| Phase 2 (outbound helpers) | `agent.go` | ~30 | Small (30m) |
| Phase 3 (client-side handler) | `client.go` | ~20 | Small (30m) |

## Dependencies

- **SDK**: Already at v0.15.0 — no upgrade needed
- **Breaking**: None — `ExtensionMethodHandler` is optional via type assertion
- **Tests**: Add table-driven tests for `HandleExtensionMethod` in `agent_pando_test.go`

## Priority

**Medium**. No existing functionality breaks without it. Implementing Phase 1 unlocks a clean extension point for future Pando-specific ACP features without polluting the standard `SessionUpdate` channel or the mode/persona encoding in `SetSessionMode`.

---

*Generated: 2026-05-11*
*SDK version: madeindigio/acp-go-sdk v0.15.0*