---
created_at: 2026-09-04T08:18:40.182269339Z
updated_at: 2026-09-04T08:18:40.182269339Z
tags:
    - fix
    - sqlite
    - performance
    - cpu
    - connection-pool
---
# Fix: SQLite Connection Pool Exhaustion in MCP Server (CPU 100%)

## Problem

A `pando mcp-server --no-http` instance (PID 64852) was consuming **440-860% CPU** with:
- **4838 file descriptors** open (1449 SQLite connections × 3 files each: db, wal, shm)
- **303 OS threads**
- **5.9 GB RSS**, 493 GB virtual memory
- WAL file growing from 223MB → 763MB during indexing

## Root Cause Analysis

### Trigger
The project `/www/git-in-track` has a `web/` directory with 571 npm packages. A build (likely React/Vite) generated many files at once. The startup code indexer detected them and began indexing ~2528 files / 39263 symbols concurrently.

### Connection leak mechanism
The SQLite driver is `ncruces/go-sqlite3` (WASM-based via wazero):
- 1 shared `wazero.Runtime` (singleton via `sync.Once`)
- **Each `database/sql` pool connection = 1 new WASM module instance** (`InstantiateModule`)
- Each WASM instance opens **3 real OS file descriptors** via `os.OpenFile` (db, wal, shm)

`db.Connect()` in `internal/db/connect.go` never called `SetMaxOpenConns()`, leaving the pool **unlimited**. Under concurrent indexing load (4 workers × many files), the pool grew unboundedly to 1449 connections = 4347 FDs.

### Why CPU spiked
- 1449 WASM module instances each with their own memory space
- SQLite lock contention across 1449 connections
- GC pressure from 5.9 GB heap
- WAL grew to 763MB without checkpointing

## Fix Applied

**File:** `internal/db/connect.go`

Added pool limits to `Connect()` (the primary RW connection):

```go
db.SetMaxOpenConns(8)
db.SetMaxIdleConns(4)
```

Rationale:
- SQLite in WAL mode: 1 writer + concurrent readers → small pool is optimal
- 8 max connections = 24 FDs max (vs 4347 before)
- Matches the pattern already used by `ConnectReadOnly()` (4) and `ConnectRWSecondary()` (1)

## Verification

- `go build ./internal/db/...` — clean
- `go test ./internal/db/...` — all pass
- Pre-existing unrelated failure in `internal/llm/agent` (extension tools count test)

## Related

- `ConnectReadOnly()` already had `SetMaxOpenConns(4)` ✓
- `ConnectRWSecondary()` already had `SetMaxOpenConns(1)` ✓
- `Connect()` was the only one missing the cap — now fixed
