# Inter-Instance Communication — Phase 5 Completed

**Date:** 2026-05-06  
**Status:** COMPLETED ✓

## What was implemented

### Phase 5: Remote Observation & Control

**Goal achieved:** Any Pando instance can observe and control any other instance in real time via ZMQ PUB/SUB and JSON-RPC.

---

## Files Created

### `internal/remoteview/session.go`
- `RemoteSession` struct that subscribes to a remote instance's PUB socket via `ipc.Client.SubscribeTo`
- Internal `events chan Event` reforwards all `Envelope` from PUB stream
- `Sync(ctx)` — calls RPC `state.sync` to bootstrap initial state (populates `mirror` map)
- `Mirror()` — returns thread-safe copy of synced sessions
- `Messages()` — returns channel of live events (message.append, llm.token, etc.)
- Accessors: `InstanceID()`, `SessionID()`

### `internal/remoteview/control.go`
- `RemoteControl` wrapping `*ipc.Client` with typed methods:
  - `SendMessage(ctx, sessionID, content string) error` → RPC `message.send`
  - `SwitchSession(ctx, sessionID string) error` → RPC `session.activate`
  - `Interrupt(ctx, sessionID string) error` → RPC `session.interrupt`
  - `ListSessions(ctx) ([]protocol.SessionPayload, error)` → RPC `session.list`
  - `GetSession(ctx, sessionID string) (protocol.SessionPayload, error)` → RPC `session.get`
  - `Ping(ctx) (protocol.PingResult, error)` → RPC `instance.ping`

### `internal/api/handlers_instances.go`
- `GET /api/v1/instances` — lists live instances from `instanceregistry.Registry`
- `GET /api/v1/instances/{id}` — detail of a specific instance
- `GET /api/v1/instances/{id}/stream` — SSE proxy of remote instance's ZMQ PUB stream
- `POST /api/v1/instances/{id}/sessions/{sid}/message` — sends message to remote session via RPC `message.send`

---

## Files Modified

### `internal/ipc/bridge/handlers.go`
- Added local interface `MessageRunner` with `RunMessage(ctx, sessionID, content string) error`
- Added local interface `SessionInterrupter` with `Cancel(sessionID string)`
- New `RegisterHandlersWithAgent(bus, instanceID, svc, startedAt, runner, interrupter)` function
- Original `RegisterHandlers()` delegates to `RegisterHandlersWithAgent(nil, nil)` for backward compat
- Handler `message.send` — delegates to `MessageRunner.RunMessage` (returns error if runner=nil)
- Handler `session.interrupt` — delegates to `SessionInterrupter.Cancel` (returns error if nil)

### `internal/api/routes.go`
- Registered 4 new instance routes under `/api/v1/instances`

---

## Pending Integration

In `cmd/root.go`, replace:
```go
bridge.RegisterHandlers(bus, instanceID, pandoApp.Sessions, time.Now())
```
with:
```go
bridge.RegisterHandlersWithAgent(bus, instanceID, pandoApp.Sessions, time.Now(), agentRunner, pandoApp.CoderAgent)
```
Where `agentRunner` is an adapter implementing `MessageRunner` backed by `agent.Service.Run()`.

---

## Architecture after Phase 5

```
Primary instance:
  ipc.Bus (PUB + ROUTER)
    ← bridge.RegisterHandlersWithAgent (session.list/get/activate, message.send, session.interrupt, instance.ping)
    ← dbproxy.RegisterHandlers (db.write)
  bridge.Bridge → pubsub events → ZMQ PUB

Secondary / Observer instance:
  remoteview.RemoteSession
    → ipc.Client.SubscribeTo(primary.PubAddr) → live event channel
    → ipc.Client.Call(primary.RPCAddr, "state.sync") → bootstrap mirror

  remoteview.RemoteControl
    → ipc.Client.Call(primary.RPCAddr, "message.send") → triggers agent on primary
    → ipc.Client.Call(primary.RPCAddr, "session.interrupt") → cancels LLM on primary

  REST API (Web-UI consumers):
    GET  /api/v1/instances           → instanceregistry.List()
    GET  /api/v1/instances/{id}      → instanceregistry.Get(id)
    GET  /api/v1/instances/{id}/stream → SSE proxy of remote PUB
    POST /api/v1/instances/{id}/sessions/{sid}/message → RemoteControl.SendMessage
```
