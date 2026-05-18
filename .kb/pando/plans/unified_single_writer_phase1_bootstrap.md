# Phase 1 — Unified IPC Bootstrap Across All Entrypoints

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** low
**Effort:** medium

## 1. Goal

Make every Pando entrypoint (`root`/CLI, `serve`, `app`, `desktop`, `acp`) follow the **same** bootstrap sequence for IPC role detection, DB opening, and producer/consumer wiring.

```
Every entrypoint → Determine workdir → Try lock → Primary or Secondary → Open DB → Wire services
```

After this phase, the gap table in the master plan should be all-green: every entrypoint uses `AcquireLock`, opens RO or RW accordingly, and wires `dbproxy` when secondary.

## 2. Problem Today

| Entrypoint | Lock | AcquireLock | Secondary RO | Primary bus | Gap |
|---|---|---|---|---|---|
| `cmd/root.go` | ✅ | ✅ | ✅ | ✅ | Reference |
| `cmd/serve.go` | ❌ | ❌ | ❌ | ❌ | Opens RW, own independent bus |
| `cmd/app.go` | ❌ | ❌ | ❌ | ❌ | Opens RW, own independent bus |
| `cmd/desktop.go` | ❌ | ❌ | ❌ | ❌ | Opens RW, own independent bus |
| `cmd/acp.go` | ❌ | ❌ | ❌ | ❌ | Opens RW inside `runACPServerWithOptions` |

`serve` / `app` / `desktop` start their own `Bus` regardless, but never check for an existing primary. If a CLI TUI is already running in that path, you end up with **two RW connections** to the same SQLite file.

## 3. What to Build

### 3.1 New package: `internal/ipc/runtime/`

A self-contained bootstrap component that encapsulates the role decision and connection setup. The idea: every entrypoint calls one function and gets back everything it needs.

```go
// internal/ipc/runtime/runtime.go

package runtime

import (
    "context"
    "database/sql"
    "os"

    "github.com/digiogithub/pando/internal/db"
    "github.com/digiogithub/pando/internal/ipc"
    "github.com/digiogithub/pando/internal/ipc/dbproxy"
)

// Role describes the IPC role of this instance.
type Role string

const (
    RolePrimary   Role = "primary"
    RoleSecondary Role = "secondary"
)

// BootstrapResult is the result of the bootstrap sequence.
type BootstrapResult struct {
    Role       Role
    Querier    db.Querier       // primary: direct; secondary: DBProxy
    SQLDB      *sql.DB          // underlying connection (RW or RO)
    Bus        *ipc.Bus         // non-nil only when primary
    IPCClient  *ipc.Client      // non-nil only when secondary
    InstanceID string
    PubPort    int
    RPCPort    int
    LockFile   *os.File         // nil if no lock / secondary
    Cleanup    func()           // call on shutdown
}

// Bootstrap runs the unified startup sequence for the given workdir.
//
// 1. Derive ports for the path.
// 2. Attempt to acquire the IPC lock.
// 3. If primary: open RW DB, create Bus, register dbproxy handlers.
// 4. If secondary: open RO DB, create IPC Client, create DBProxy.
//
// The caller MUST call result.Cleanup() on shutdown.
func Bootstrap(ctx context.Context, workdir, instanceID string) (*BootstrapResult, error)
```

### 3.2 Helper in `internal/db/connect.go`

Add a lightweight helper that chooses between `Connect()` and `ConnectReadOnly()` based on role, and avoids duplicating the role argument everywhere.

```go
func ConnectForRole(isPrimary bool) (*sql.DB, error) {
    if isPrimary {
        return Connect()
    }
    return ConnectReadOnly()
}
```

### 3.3 Migrate entrypoints

#### `cmd/root.go`

Replace the current inline primary/secondary block (lines ~198-254) with a call to `ipcruntime.Bootstrap(...)`. The existing handler registration (`dbproxy.RegisterHandlers`, `bridge.RegisterHandlers`, `pandoApp.SetupIPC`) and bus start can be streamlined.

Before:

```go
// ~50 lines of lock, primary/secondary branching, etc.
isPrimary, lockInfo, lockFile, lockErr := ipc.AcquireLock(cwd, instanceID, pubPort, rpcPort)
...
if isPrimary || lockErr != nil {
    // primary path (~30 lines)
} else {
    // secondary path (~30 lines)
}
```

After:

```go
rt, err := ipcruntime.Bootstrap(ctx, cwd, instanceID)
if err != nil { return err }
defer rt.Cleanup()

appOpts := app.AppOptions{DBQuerier: rt.Querier}
pandoApp, err := app.New(ctx, rt.SQLDB, appOpts)
if err != nil { return err }

if rt.Role == ipcruntime.RolePrimary {
    dbproxy.RegisterHandlers(rt.Bus, db.New(rt.SQLDB))
    bridge.RegisterHandlers(rt.Bus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now())
    pandoApp.SetupIPC(rt.Bus)
    if err := rt.Bus.Start(ctx, rt.PubPort, rt.RPCPort); err != nil {
        logging.Warn("IPC: failed to start bus", "error", err)
    } else {
        br := bridge.New(rt.Bus, pandoApp.Sessions, pandoApp.CoderAgent)
        br.Start(ctx)
    }
}
```

#### `cmd/app.go`

1. Replace `db.Connect()` + manual bus setup with `ipcruntime.Bootstrap()`.
2. If secondary, the returned `Querier` is already a `DBProxy` — no need to create a local Bus.
3. Keep instance registry announcement but set `IsPrimary` correctly.

Before:

```go
conn, err := db.Connect()
// ... IPC bus created unconditionally with free ports
appBus := ipc.NewBus(instanceID)
dbproxy.RegisterHandlers(appBus, db.New(conn))
```

After:

```go
rt, err := ipcruntime.Bootstrap(ctx, cwd, instanceID)
if err != nil { return err }
defer rt.Cleanup()

conn := rt.SQLDB

_ = instanceregistry.Announce(&instanceregistry.Entry{
    InstanceID: instanceID,
    Path:       cwd,
    PID:        os.Getpid(),
    PubPort:    rt.PubPort,
    RPCPort:    rt.RPCPort,
    StartedAt:  time.Now(),
    Mode:       instanceregistry.ModeWebUI,
    IsPrimary:  rt.Role == ipcruntime.RolePrimary,
})

server, err := api.NewServer(ctx, api.ServerConfig{
    DB: conn,
    // ...
})

if rt.Role == ipcruntime.RolePrimary {
    pandoApp := server.PandoApp()
    // register handlers & start bus (same pattern as root.go)
    appBus := rt.Bus
    dbproxy.RegisterHandlers(appBus, rt.Querier)
    bridge.RegisterHandlers(appBus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now())
    if busErr := appBus.Start(ctx, rt.PubPort, rt.RPCPort); busErr != nil {
        logging.Warn("IPC: failed to start bus", "error", busErr)
    } else {
        appBridge := bridge.New(appBus, pandoApp.Sessions, pandoApp.CoderAgent)
        appBridge.Start(ctx)
    }
}
// secondary: no Bus needed, writes go through DBProxy automatically
```

#### `cmd/serve.go`

Same pattern as `cmd/app.go` — adopt `Bootstrap()`, conditionally start Bus only when primary, use path-derived ports.

#### `cmd/desktop.go`

Same pattern — adopt `Bootstrap()`.

#### `cmd/acp.go` (ACP server mode)

`runACPServerWithOptions` currently calls `db.Connect()` directly. Replace with `Bootstrap()` and wire secondary DBProxy when needed.

### 3.4 Update `api.NewServer` to accept `db.Querier`

Currently `api.NewServer` takes `*sql.DB` and creates its own `db.New(conn)`. It should optionally accept a `db.Querier` override (to receive the `DBProxy` from the bootstrap result).

Option A (minimal change): add `Querier db.Querier` to `ServerConfig` — if non-nil, use it instead of `db.New(cfg.DB)`.

Option B: always pass the Querier. The caller has it from `BootstrapResult`.

**Recommendation:** Option A — minimal diff, backward-compatible.

## 4. Acceptance Criteria

- [ ] `internal/ipc/runtime/` package exists with `Bootstrap()` function
- [ ] `db.ConnectForRole()` helper exists
- [ ] All 5 entrypoints (`root`, `serve`, `app`, `desktop`, `acp`) call `Bootstrap()`
- [ ] No entrypoint calls `db.Connect()` directly for the primary project DB
- [ ] `api.NewServer` accepts an optional `db.Querier` override
- [ ] Instance registry announcements reflect correct `IsPrimary` flag
- [ ] Existing tests pass: `go test ./cmd/... ./internal/ipc/... ./internal/app/... ./internal/db/... ./internal/api/...`
- [ ] Manual smoke test: start TUI, then `pando serve` in same path → observe second instance as secondary

## 5. Risks

- **Low risk.** This phase is mostly refactoring — consolidating code that already exists in `root.go` into a reusable component and applying it to other entrypoints.
- The `api.NewServer` change is the only API signature modification.

## 6. Dependencies

- None (this is the first phase and unblocks everything else).

## 7. Estimated effort

3-5 days for implementation + testing.
