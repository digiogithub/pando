# Pando Unified Single-Writer SQLite + IPC Architecture — Master Implementation Plan

**Date:** 2026-05-18
**Status:** draft proposal
**Author:** Pando / Claude Sonnet 4.5

## 0. Executive Summary

This plan unifies Pando's inter-instance SQLite write model across **all entrypoints** (`root`/TUI, `serve`, `app`, `desktop`, `acp`), enforcing a single invariant:

> **One SQLite writer per project path. The first instance to start in a path is the primary (sole writer). All later instances become secondaries: they read locally, write through IPC.**

The plan is structured in 7 phases, ordered by dependency and risk.

## 1. Current State (as of 2026-05-18)

### What works (implemented in Phase 4)

| Feature | Status |
|---|---|
| `flock`-based primary election | ✅ `internal/ipc/lock.go` → `AcquireLock()` |
| Port derivation per path | ✅ `ipc.PortsForPath(cwd)` (FNV-32a) |
| Secondary read-only DB | ✅ `db.ConnectReadOnly()` (`mode=ro` URI) |
| Write proxy (`DBProxy`) | ✅ `internal/ipc/dbproxy/proxy.go` — 20 write methods |
| `db.write` RPC handler | ✅ `dbproxy.RegisterHandlers()` → `dispatchWrite()` |
| IPC Bus (PUB + ROUTER) | ✅ `internal/ipc/bus.go` |
| IPC Client (SUB + DEALER) | ✅ `internal/ipc/client.go` |
| Handler registration before `bus.Start()` | ✅ All entrypoints race-fixed |
| Instance registry announce/revoke | ✅ `internal/instanceregistry/` |

### Entrypoint consistency gap

| Entrypoint | Lock check | Uses `AcquireLock` | Secondary RO | Primary bus | Gap |
|---|---|---|---|---|---|
| `cmd/root.go` | ✅ | ✅ | ✅ | ✅ | **None** — reference implementation |
| `cmd/serve.go` | ❌ | ❌ | ❌ | ❌ (own bus) | Opens RW directly; no check for existing primary |
| `cmd/app.go` | ❌ | ❌ | ❌ | ❌ (own bus) | Opens RW directly; no check for existing primary |
| `cmd/desktop.go` | ❌ | ❌ | ❌ | ❌ (own bus) | Opens RW directly; no check for existing primary |
| `cmd/acp.go` (ACP mode) | ❌ | ❌ | ❌ | ❌ | Opens RW directly inside `runACPServerWithOptions` |

### Known limitations from Phase 4

1. `history.NewService` uses `WithTx` on read-only conn on secondary → fails silently
2. `project.NewService` uses `UpdateProjectName` outside `Querier` interface
3. No leader_watcher / automatic promotion on primary death
4. No write-change PUB propagation to secondaries after writes
5. No multi-process integration tests

## 2. Architecture Target

```
┌──────────────────────────────────────────────────────────┐
│  Host machine                                            │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  Instance A  (path /proj/foo  —  PRIMARY)         │   │
│  │  • SQLite (RW) — migrates, writes                │   │
│  │  • ZMQ Bus (PUB + ROUTER)                        │   │
│  │  • write-coordinator goroutine (serialises)      │   │
│  │  • db.write handler                              │   │
│  │  • PUB events after each write                   │   │
│  └──────────────────────────────────────────────────┘   │
│                          │                               │
│  ┌───────────────────────│───────────────────────────┐   │
│  │  Instance B  (same path — SECONDARY)               │   │
│  │  • SQLite (RO) — local reads                      │   │
│  │  • DBProxy → db.write RPC to primary              │   │
│  │  • SUB socket — receives write-change events      │   │
│  │  • Full TUI / Web-UI / App functionality          │   │
│  └───────────────────────────────────────────────────┘   │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  Instance C  (same path — SECONDARY)              │   │
│  │  • CLI non-interactive (subagent)                │   │
│  │  • SQLite (RO) — local reads                     │   │
│  │  • DBProxy → db.write RPC to primary              │   │
│  └──────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────┘
```

### Key design decisions

1. **First-instance-wins**: `flock` on `<workdir>/.pando/ipc.lock`
2. **Read locally, write remotely**: Secondary uses `dbproxy.DBProxy`
3. **Single `db.write` endpoint**: All DB mutating operations routed through one method
4. **Write serialisation loop in primary**: Optional but recommended goroutine
5. **PUB change propagation**: Primary publishes after each write for secondary cache invalidation
6. **No persistent state outside SQLite**: Instance registry is ephemeral (`/tmp`)

## 3. Implementation Phases

### Phase 1 — Unify IPC Bootstrap Across All Entrypoints
**Status:** not started | **Risk:** low | **Effort:** medium

Unify the primary/secondary role detection and DB opening logic so that `app`, `serve`, `desktop`, and `acp` modes follow the same contract as `root`.

→ **Detail document:** `pando/plans/unified_single_writer_phase1_bootstrap.md`

### Phase 2 — Formalise the Write Channel Contract
**Status:** not started | **Risk:** low | **Effort:** small

Add explicit types, timeouts, retry policies, and error semantics for the `db.write` RPC channel.

→ **Detail document:** `pando/plans/unified_single_writer_phase2_write_contract.md`

### Phase 3 — Write Serialisation Loop in Primary
**Status:** not started | **Risk:** medium | **Effort:** medium

Introduce an internal write-coordinator goroutine in the primary that serialises all write operations, improving predictability and observability.

→ **Detail document:** `pando/plans/unified_single_writer_phase3_serialisation.md`

### Phase 4 — Write-Change PUB Propagation
**Status:** not started | **Risk:** low | **Effort:** medium

After each write in the primary, publish a change event so secondaries can invalidate caches and refresh views.

→ **Detail document:** `pando/plans/unified_single_writer_phase4_pub_propagation.md`

### Phase 5 — Primary Failover & Secondary Promotion
**Status:** not started | **Risk:** high | **Effort:** large

Implement graceful primary death detection, lock re-acquisition, and automatic secondary-to-primary promotion.

→ **Detail document:** `pando/plans/unified_single_writer_phase5_failover.md`

### Phase 6 — Observability & Diagnostics
**Status:** not started | **Risk:** low | **Effort:** small

Add structured logging, metrics, and instance introspection for the single-writer topology.

→ **Detail document:** `pando/plans/unified_single_writer_phase6_observability.md`

### Phase 7 — Integration Tests (Multi-Process)
**Status:** not started | **Risk:** medium | **Effort:** medium

Write Go integration tests that spawn multiple Pando processes in the same temp directory and verify the single-writer behaviour end-to-end.

→ **Detail document:** `pando/plans/unified_single_writer_phase7_integration_tests.md`

## 4. Dependency Graph

```
Phase 1 (unified bootstrap) — no dependencies, unblocks everything
  ├── Phase 2 (write contract) — depends on 1
  ├── Phase 3 (serialisation loop) — depends on 1, can be done in parallel with 2
  ├── Phase 4 (PUB propagation) — depends on 1, 3 recommended
  ├── Phase 5 (failover) — depends on 1, 3, 4
  ├── Phase 6 (observability) — depends on 1, can be done in parallel with 2-4
  └── Phase 7 (integration tests) — depends on 1, 2, 3
```

## 5. Success Criteria

- [ ] Every Pando entrypoint (`root`, `serve`, `app`, `desktop`, `acp`) follows the same lock/primary/secondary/bootstrap path
- [ ] Only one process per project path opens SQLite in RW mode
- [ ] All secondary writes reach the primary through IPC
- [ ] Secondary reads are served locally (no IPC round-trip)
- [ ] A secondary instance functions correctly even when it's not the first to start
- [ ] Multi-process integration tests pass
- [ ] Primary death is detected and secondaries fail gracefully (Phase 5)

## 6. Files to Create / Modify (summary)

### New packages
- `internal/ipc/runtime/` — unified bootstrap component (Phase 1)
- `internal/ipc/writecoordinator/` — write serialisation loop (Phase 3)

### Modified files
- `cmd/root.go` — adopt unified bootstrap
- `cmd/app.go` — adopt unified bootstrap + IPC
- `cmd/serve.go` — adopt unified bootstrap + IPC
- `cmd/desktop.go` — adopt unified bootstrap + IPC
- `cmd/acp.go` — adopt unified bootstrap + IPC
- `internal/ipc/dbproxy/proxy.go` — add timeout, retry, error semantics (Phase 2)
- `internal/ipc/dbproxy/handlers.go` — wire write-coordinator (Phase 3)
- `internal/ipc/bus.go` — publish change events (Phase 4)
- `internal/app/app.go` — adopt unified bootstrap model (Phase 1-4)
- `internal/db/connect.go` — add `ConnectPrimary()` helper (Phase 1)

