# Phase 2 — Formalise the Write Channel Contract

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** low
**Effort:** small

## 1. Goal

Make the `db.write` IPC channel a well-defined, observable, and robust contract between secondary and primary instances. Today it works but is ad-hoc: timeouts are implicit, errors aren't typed, and there's no standardised tracing metadata.

## 2. Current State

### Request format (`dbproxy/proxy.go`)

```go
type WriteRequest struct {
    Method string          `json:"method"`
    Params json.RawMessage `json:"params"`
}
```

### Call flow

```
Secondary DBProxy.CreateSession(args)
  → proxyWrite[db.Session](ctx, p, "CreateSession", arg)
    → p.client.Call(ctx, rpcAddr, "db.write", WriteRequest{Method: "CreateSession", Params: rawParams})
      → ZMQ DEALER → ROUTER (primary)
        → dispatchWrite() → db.Querier.CreateSession()
```

### Error handling

- `Call()` returns `ipc: RPC error -32601: method not found`
- `Call()` returns `ipc: timeout waiting for response to method ...`
- No structured error codes for business logic (e.g. "session already exists")
- No retry on transient failures
- No operation tracing (who requested this write? which secondary instance?)

## 3. Proposed Changes

### 3.1 Add `WriteMeta` metadata to requests

```go
type WriteMeta struct {
    SourceInstanceID string `json:"source_instance_id"`
    RequestID        string `json:"request_id"`
    Timestamp        string `json:"timestamp"` // RFC3339
}

type WriteRequest struct {
    Meta   WriteMeta       `json:"meta"`
    Method string          `json:"method"`
    Params json.RawMessage `json:"params"`
}
```

The primary can log this on each write, making it trivial to trace write provenance.

### 3.2 Typed Write Errors

Define a standard error envelope so callers can distinguish transient from permanent errors:

```go
// internal/ipc/dbproxy/errors.go

type WriteErrorCode string

const (
    ErrCodeTimeout        WriteErrorCode = "TIMEOUT"
    ErrCodeUnreachable    WriteErrorCode = "UNREACHABLE"
    ErrCodeMethodNotFound WriteErrorCode = "METHOD_NOT_FOUND"
    ErrCodeInvalidParams  WriteErrorCode = "INVALID_PARAMS"
    ErrCodeConflict       WriteErrorCode = "CONFLICT"
    ErrCodeInternal       WriteErrorCode = "INTERNAL"
)

type WriteError struct {
    Code    WriteErrorCode `json:"code"`
    Message string         `json:"message"`
    Method  string         `json:"method"`
}

func (e *WriteError) Error() string {
    return fmt.Sprintf("dbproxy: %s (%s): %s", e.Code, e.Method, e.Message)
}

func (e *WriteError) IsRetryable() bool {
    switch e.Code {
    case ErrCodeTimeout, ErrCodeUnreachable:
        return true
    default:
        return false
    }
}
```

On the handler side, wrap business errors with appropriate codes:

```go
// In dispatchWrite():
r, err := q.CreateSession(ctx, p)
if err != nil {
    // Map known SQL / business errors to WriteError codes
    return nil, wrapWriteError("CreateSession", err)
}
```

### 3.3 Configurable Timeouts

`proxyWrite` currently uses whatever timeout is on the `Client`. Make it explicit and configurable per write type:

```go
// internal/ipc/dbproxy/proxy.go

type WriteTimeout struct {
    Default time.Duration // e.g. 5s
    Long    time.Duration // e.g. 30s for bulk operations
}

var DefaultWriteTimeouts = WriteTimeout{
    Default: 5 * time.Second,
    Long:    30 * time.Second,
}

func proxyWrite[R any](ctx context.Context, p *DBProxy, method string, params any, timeout time.Duration) (R, error) {
    // Create context with explicit timeout
    callCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    // ... use callCtx instead of ctx for the RPC call
}
```

### 3.4 Retry Policy on Transient Errors

Add a simple retry mechanism for transient errors:

```go
func (p *DBProxy) writeWithRetry(ctx context.Context, method string, params any, timeout time.Duration) error {
    const maxRetries = 3
    backoff := 50 * time.Millisecond

    for attempt := 0; attempt < maxRetries; attempt++ {
        err := proxyVoidWrite(ctx, p, method, params, timeout)
        if err == nil {
            return nil
        }

        var werr *WriteError
        if errors.As(err, &werr) && werr.IsRetryable() && attempt < maxRetries-1 {
            time.Sleep(backoff)
            backoff *= 2
            continue
        }
        return err
    }
    return fmt.Errorf("dbproxy: exhausted retries for %s", method)
}
```

### 3.5 Unify Response Format on Handler Side

Today `dispatchWrite` returns `(json.RawMessage, error)`. The RPC framework wraps errors in `rpcError{Code: -32000, Message: err.Error()}`. With typed errors, ensure the code propagates:

```go
func dispatchWrite(ctx context.Context, q db.Querier, req WriteRequest) (json.RawMessage, error) {
    // ... switch cases unchanged, but wrap errors with codes:

    case "CreateSession":
        var p db.CreateSessionParams
        if err := json.Unmarshal(req.Params, &p); err != nil {
            return nil, &WriteError{Code: ErrCodeInvalidParams, Method: req.Method, Message: err.Error()}
        }
        r, err := q.CreateSession(ctx, p)
        if err != nil {
            return nil, mapToWriteError(req.Method, err)
        }
        return marshalResult(r, nil)
}
```

## 4. Acceptance Criteria

- [ ] `WriteMeta` struct with instance tracing added to requests
- [ ] `WriteError` type with standard error codes
- [ ] Timeout per write method type
- [ ] Retry with exponential backoff on transient errors
- [ ] All existing proxy methods updated to use new signature
- [ ] `dispatchWrite` maps DB errors to typed codes
- [ ] Existing tests pass; new tests cover error code mapping

## 5. Risks

- Low — additive changes, no API breakage. Existing RPC works the same; just adds structure.

## 6. Dependencies

- Phase 1 (unified bootstrap) — ensures all secondaries use the same channel.

## 7. Estimated effort

2-3 days.
