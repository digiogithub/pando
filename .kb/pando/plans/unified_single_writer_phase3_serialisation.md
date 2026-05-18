# Phase 3 — Write Serialisation Loop in Primary

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** medium
**Effort:** medium

## 1. Goal

Introduce an internal **write-coordinator goroutine** in the primary instance that serialises all `db.write` RPC requests. Instead of the RPC handler calling `db.Querier` methods directly (potentially concurrently from multiple RPC goroutines), all writes are enqueued and processed sequentially by a single writer goroutine.

## 2. Why

### 2.1 SQLite single-writer at the process level

SQLite already serialises writes internally, but having a single explicit write path in the process:

- Eliminates any chance of interleaving at the Go level
- Makes write ordering deterministic
- Provides a natural place for metrics, tracing, and backpressure
- Makes reasoning about concurrent writes trivial — there are no concurrent writes

### 2.2 Backpressure

If many secondaries/subagents are sending writes simultaneously, a serialised loop can apply backpressure (drop or defer) rather than letting SQLite's internal "database is locked" mechanism kick in unpredictably.

### 2.3 Observability

A single write loop is the ideal place to emit:
- Write request accepted / completed events
- Latency histograms per write method
- Queue depth metrics

## 3. Design

### 3.1 `internal/ipc/writecoordinator/` package

```go
package writecoordinator

import (
    "context"
    "encoding/json"
    "sync"
    "time"

    "github.com/digiogithub/pando/internal/db"
    "github.com/digiogithub/pando/internal/ipc/dbproxy"
)

// WriteJob represents a single write request submitted to the coordinator.
type WriteJob struct {
    Request dbproxy.WriteRequest
    ResultC chan WriteResult
}

// WriteResult is the outcome of a serialised write.
type WriteResult struct {
    Result json.RawMessage
    Error  error
}

// Coordinator serialises all write operations through a single goroutine.
type Coordinator struct {
    mu     sync.RWMutex
    q      db.Querier
    jobs   chan WriteJob
    done   chan struct{}
    cancel context.CancelFunc

    // Metrics (exposed via diagnostics)
    Accepted   uint64
    Completed  uint64
    Failed     uint64
    QueueDepth int64
}

// New creates a coordinator and starts the writer goroutine.
func New(ctx context.Context, q db.Querier, queueSize int) *Coordinator {
    if queueSize <= 0 {
        queueSize = 256
    }
    ctx, cancel := context.WithCancel(ctx)
    c := &Coordinator{
        q:      q,
        jobs:   make(chan WriteJob, queueSize),
        done:   make(chan struct{}),
        cancel: cancel,
    }
    go c.run(ctx)
    return c
}

// Submit enqueues a write job and waits for the result.
// Returns an error if the coordinator is stopped or the queue is full.
func (c *Coordinator) Submit(ctx context.Context, req dbproxy.WriteRequest) (json.RawMessage, error) {
    job := WriteJob{
        Request: req,
        ResultC: make(chan WriteResult, 1),
    }

    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-c.done:
        return nil, fmt.Errorf("writecoordinator: stopped")
    case c.jobs <- job:
        // enqueued
    }

    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-c.done:
        return nil, fmt.Errorf("writecoordinator: stopped")
    case res := <-job.ResultC:
        return res.Result, res.Error
    }
}

func (c *Coordinator) run(ctx context.Context) {
    defer close(c.done)
    for {
        select {
        case <-ctx.Done():
            return
        case job := <-c.jobs:
            c.processJob(ctx, job)
        }
    }
}

func (c *Coordinator) processJob(ctx context.Context, job WriteJob) {
    c.mu.Lock()
    c.Accepted++
    c.QueueDepth = int64(len(c.jobs))
    c.mu.Unlock()

    start := time.Now()
    result, err := dbproxy.DispatchWriteFunc(ctx, c.q, job.Request) // exported helper
    elapsed := time.Since(start)

    c.mu.Lock()
    if err != nil {
        c.Failed++
    } else {
        c.Completed++
    }
    c.mu.Unlock()

    // TODO: emit metrics (elapsed, method, success/failure)

    job.ResultC <- WriteResult{Result: result, Error: err}
}

// Shutdown stops the coordinator gracefully.
func (c *Coordinator) Shutdown() {
    c.cancel()
    <-c.done
}
```

### 3.2 Export `DispatchWriteFunc` from `dbproxy`

Currently `dispatchWrite` is unexported. Create an exported wrapper:

```go
// internal/ipc/dbproxy/handlers.go

func DispatchWrite(ctx context.Context, q db.Querier, req WriteRequest) (json.RawMessage, error) {
    return dispatchWrite(ctx, q, req)
}
```

### 3.3 Wire the Coordinator

In `RegisterHandlers`, the handler delegates to the coordinator instead of calling `dispatchWrite` directly:

```go
// Option A: coordinator is the registrar (coordinator implements BusRegistrar)
type CoordinatorRegistrar struct {
    coordinator *writecoordinator.Coordinator
}

func (cr *CoordinatorRegistrar) RegisterMethod(method string, handler ipc.HandlerFunc) {
    // Register a handler that submits to the coordinator
    // (caller still uses Bus.RegisterMethod under the hood)
}

// Option B: simpler — the handler just submits to the coordinator
func RegisterHandlersWithCoordinator(bus BusRegistrar, c *writecoordinator.Coordinator) {
    bus.RegisterMethod(MethodDBWrite, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
        var req WriteRequest
        if err := json.Unmarshal(params, &req); err != nil {
            return nil, fmt.Errorf("dbproxy handler: unmarshal WriteRequest: %w", err)
        }
        return c.Submit(ctx, req)
    })
}
```

**Recommendation:** Option B — simpler, fewer abstractions.

### 3.4 Wiring in entrypoints

In `cmd/root.go`, `cmd/app.go`, etc., when primary:

```go
if rt.Role == ipcruntime.RolePrimary {
    coord := writecoordinator.New(ctx, db.New(rt.SQLDB), 256)
    defer coord.Shutdown()

    dbproxy.RegisterHandlersWithCoordinator(rt.Bus, coord)
    // ...
}
```

### 3.5 Queue size and backpressure

- Default queue size: 256 jobs
- If queue is full, `Submit` blocks until space is available (natural backpressure)
- Configurable via environment or config file: `PANDO_WRITE_QUEUE_SIZE=512`

### 3.6 Graceful degradation

If the write coordinator is not used (legacy mode), `RegisterHandlers` still works exactly as today:

```go
// Legacy path (no coordinator)
func RegisterHandlers(bus BusRegistrar, q db.Querier) {
    bus.RegisterMethod(MethodDBWrite, func(ctx context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
        var req WriteRequest
        if err := json.Unmarshal(params, &req); err != nil {
            return nil, fmt.Errorf("dbproxy handler: unmarshal WriteRequest: %w", err)
        }
        return dispatchWrite(ctx, q, req)
    })
}
```

Both `RegisterHandlers` and `RegisterHandlersWithCoordinator` coexist.

## 4. Acceptance Criteria

- [ ] `internal/ipc/writecoordinator/` package with `Coordinator` type
- [ ] Coordinator serialises writes through a single goroutine
- [ ] `dbproxy.RegisterHandlersWithCoordinator` wires coordinator into bus
- [ ] Legacy `dbproxy.RegisterHandlers` still works (backward-compatible)
- [ ] Queue full → natural backpressure
- [ ] Coordinator shutdown is graceful (drain remaining jobs or drop)
- [ ] Tests: coordinator serialisation, backpressure, shutdown
- [ ] Existing IPC tests still pass

## 5. Risks

- **Medium risk.** This changes the internal write path and introduces a queue. If the queue blocks forever (deadlock), all writes stop. Good testing + timeouts mitigate this.
- Performance: a sequential loop removes concurrency. In practice, SQLite already serializes internally, so this is unlikely to degrade throughput noticeably.

## 6. Dependencies

- Phase 1 (unified bootstrap) — needed to know when primary vs secondary.

## 7. Estimated effort

3-4 days.
