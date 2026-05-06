# Inter-Instance Communication — Phase 4 Completed

**Date:** 2026-05-06  
**Status:** COMPLETED ✓

## What was implemented

### Phase 4: Single-Writer SQLite Proxy

**Goal achieved:** Secondary instances never write to SQLite directly. All writes are proxied to the primary via ZMQ JSON-RPC `db.write`.

---

## Files Created

### `internal/ipc/dbproxy/proxy.go`
- `DBProxy` struct that embeds `db.Querier` interface (reads go to local DB automatically)
- Overrides all 20 write methods: CreateSession, UpdateSession, DeleteSession, DeleteSessionMessages, CreateMessage, UpdateMessage, DeleteMessage, CreateFile, UpdateFile, DeleteFile, DeleteSessionFiles, InsertPromptTemplate, InsertSessionScore, InsertSkill, DeactivateLowestSkill, IncrementSkillUsage, CreateProject, UpdateProjectStatus, UpdateProjectLastOpened, MarkProjectInitialized, DeleteProject
- Generic helpers `proxyWrite[R]` and `proxyVoidWrite` for serialization
- `WriteRequest{Method, Params json.RawMessage}` type for ZMQ transport
- When `client == nil` (primary), behaves as passthrough to local querier
- Compile-time assertion: `var _ db.Querier = (*DBProxy)(nil)`

### `internal/ipc/dbproxy/handlers.go`
- `RegisterHandlers(bus *ipc.Bus, q db.Querier)` — registers `db.write` method on primary bus
- `dispatchWrite()` — switch-case routing for all 20 write methods
- Properly deserializes `WriteRequest.Params` to the correct Go type before calling `db.Querier`

---

## Files Modified

### `internal/db/connect.go`
- Added `ConnectReadOnly()` — opens existing `pando.db` with `file:path?mode=ro`
- No migrations (primary's responsibility), 4 concurrent readers allowed
- Used by secondary instances to open the shared SQLite without acquiring write lock

### `internal/app/app.go`
- New fields on `App`: `IPCBus *ipc.Bus`, `IPCIsPrimary bool`
- New field on `AppOptions`: `DBQuerier db.Querier` — when non-nil, replaces `db.New(conn)` for session/message services
- New method `SetupIPC(bus *ipc.Bus)` — stores bus reference, calls `session.SetIPCPublisher(bus)`, logs startup
- `Shutdown()` now calls `bus.Shutdown()` if `IPCBus != nil`
- `project.NewService` still uses `rawQ *db.Queries` (uses `UpdateProjectName` outside interface — Phase 5+ concern)
- `history.NewService` still uses `rawQ *db.Queries` (uses `WithTx` for transactions — Phase 5+ concern)

### `cmd/root.go`
- After DB connect: calls `ipc.PortsForPath(cwd)` for deterministic ports, then `ipc.AcquireLock()`
- **Primary path**: announces to instanceregistry (ModeTUI, IsPrimary=true), starts `ipc.Bus`, registers `dbproxy.RegisterHandlers` + `bridge.RegisterHandlers`, calls `pandoApp.SetupIPC(bus)`, starts event bridge
- **Secondary path**: opens read-only DB, creates `ipc.Client`, builds `dbproxy.New(db.New(roConn), client, rpcAddr)`, passes as `AppOptions.DBQuerier`; announces to instanceregistry (IsPrimary=false)
- Both paths: `defer ipc.ReleaseLock(lockFile)` and `defer instanceregistry.Revoke(instanceID)`

---

## Known Limitations (to address in later phases)

1. `history.NewService` writes on secondary will fail silently (read-only conn + `WithTx` pattern)
2. `project.NewService` writes on secondary will fail (read-only conn + `UpdateProjectName` not in Querier)
3. `cmd/app.go`, `cmd/serve.go`, `cmd/acp.go` entry points do not yet wire IPC (only `cmd/root.go` TUI does)
4. No leader_watcher / automatic promotion on primary death yet

---

## Architecture

```
Primary instance:
  conn (RW) → db.New(conn) → session.Service, message.Service
  ipc.Bus (PUB:40xxx, ROUTER:40xxx+1) ← dbproxy.RegisterHandlers
  bridge.Bridge → forwards pubsub events to ZMQ PUB

Secondary instance:
  roConn (RO) → db.New(roConn) → history.Service (reads only)
  DBProxy{local: db.New(roConn), client, rpcAddr} → session.Service, message.Service
  DBProxy.CreateSession() → ipc.Client.Call(rpcAddr, "db.write", {Method:"CreateSession",...})
  → primary bus handler → q.CreateSession() → returns JSON → secondary deserializes
```
