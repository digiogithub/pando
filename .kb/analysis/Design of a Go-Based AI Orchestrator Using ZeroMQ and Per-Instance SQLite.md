# Design of a Go-Based AI Orchestrator Using ZeroMQ and Per-Instance SQLite

## 1. Overview

This document proposes an architecture for an AI orchestrator implemented in Go that coordinates multiple sub‑agents via ZeroMQ and maintains state using per‑instance SQLite databases. It targets long‑running, tool‑heavy AI workflows where LLM context and generation limits require durable orchestration, event‑driven coordination, and local‑first state management.[^1][^2]

The design assumes: (a) a Go binary for the orchestrator, (b) Go binaries for sub‑agents, (c) ZeroMQ for IPC and message routing, and (d) one SQLite database per agent instance, colocated with that instance’s working directory.[^3][^4][^5]

## 2. High-Level Architecture

At a high level, the system follows a microservice‑style architecture decoupled by an event bus implemented on top of ZeroMQ.[^6][^5]

- **Orchestrator service**: Maintains the global view of runs, tasks, dependencies, and policies; makes routing and planning decisions.
- **Sub‑agent services**: Specialized workers (code, QA, documentation, refactor, research, etc.) that execute tasks with bounded scopes and use local tools/MCP servers.
- **ZeroMQ event bus**: A central broker or small cluster that provides PUB/SUB and REQ/REP semantics for events and RPC‑like calls between orchestrator and agents.[^3][^6]
- **Per‑instance SQLite**: Each agent and the orchestrator have their own SQLite DB file in the instance’s working directory, used for durable local state and projections of the event stream.[^4][^7][^8]
- **Durable workflow layer (optional but recommended)**: A Temporal or similar workflow platform wraps orchestrator logic for long‑running runs, retries, and exact‑once semantics.[^9][^2][^10][^1]

This separation allows pure Go processes to be stateless in memory (crash‑safe) while treating ZeroMQ and SQLite as the backbone for communication and persistence.

## 3. ZeroMQ as Event Bus

### 3.1 Event Bus Topology

The event bus is implemented as a separate process that exposes three logical capabilities over ZeroMQ:

- **PUB/SUB topics** for broadcast‑style events (task status changes, heartbeats, run lifecycle, observability).[^6][^3]
- **ROUTER/DEALER or REQ/REP** sockets for direct request‑response calls (e.g., synchronous RPC‑like invocations from orchestrator to a specific agent).
- **Snapshot/clone channel** for late‑joining clients to obtain the current key‑value state of selected topics, based on ZeroMQ’s Clone/Clustered Hashmap pattern.[^6]

The ZeroMQ event‑bus pattern keeps services decoupled, letting new agents or tools join and subscribe to relevant topics without tight integration.[^5][^6]

### 3.2 Event Types and Topics

Define canonical topics with structured payloads (e.g., JSON or MsgPack). Example topic taxonomy:

- `orchestrator.run.created`
- `orchestrator.run.updated`
- `orchestrator.task.created`
- `orchestrator.task.assigned`
- `agent.task.started.{agentType}`
- `agent.task.completed.{agentType}`
- `agent.task.failed.{agentType}`
- `agent.heartbeat.{agentId}`
- `agent.log.{agentId}`

Events share a common envelope:

```json
{
  "event_id": "uuid",
  "event_type": "agent.task.completed.code",
  "run_id": "uuid",
  "task_id": "uuid",
  "agent_id": "agent-code-1",
  "timestamp": "2026-05-05T22:00:00Z",
  "payload": { ... }
}
```

Metadata such as correlation IDs, attempt counts, and causal links can be included in headers to support tracing and retries.[^11][^6]

### 3.3 RPC over ZeroMQ

For synchronous interactions (e.g., orchestrator querying an agent for its capabilities or sending a blocking command), use a REQ/REP or DEALER/ROUTER pair separate from the PUB/SUB channels.[^12][^3]

- Orchestrator sends a request to a logical address (`agent-code-1`) via ROUTER.
- The agent’s DEALER socket receives the message, executes the requested action, and replies with a structured response.
- A timeout and retry policy ensures robustness if an agent is unavailable.

This dual pattern (event bus + RPC) matches common ZeroMQ event‑bus implementations used in microservice architectures.[^13][^5]

## 4. Per-Instance SQLite as Local State Store

### 4.1 Rationale and Role

Each process (orchestrator, agent) maintains a local SQLite database file for its persistent state. This aligns with local‑first and microservice patterns where SQLite serves as an embedded, low‑latency data store per service or per instance.[^14][^8][^4]

Benefits include:

- Fast local queries and snapshots of relevant state.
- Simple, file‑based persistence without external DB dependencies.
- Clear ownership: each binary “owns” its schema and data; cross‑service interactions flow through the event bus.

### 4.2 Orchestrator Schema

The orchestrator’s SQLite DB maintains the global view of runs and tasks as a projection of the event stream.

Example tables:

- `runs(run_id PRIMARY KEY, goal, status, created_at, updated_at, spec_json)`
- `tasks(task_id PRIMARY KEY, run_id, parent_task_id, type, status, agent_type, retries, created_at, updated_at, payload_json)`
- `events(seq INTEGER PRIMARY KEY AUTOINCREMENT, event_id, event_type, run_id, task_id, timestamp, raw_json)`
- `artifacts(artifact_id PRIMARY KEY, task_id, path, mime_type, metadata_json)`
- `metrics(run_id, task_id, agent_id, tokens_in, tokens_out, cost, duration_ms, PRIMARY KEY(run_id, task_id, agent_id))`

The orchestrator listens on PUB/SUB for all relevant events, appends them to `events`, and maintains materialized views or summary tables for quick scheduling decisions.[^7][^4]

### 4.3 Agent Schema

Each agent’s SQLite DB focuses on its local responsibilities, typically:

- `agent_state(agent_id PRIMARY KEY, capabilities_json, last_seen_at, status)`
- `assigned_tasks(task_id PRIMARY KEY, run_id, status, input_json, output_json, attempts, last_error_text)`
- `local_artifacts(artifact_id PRIMARY KEY, task_id, path, metadata_json)`
- `logs(seq INTEGER PRIMARY KEY AUTOINCREMENT, level, message, timestamp, context_json)`

This allows an agent to recover from crashes, re‑attach to the event bus, and resume task processing based on its local DB, using `assigned_tasks` as canonical truth for work it believes it owns.[^15][^4]

### 4.4 Concurrency and Single-Writer Pattern

SQLite’s concurrency model is optimized for many readers and a small number of writers, often a single writer per process.[^14]

- Each binary should have only one write path (e.g., a dedicated internal component) that executes DB transactions.
- Reads can be concurrent (within the process) and are snapshot‑isolated.
- If external components need to read the state, expose APIs or query endpoints instead of sharing the DB file.

This approach mirrors how production systems use SQLite for event processing or local‑first apps.[^4][^14]

## 5. Orchestration and Workflow Logic

### 5.1 Run Lifecycle and Planning

For each high‑level goal, the orchestrator creates a `run` record and publishes `orchestrator.run.created`. It then uses an LLM‑backed planner (or classical heuristics) to generate a task graph, writing rows into `tasks` and emitting `orchestrator.task.created` for each task.[^2][^1]

The planner can run as a sub‑agent or as a dedicated service; the key is that its output (task graph, dependencies, acceptance criteria) is persisted in SQLite and propagated via events.[^10][^1]

### 5.2 Task Scheduling and Routing

A scheduler loop in the orchestrator queries `tasks` for tasks in `ready` state whose dependencies are satisfied and not currently assigned. It then:

1. Selects an appropriate agent type and specific agent instance based on `agent_state` and `capabilities_json`.
2. Updates the task row (status `assigned`, `agent_type`, etc.) within a transaction.
3. Publishes an `orchestrator.task.assigned` event targeted at the chosen agent.

Agents subscribe to task‑assignment topics and insert the task into their `assigned_tasks` table, transitioning to `running` and publishing `agent.task.started`.[^11][^5]

### 5.3 Agent Execution and Reporting

When an agent receives a task assignment event, it:

- Writes the task into `assigned_tasks` with status `pending`.
- Launches a worker routine that performs the LLM calls, tool/MCP usage, and any side‑effects in its working directory.
- Periodically emits `agent.heartbeat` events with progress and metrics.
- On completion or failure, updates `assigned_tasks` and publishes `agent.task.completed` or `agent.task.failed` with structured outputs.

The orchestrator consumes these events, updates its `tasks` table, and evaluates which downstream tasks are now unblocked.[^1][^2]

### 5.4 Durable Execution with Temporal (Optional)

To achieve full durability, exactly‑once semantics, and robust retries for long‑running runs, wrap the orchestrator logic in a Temporal workflow.[^9][^2][^10][^1]

- **Temporal Workflow**: Represents one run’s lifecycle; it does not directly call LLMs or tools but orchestrates them via Activities.
- **Temporal Activities**: Thin wrappers that delegate to the orchestrator binary or directly publish commands onto the ZeroMQ bus and wait for specific events.
- **Event History**: Temporal stores the workflow’s decisions and event history, allowing replay after crashes without recomputing past planning or routing decisions.[^2][^11]

This pattern has been demonstrated for agentic AI systems where Temporal coordinates complex LLM workflows and tool calls.[^10][^9][^1]

## 6. Heartbeats, Health, and Liveness

### 6.1 Agent Heartbeats

Each agent periodically publishes `agent.heartbeat.{agentId}` containing:

- `agent_id`
- `status` (healthy, draining, overloaded)
- `current_tasks`, `queue_length`
- `last_error` (optional)

The orchestrator consumes these events and updates `agent_state` in its DB, allowing the scheduler to avoid unhealthy agents and to implement back‑pressure.[^5][^3]

### 6.2 Task Timeouts and Retries

The orchestrator enforces timeouts based on task type and run policies. For example:

- If `agent.task.started` is seen but no completion event arrives within `task_timeout`, the orchestrator marks the task as `timed_out` and either retries on another agent or escalates.[^1][^11]
- Retry counts and back‑off policies are stored in `tasks.retries` and applied deterministically.

With Temporal integration, these retry and timeout policies can be enforced at the workflow level, with Activities representing calls to agents or tools.[^9][^2]

## 7. Consistency, Recovery, and Idempotency

### 7.1 Event Sourcing and Projections

The combination of the ZeroMQ event bus and local SQLite DBs lends itself to an event‑sourcing style:

- The event bus is the primary channel for domain‑level events.
- Each service stores an append‑only `events` table for its view.
- Other tables (`runs`, `tasks`, `agent_state`) are projections derived from events.

After a crash, a service can:

1. Drop and rebuild projections from `events`, or
2. Request a snapshot from the event bus (if the Clone protocol is implemented), then replay recent events.[^3][^6]

### 7.2 Idempotent Handlers

Each event handler must be idempotent, typically enforced by:

- Using `event_id` as a unique constraint in the `events` table.
- Ignoring events that already exist.
- Making updates conditional on current status (e.g., only transition `pending -> running`, not from any status).[^11][^6]

For commands sent via RPC (REQ/REP), include a `command_id` and ensure that repeated commands with the same ID do not produce duplicate side‑effects.

### 7.3 Recovery Scenarios

Examples:

- **Agent crash**: On restart, the agent reads `assigned_tasks` with status `running` or `pending` and re‑emits `agent.heartbeat` and `agent.task.started` for tasks it intends to resume or retry, or marks them as `failed` with a specific reason.
- **Orchestrator crash**: On restart, the orchestrator replays events from the bus (or reads its `events` table) to reconstruct `runs` and `tasks`, then re‑evaluates which tasks are `ready` and should be scheduled again.[^2][^1]

## 8. Observability and Tooling

The architecture exposes multiple hooks for observability:

- The event log in SQLite is queryable for debugging and analytics.
- Metrics tables capture tokens, cost, durations, and tool calls per task and per agent.
- ZeroMQ allows plugging in a monitoring socket or proxy that records raw traffic.
- With Temporal, workflow histories and Activity logs provide another rich source of insights for agent runs.[^10][^9][^11]

Traces can be correlated via `run_id`, `task_id`, `agent_id`, and `event_id` across databases and logs.

## 9. Security and Multi-Host Extensions

While the base design focuses on one host, it extends naturally to multiple hosts:

- ZeroMQ endpoints can be bound to TCP interfaces instead of or in addition to IPC.
- Service discovery (e.g., via mDNS or registry service) can let agents and orchestrator locate the event bus.[^5]
- TLS or mTLS can secure connections between hosts, with instance identities represented in certificates.

SQLite remains per‑instance and local; shared state across hosts is communicated via the event bus or, at higher scale, through an additional replicated store or workflow engine backend.[^8][^11]

## 10. Summary of Key Design Choices

- **ZeroMQ‑based event bus** decouples orchestrator and agents, enabling flexible topologies and patterns (PUB/SUB, REQ/REP, snapshot/clone).[^3][^6][^5]
- **Per‑instance SQLite** provides lightweight, robust, local persistence for each process, well aligned with local‑first and microservice patterns, provided a single‑writer discipline is followed.[^8][^4][^14]
- **Event‑sourced projections** in SQLite ensure the system can rebuild state after crashes and keep a full audit trail of decisions.
- **Optional Temporal integration** turns the orchestrator into a durable workflow engine with exact‑once semantics, retries, and rich observability for long‑running AI agent runs.[^9][^1][^2][^10]

This combination allows a Go‑based AI orchestrator to coordinate complex multi‑agent workflows while remaining crash‑safe, observable, and extensible, without depending on a large centralized database from the outset.

---

## References

1. [Durable AI Agents: Orchestrating the Future with Fred and Temporal](https://fredk8.dev/blog/durable-ai-agents-orchestrating-the-future-with-fred-and-temporal/) - How Fred leverages Temporal.io to provide durable execution for long-running agents, ensuring reliab...

2. [Of course you can build dynamic AI agents with Temporal](https://temporal.io/blog/of-course-you-can-build-dynamic-ai-agents-with-temporal) - Temporal can absolutely handle your AI agents. This guide shows you how Temporal Workflows and Activ...

3. [micro-toolkit/event-bus-zeromq - GitHub](https://github.com/micro-toolkit/event-bus-zeromq) - The ØMQ implementation of event BUS will be based in the Clone Pattern, present in the ØMQ guid and ...

4. [Local-First State Management With SQLite - PowerSync](https://www.powersync.com/blog/local-first-state-management-with-sqlite) - Using a local SQLite database for state management is a quickly-evolving new approach, and over time...

5. [Microservices Architecture — Early Thoughts Before That First Step](https://codeburst.io/microservices-architecture-early-thoughts-before-that-first-step-fecc2ef9d64) - The five listed systems (services) were completely decoupled from one another by what we called the ...

6. [RFC.md - micro-toolkit/event-bus-zeromq - GitHub](https://github.com/micro-toolkit/event-bus-zeromq/blob/master/RFC.md) - The Event BUS Protocol defines mechanisms for sharing events across a set of clients. It allows clie...

7. [Local-first architecture with Expo](https://docs.expo.dev/guides/local-first/) - Expo SQLite is a SQLite library that is a great choice for persistence for local-first apps. You can...

8. [SQLite for Microservices, Lightweight Databases Explained](https://www.sqliteforum.com/p/sqlite-for-microservices) - Learn how SQLite fits into modern microservices. Patterns for isolated data storage, fast local acce...

9. [Temporal + AI Agents: The Missing Piece for Production-Ready ...](https://dev.to/akki907/temporal-workflow-orchestration-building-reliable-agentic-ai-systems-3bpm) - This article explores Temporal's core concepts, workflow patterns, and how it's revolutionizing the ...

10. [Temporal and OpenAI Launch AI Agent Durability with Public ... - InfoQ](https://www.infoq.com/news/2025/09/temporal-aiagent/) - Temporal has unveiled a public preview integration with the OpenAI Agents SDK, introducing durable e...

11. [Durable Workflow Platforms for AI Agents and LLM Workloads](https://render.com/articles/durable-workflow-platforms-ai-agents-llm-workloads) - Compare durable workflow platforms for AI agents and LLM-powered applications. Evaluate Temporal, In...

12. [Implementing a message bus using ZeroMQ - sockets - Stack Overflow](https://stackoverflow.com/questions/24621337/implementing-a-message-bus-using-zeromq) - For this, I am using ZeroMQ over TCP. The pattern is PUB-SUB with a forwarder. My bus runs as a sepa...

13. [GitHub - dano/vertx-zeromq: A ZeroMQ Event Bus bridge for Vert.x 3.x.](https://github.com/dano/vertx-zeromq) - A ZeroMQ Event Bus bridge for Vert.x 3.x. Contribute to dano/vertx-zeromq development by creating an...

14. [Single-writer Database Architecture with SQLite - Bugsink](https://www.bugsink.com/blog/database-transactions/) - TL;DR: Bugsink uses a single-writer architecture to keep database state consistent and predictable. ...

15. [Offline-First Architecture in Flutter: Part 1 — SQLite Local Storage ...](https://dev.to/anurag_dev/implementing-offline-first-architecture-in-flutter-part-1-local-storage-with-conflict-resolution-4mdl) - Part 1 (this post): Setting up local storage with SQLite, implementing conflict resolution strategie...

