# Communication Protocol over ZeroMQ (pure Go) for Subagent Orchestration and Real-Time Session Monitoring

## 1. Purpose and Scope

This document defines a high-level communication protocol for orchestrating subagents ("workers" or "agents") and monitoring all sessions in real time, using ZeroMQ implemented in pure Go via `github.com/go-zeromq/zmq4`.[^1][^2]
The protocol is designed for:

- Communication between orchestrator and subagents (control, work, heartbeats, logs).
- Broadcasting events to a desktop or web UI view (per project and per session).
- Simple integration of JSON-RPC 2.0 as the message layer for commands and responses.[^3][^4]

Each **project** (repository/workspace) is assumed to have its own local orchestrator, and each orchestrator can manage multiple **sessions** and multiple **subagents** in parallel.

## 2. Overall Architecture

The architecture is organized around an internal **ZeroMQ bus** on the host, using several combined patterns:

- **REQ/REP** for synchronous commands orchestrator ↔ subagent (for example, `startTask`, `stopTask`).[^5]
- **DEALER/ROUTER** to multiplex many sessions and subagents through a single orchestrator socket.[^6][^5]
- **PUB/SUB** to broadcast events to UIs (desktop and web) and allow any observer to subscribe to the state of projects/sessions.[^7][^5]

Using DEALER/ROUTER allows the orchestrator to act as an asynchronous message broker, while REQ/REP (or DEALER/ROUTER with message conventions) supports RPC-style calls.[^8][^6]

### 2.1 Logical Topology

Main components:

- **Project orchestrator**: one process per project; maintains the table of sessions, subagents, and UI clients.
- **Subagent**: worker process that runs tasks, exposed as a ZeroMQ endpoint.
- **UI Client**:
  - Desktop (native app or integrated into an editor).
  - Web UI (SPA that connects via WebSocket to a backend gateway → ZeroMQ).

### 2.2 ZeroMQ Patterns

| Communication                 | ZMQ Pattern     | Involved Sockets                            |
|------------------------------|-----------------|---------------------------------------------|
| Orchestrator ↔ Subagent      | DEALER↔ROUTER   | Orchestrator = ROUTER, Subagent = DEALER    |
| Orchestrator → UIs (events)  | PUB/SUB         | Orchestrator = PUB, UI clients = SUB        |
| UI → Orchestrator (commands) | REQ/REP or DEALER/ROUTER | UI = DEALER/REQ, Orchestrator = ROUTER/REP |

The choice of DEALER/ROUTER allows multiplexing many sessions and subagents through a single pair of sockets, with explicit identification via the ZeroMQ identity frame.[^9][^6]

## 3. Transport Layer with `go-zeromq/zmq4`

The implementation uses `github.com/go-zeromq/zmq4`, a pure-Go implementation of ZeroMQ 4 that supports `REQ`, `REP`, `DEALER`, `ROUTER`, `PUB`, `SUB`, `PUSH`, and `PULL` sockets.[^2][^1]

### 3.1 Local vs TCP Transport

To simplify the cross-platform implementation and facilitate debugging, the protocol is initially defined over **TCP on localhost**:

- Typical endpoints: `tcp://127.0.0.1:4XXX`.
- Security is delegated to host isolation and the upper layer (UI client authentication).

Future iterations can add support for `ipc://` transport when `zmq4` has the required level of IPC support, leveraging the operating system's native IPC mechanisms.[^10][^1]

### 3.2 Basic Socket Examples in Go

Simplified example of an orchestrator PUB socket:

```go
pub := zmq4.NewPub(ctx)
defer pub.Close()

if err := pub.Listen("tcp://127.0.0.1:4100"); err != nil {
    log.Fatalf("could not listen: %v", err)
}

// Send event
msg := zmq4.NewMsg([]byte("session.update"), payloadBytes)
_ = pub.Send(msg)
```

Simplified example of a SUB socket in a UI:

```go
sub := zmq4.NewSub(ctx)
defer sub.Close()

if err := sub.Dial("tcp://127.0.0.1:4100"); err != nil {
    log.Fatalf("could not dial: %v", err)
}

_ = sub.SetOption(zmq4.OptionSubscribe, "session.") // topic prefix

for {
    msg, err := sub.Recv()
    if err != nil { break }
    topic := string(msg.Frames)
    payload := msg.Frames[^1]
    // deserialize JSON payload
}
```

This pattern aligns with the pub/sub examples provided in `go-zeromq/zmq4` and the ZeroMQ Guide.[^11][^5]

## 4. Message Layer: JSON-RPC 2.0

On top of the ZeroMQ data frames, the protocol defines a JSON-RPC 2.0 layer for commands and responses, leveraging the fact that JSON-RPC is transport-agnostic and can operate over any message channel.[^4][^3]

### 4.1 JSON-RPC Message Structure

The protocol uses the standard JSON-RPC 2.0 specification:[^3]

- `jsonrpc`: always "2.0".
- `method`: remote method name (for example, `agent.startTask`).
- `params`: object or array with parameters.
- `id`: unique call identifier (for request/response correlation).

Example **request**:

```json
{
  "jsonrpc": "2.0",
  "id": "f83a2cf6-3b2c-4bde-a4c1-b7d412e4db43",
  "method": "agent.startTask",
  "params": {
    "projectId": "proj-123",
    "sessionId": "sess-456",
    "agentId": "mesnada-lint",
    "payload": { "path": "cmd/api" }
  }
}
```

Example **response**:

```json
{
  "jsonrpc": "2.0",
  "id": "f83a2cf6-3b2c-4bde-a4c1-b7d412e4db43",
  "result": {
    "status": "accepted",
    "taskId": "task-789"
  }
}
```

In case of error, the standard JSON-RPC `error` object is used:[^3]

```json
{
  "jsonrpc": "2.0",
  "id": "f83a2cf6-3b2c-4bde-a4c1-b7d412e4db43",
  "error": {
    "code": -32001,
    "message": "Agent not available",
    "data": { "agentId": "mesnada-lint" }
  }
}
```

## 5. ZeroMQ Envelope and Session Metadata

To fully control all sessions and subagents in real time, the ZeroMQ layer uses **envelopes** that separate routing addresses from the application payload.[^5][^6]

### 5.1 Envelope for ROUTER/DEALER

On a ROUTER socket, every received message has the form:

- Frames 0..N−2: return address frames (identity frames).
- Frame N−1: empty frame (separator).
- Frames N..M: application payload frames.

To simplify, this protocol defines a single identity frame followed by the JSON-RPC payload:

- Frame 0: `identity` (bytes).
- Frame 1: JSON payload (bytes).

The `identity` is used in the orchestrator to map to an `agentId` or `sessionId`.

### 5.2 Metadata in the Payload

In addition to JSON-RPC fields, `params` always include logical routing information:

- `projectId`: project identifier.
- `sessionId`: session (chat/work) identifier.
- `agentId`: logical subagent identifier.

This allows the UI to filter and group events by project, session, and subagent.

## 6. Session and Subagent Model

### 6.1 Identifiers

The following stable identifiers are defined:

- `projectId`: unique project string (e.g., a hash of the repository path).
- `sessionId`: UUID per project work/chat thread.
- `agentId`: subagent name (e.g., `mesnada-lint`, `mesnada-refactor`).
- `taskId`: internal identifier for long-running tasks.

### 6.2 Orchestrator State Table

The orchestrator keeps an in-memory state table:

- `projects[projectId]` → `{ sessions, agents }`.
- `sessions[sessionId]` → `{ status, currentTask, lastActivity, agents[] }`.
- `agents[agentId]` → `{ status, lastHeartbeat, capabilities }`.

Every relevant change in this table produces a PUB event toward the UIs.

## 7. Protocol Message Types

Although the transport layer is generic JSON-RPC, the system defines a standard set of methods.

### 7.1 Orchestrator → Subagent Methods

- `agent.startTask`
  - Params: `{ projectId, sessionId, agentId, payload }`.
  - Result: `{ status, taskId, meta? }`.
- `agent.cancelTask`
  - Params: `{ taskId }`.
  - Result: `{ status }`.
- `agent.shutdown`
  - Params: `{ agentId }`.
  - Result: `{ status }`.

### 7.2 Subagent → Orchestrator Methods

- `agent.taskProgress`
  - Notification (no `id`) sent by the subagent.
  - Params: `{ projectId, sessionId, taskId, progress, message, meta? }`.
- `agent.taskResult`
  - Final notification.
  - Params: `{ projectId, sessionId, taskId, status, result, meta? }`.
- `agent.heartbeat`
  - Params: `{ agentId, ts, load, capabilities? }`.

On the ZeroMQ side, these methods are all sent as JSON-RPC payloads; for notifications, the `id` field is omitted as per JSON-RPC specification.[^3]

### 7.3 PUB Events for UIs

On the PUB/SUB channel, topics follow this pattern:

- `session.update` → session state changes.
- `agent.update` → subagent state changes.
- `task.update` → task progress.
- `log.event` → structured logging events.

The payload of these messages is a JSON object:

```json
{
  "projectId": "proj-123",
  "sessionId": "sess-456",
  "agentId": "mesnada-lint",
  "eventType": "task.progress",
  "timestamp": "2026-05-05T21:43:00Z",
  "data": { /* event specific */ }
}
```

## 8. Real-Time Session Control

The UI (desktop or web) maintains a SUB connection to the orchestrator and optionally a REQ/DEALER connection for sending commands.[^7][^5]

### 8.1 UI Subscription

The UI can apply topic filters to minimize traffic:

- `session.update` to see global session changes.
- `task.update` to render progress bars.
- `agent.update` to power a subagent health panel.

On top of the payload, the UI can further filter by `projectId` and `sessionId` to display only the active project.

### 8.2 Initial Synchronization

When a UI connects, it sends a JSON-RPC command to the orchestrator:

- `ui.syncState`
  - Params: `{ projectId? }`.
  - Result: `{ sessions: [...], agents: [...], tasks: [...] }`.

The UI uses this response to build the initial state and then stays up-to-date through PUB events.

## 9. Integration with Mesnada Subagents

For Mesnada subagents (or similar agents), a thin adapter exposes the same JSON-RPC protocol over ZeroMQ.

### 9.1 Transport Adapter

Each Mesnada subagent runs a small server that:

- Listens on a DEALER endpoint (`zmq4.NewDealer`).[^2]
- Connects to the orchestrator ROUTER socket.
- Reads JSON-RPC messages from ZeroMQ and adapts them to the internal Mesnada API.

This keeps ZeroMQ concerns inside the adapter, allowing the subagent to remain transport-agnostic.

### 9.2 Mesnada State Updates

When a Mesnada subagent updates its internal state (for example, model change, context change, reassignment to another session), the adapter sends:

- `agent.update` as a JSON-RPC notification to the orchestrator.

The orchestrator updates its state table and emits an `agent.update` PUB event toward the UIs.

## 10. Multiplexing and Message Correlation

### 10.1 JSON-RPC `id` Usage

The `id` field is used to correlate each request with its response according to the JSON-RPC specification.[^12][^3]

- For orchestrator → subagent calls, the orchestrator generates a unique `id`.
- The subagent returns the same `id` in the response.
- For notifications (heartbeats, progress, logs) no `id` is sent.

### 10.2 Correlation via `taskId`

For long-running tasks, `taskId` travels inside `params` and is the canonical domain-level identifier.

- `agent.startTask` returns `taskId`.
- `agent.taskProgress` and `agent.taskResult` always include `taskId`.
- The UI can use `taskId` to group messages and draw timelines.

## 11. Error Handling and Reconnection

### 11.1 JSON-RPC-Level Errors

Standard JSON-RPC error codes are used for protocol- and method-level failures:[^3]

- `-32601` (Method not found).
- `-32602` (Invalid params).
- `-32603` (Internal error).

Domain-specific errors use codes in the `-32000` to `-32099` range.

### 11.2 ZeroMQ Transport Errors

ZeroMQ automatically handles reconnections in patterns such as DEALER/ROUTER and PUB/SUB.[^5][^7]

At the protocol level, additional rules apply:

- If the orchestrator detects missing heartbeats from a subagent, it marks `status = "unreachable"` and emits an `agent.update` event.
- If a UI stops receiving events, it can re-run `ui.syncState` upon reconnection.

## 12. Security and Isolation

For a local development environment (similar to typical Zed/Copilot usage), the design relies on:

- Transport over `tcp://127.0.0.1` only.
- Optional authentication via shared tokens in the JSON-RPC payload (`authToken` in `params`).

If ZeroMQ is ever exposed over the network, additional mechanisms must be considered (CurveZMQ, TLS tunnels, etc.), which are out of scope for this initial design.[^5]

## 13. Key Design Decisions

- **Pure-Go ZeroMQ** (`go-zeromq/zmq4`) as the transport foundation, avoiding CGO and simplifying cross-platform builds.[^1][^2]
- **ROUTER/DEALER + PUB/SUB patterns** for multiplexing sessions/subagents and broadcasting to UIs, following best practices from the ZeroMQ Guide.[^6][^7][^5]
- **JSON-RPC 2.0** as the message layer, leveraging its simplicity, transport-agnostic design, and request/response model with `id` correlation.[^4][^3]
- **Domain metadata** (`projectId`, `sessionId`, `agentId`, `taskId`) embedded in `params` and PUB events to enable real-time, project/session-centric views.
- **Adapter for Mesnada subagents** that translates ZeroMQ+JSON-RPC into the internal API, keeping agents isolated from transport details.

This protocol provides a solid foundation for implementing a unified desktop or web view that controls and observes, in real time, the state of all sessions and subagents across multiple projects, with an efficient transport and flexible messaging patterns inspired by ZeroMQ best practices.

---

## References

1. [GitHub - go-zeromq/zmq4: [WIP] Pure-Go implementation of ZeroMQ-4](https://github.com/go-zeromq/zmq4) - zmq4 is a pure-Go implementation of ØMQ (ZeroMQ), version 4. See zeromq.org for more informations. D...

2. [zmq4 package - github.com/go-zeromq/zmq4 - Go Packages](https://pkg.go.dev/github.com/go-zeromq/zmq4) - Package zmq4 implements the ØMQ sockets and protocol for ZeroMQ-4. For more informations, see http:/...

3. [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification) - JSON-RPC is a stateless, light-weight remote procedure call (RPC) protocol. Primarily this specifica...

4. [JSON-RPC 2.0 (prior document, seek updated document)](https://jsonrpc.org/historical/json-rpc-1-2-proposal.html) - The use of Null for id in Requests is discouraged, because this specification uses an id of Null for...

5. [Chapter 2 - Sockets and Patterns - ZeroMQ Guide](https://zguide.zeromq.org/docs/chapter2/) - In Chapter 1 - Basics we took ZeroMQ for a drive, with some basic examples of the main ZeroMQ patter...

6. [Chapter 3 - Advanced Request-Reply Patterns - ZeroMQ Guide](https://zguide.zeromq.org/docs/chapter3/) - A request-reply exchange consists of a request message, and an eventual reply message. In the simple...

7. [Chapter 5 - Advanced Pub-Sub Patterns - ZeroMQ Guide](https://zguide.zeromq.org/docs/chapter5/) - In this chapter we'll focus on publish-subscribe and extend ZeroMQ's core pub-sub pattern with highe...

8. [ZeroMQ mixed PUB/SUB DEALER/ROUTER pattern - Stack Overflow](https://stackoverflow.com/questions/35528407/zeromq-mixed-pub-sub-dealer-router-pattern) - DEALER/ROUTER is used to make a proxy between REQ/REP nodes, not really the kind of thing for sendin...

9. [Router-Dealer | Bonsai.ZeroMQ](https://bonsai-rx.org/zeromq/articles/router-dealer.html) - In the Request-Response pattern we typically have one client sending requests to a single server. Ho...

10. [zmq4: pure-Go ZeroMQ-4 package : r/golang - Reddit](https://www.reddit.com/r/golang/comments/8qvqn1/zmq4_purego_zeromq4_package/) - I've just pushed a few examples for my pure-Go implementation of ZeroMQ-4 sockets. Implemented so fa...

11. [Pure zmq4 based on golang 오픈소스를 활용하여 PUB/SUB 테스트](https://rection34.tistory.com/83) - Pure zmq4 based on golang 오픈소스를 활용하여 PUB/SUB을 테스트 Publish golang 소스코드 작성 Subscription golang 소스코드 작성...

12. [JSON RPC - What is the "id" for? - Stack Overflow](https://stackoverflow.com/questions/2210791/json-rpc-what-is-the-id-for) - JSON RPC 1.0: The request id. This can be of any type. It is used to match the response with the req...

