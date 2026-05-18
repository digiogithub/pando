# Phase 6 — Observability & Diagnostics

**Date:** 2026-05-18
**Parent plan:** `pando/plans/unified_single_writer_master_plan.md`
**Status:** not started
**Risk:** low
**Effort:** small

## 1. Goal

Add structured logging, metrics, and diagnostic endpoints that make the single-writer IPC topology observable and debuggable in production.

## 2. Why

With multiple instances, primary/secondary roles, and IPC channels between them, operators and developers need to:

- Know which instance is the current writer for a given path
- See write throughput and latency
- Detect stuck queues or unreachable primaries
- Trace individual writes across instances

## 3. Design

### 3.1 Structured Logging

All IPC-related log messages should use consistent key-value pairs:

| Key | Meaning | Example |
|---|---|---|
| `role` | Instance role | `primary`, `secondary` |
| `workdir` | Project path | `/home/user/projects/foo` |
| `pub_port` | PUB socket port | `40001` |
| `rpc_port` | ROUTER socket port | `40002` |
| `primary_rpc` | Primary's RPC address (secondary) | `tcp://127.0.0.1:40002` |
| `write_method` | DB write method being executed | `CreateSession` |
| `write_latency_ms` | Write operation latency | `12.4` |
| `queue_depth` | Coordinator queue depth | `3` |
| `source_instance` | Instance that initiated the write | `uuid` |
| `request_id` | Unique write request ID | `req-123456` |

Example log entry for a proxied write:

```
IPC: write proxied role=secondary workdir=/proj/foo write_method=CreateSession
     write_latency_ms=8.2 primary_rpc=tcp://127.0.0.1:40002 request_id=req-123456
```

### 3.2 Instance Introspection (CLI)

Add diagnostic flags to the root command:

```bash
# Show IPC status of the current instance
pando ipc status

# Output example:
# Role:          primary
# Workdir:       /proj/foo
# Instance ID:   abc123...
# PID:           12345
# PUB port:      40001 (bound)
# RPC port:      40002 (bound)
# Coordinator:   active (queue: 0/256)
# Writes:        42 accepted, 42 completed, 0 failed
# Secondary clients: 1 connected

# List all instances for a path
pando ipc instances --path /proj/foo

# Output example:
# INSTANCE ID  ROLE        PID    PUB    RPC    STARTED
# abc123...    primary     12345  40001  40002  2026-05-18T10:00:00Z
# def456...    secondary   12346  -      -      2026-05-18T10:05:00Z
```

### 3.3 API Endpoint: `GET /api/ipc/status`

Return JSON with same information for Web-UI and external tooling:

```json
{
  "role": "primary",
  "workdir": "/proj/foo",
  "instance_id": "abc123...",
  "pid": 12345,
  "pub_port": 40001,
  "rpc_port": 40002,
  "coordinator": {
    "active": true,
    "queue_depth": 3,
    "max_queue": 256,
    "accepted": 42,
    "completed": 42,
    "failed": 0
  },
  "route_table": [
    {
      "instance_id": "def456...",
      "role": "secondary",
      "connected_at": "2026-05-18T10:05:00Z"
    }
  ]
}
```

Primary instances expose this. Secondaries can call it on the primary via RPC.

### 3.4 Metrics (Prometheus-compatible or internal)

Expose key metrics:

| Metric | Type | Description |
|---|---|---|
| `pando_ipc_writes_total{method,status}` | Counter | Writes by method and success/failure |
| `pando_ipc_write_latency_ms{method}` | Histogram | Write latency per method |
| `pando_ipc_queue_depth` | Gauge | Current coordinator queue depth |
| `pando_ipc_role` | Gauge | 1 = primary, 0 = secondary |
| `pando_ipc_connected_instances` | Gauge | # of connected secondary instances |

Add a `/api/metrics` endpoint if not already present, or integrate with an existing metrics system.

### 3.5 WriteCoordinator Metrics (Phase 3 integration)

The `Coordinator` already tracks `Accepted`, `Completed`, `Failed`, `QueueDepth`. Expose them:

```go
func (c *Coordinator) Metrics() CoordinatorMetrics {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return CoordinatorMetrics{
        Accepted:   c.Accepted,
        Completed:  c.Completed,
        Failed:     c.Failed,
        QueueDepth: c.QueueDepth,
        MaxQueue:   c.maxQueue,
    }
}
```

### 3.6 Tracing with RequestID

Every `WriteRequest` carries a `WriteMeta.RequestID`. This request ID should propagate through:

1. Secondary DBProxy → writes it into `WriteMeta.RequestID`
2. Primary handler → logs it when dispatching
3. Coordinate → includes it in metrics/logs
4. Back to secondary → response includes a `request_id` field

This enables end-to-end tracing of a single write across instances.

## 4. Acceptance Criteria

- [ ] Consistent structured logging for all IPC events
- [ ] `pando ipc status` CLI command
- [ ] `pando ipc instances --path X` CLI command
- [ ] `GET /api/ipc/status` endpoint (primary only)
- [ ] Coordinator metrics exposed via API
- [ ] RequestID tracing propagated end-to-end
- [ ] Logs are grep-able for troubleshooting (`role=primary`, `write_method=CreateSession`)

## 5. Risks

- **Low risk.** Purely additive. No changes to behaviour.

## 6. Dependencies

- Phase 1 (unified bootstrap) — so we know role/workdir/ports consistently
- Phase 3 (serialisation loop) — for coordinator metrics

## 7. Estimated effort

2-3 days.
