# Mesnada `pando` Engine vs Inter-Instance SQLite/IPC Architecture Analysis

**Date:** 2026-05-14
**Status:** analysis updated with additional project context and concrete fix

## Executive Summary

The recent Mesnada subtask failures are consistent with the current inter-instance SQLite architecture rather than with bad task prompts. The project intentionally enforces a **single SQLite writer per working directory**: the first Pando instance for a project path becomes the **primary** and owns the writable SQLite connection, while later instances become **secondaries** that open the DB read-only and proxy writes to the primary over ZeroMQ JSON-RPC through `dbproxy`.

That architecture is valid and documented. The failure appeared because the Mesnada `pando` engine launches a new `pando` subprocess in the **same project path**, so that subprocess becomes a **secondary instance**. In non-interactive mode the subprocess still needs to create a session, which means performing a DB write. That write is routed through `dbproxy` to the primary. The observed error:

```text
failed to create session for non-interactive mode: dbproxy: remote CreateSession: ipc: RPC error -32601: ipc: method not found
```

was ultimately caused by a **startup race in the primary CLI path**, not by the single-writer architecture itself.

## New Context Incorporated

The user clarified that:

- session/config state is stored partly in SQLite under the project path;
- SQLite should have only one writer process;
- if another instance starts in the same path, it intentionally becomes a **secondary**;
- the secondary communicates with the primary to work with the SQLite database.

This matches the existing design and implementation artifacts in the repository.

## Relevant Existing Design Documents

### `.kb/pando/plans/inter_instance_ipc_plan.md`
This plan explicitly defines the primary/secondary model:

- first instance per path = primary, sole SQLite writer;
- later instances = secondaries;
- secondary instances use read-only SQLite and relay DB writes via ZeroMQ RPC;
- ports are derived deterministically from the working directory path;
- lock file at `<workdir>/.pando/ipc.lock` determines the leader.

### `.kb/pando/plans/inter_instance_phase4_completed.md`
Phase 4 confirms this was implemented:

- secondaries never write SQLite directly;
- writes are proxied to primary via `db.write`;
- `cmd/root.go` wires secondaries to `dbproxy.New(db.New(roConn), client, rpcAddr)`;
- `internal/ipc/dbproxy/handlers.go` registers `db.write` on the primary bus.

It also lists known limitations, notably that only some entry points were wired initially.

## Current Runtime Behavior in Code

### 1. Secondary instance setup in `cmd/root.go`
When the current process cannot acquire the lock for the path, it becomes a secondary:

- opens read-only DB via `db.ConnectReadOnly()`;
- creates `ipc.Client`;
- constructs `dbproxy.New(db.New(roConn), ipcClient, rpcAddr)`;
- passes that proxy as `app.AppOptions.DBQuerier`.

So all session/message writes for that secondary go to the primary through RPC.

### 2. Primary handler registration in `cmd/root.go`
For the primary instance, `cmd/root.go` starts the bus and registers:

- `dbproxy.RegisterHandlers(bus, db.New(conn))`
- `bridge.RegisterHandlers(...)`
- `pandoApp.SetupIPC(bus)`

In the fixed code, those registrations happen **before** `bus.Start(...)`, so the RPC server cannot begin serving requests without the `db.write` handler already installed.

### 3. Non-interactive Pando run still creates a session
`internal/app/app.go` shows non-interactive mode still creates a session through `a.Sessions.Create(ctx, title)`. That means even a pure `pando -p ...` subagent invocation is not stateless with respect to the project DB; it must perform a DB write.

### 4. Mesnada `pando` engine launches a subprocess in the same workdir
`internal/mesnada/agent/spawner_pando_cli.go` launches the running Pando binary as a subprocess with roughly:

- `--yolo`
- `--output-format text`
- `-p <prompt>`
- `-c <task.WorkDir>` when set

So Mesnada's `pando` engine is not ACP here; it is a real CLI subprocess. But because it uses the same `work_dir`, it participates in the inter-instance lock/SQLite topology.

## Concrete Root Cause Found

The failing tasks all had:

- `engine: "pando"`
- `work_dir: /www/MCP/Pando/pando`
- non-interactive CLI execution

The concrete bug was:

1. `cmd/root.go` created the primary IPC bus;
2. it called `bus.Start(...)` first;
3. `bus.Start(...)` immediately launched the RPC serving goroutine;
4. only **after that** did `cmd/root.go` call `dbproxy.RegisterHandlers(...)`;
5. a newly spawned same-path `engine=pando` subprocess could become a secondary very quickly and issue `db.write -> CreateSession` before the primary had registered the handler;
6. the primary ROUTER was alive but had no `db.write` method yet, so it returned `-32601 method not found`.

So the root cause was a **primary startup race during handler registration**.

## Why the Earlier Hypothesis Needed Refinement

Before the fix, it was plausible that app/serve/desktop entrypoints lacking `db.write` registration were involved. That remains architecturally relevant, but the concrete reproducible failure for Mesnada `engine=pando` was narrowed further: the main failing path was the CLI/root entrypoint itself, where the handler registration order created a race window.

## Implemented Fix

### Changed files
- `cmd/root.go`
- `cmd/app.go`
- `cmd/serve.go`
- `cmd/desktop.go`
- `internal/ipc/dbproxy/handlers.go`
- `internal/ipc/dbproxy/handlers_test.go`

### Fix details

#### `cmd/root.go`
Moved these calls to happen **before** `bus.Start(...)`:

- `dbproxy.RegisterHandlers(bus, db.New(conn))`
- `bridge.RegisterHandlers(bus, instanceID, pandoApp.Sessions, pandoApp.Messages, time.Now())`
- `pandoApp.SetupIPC(bus)`

This ensures the bus starts serving only after all expected RPC methods are registered.

#### `cmd/app.go`, `cmd/serve.go`, `cmd/desktop.go`
Extended the same startup hardening pattern to the other entrypoints:

- register `dbproxy.RegisterHandlers(...)` before `Start(...)`
- register `bridge.RegisterHandlers(...)` before `Start(...)`

This makes handler availability consistent across entrypoints and reduces the risk of similar early-RPC races elsewhere.

#### `internal/ipc/dbproxy/handlers.go`
Generalized `RegisterHandlers` to accept a minimal registrar interface instead of a concrete `*ipc.Bus`, making handler registration testable in isolation.

#### `internal/ipc/dbproxy/handlers_test.go`
Added targeted tests covering:

- `db.write` handler registration before bus start semantics;
- correct dispatch of `CreateSession` through the registered handler;
- propagation of underlying querier errors.

## Important Clarification: The Architecture Itself Is Not the Problem

With the new context and the concrete fix, the refined conclusion is:

- It is **not** sufficient to say "`pando` should never use IPC".
- Because the subprocess runs in the same project path and the project intentionally enforces one SQLite writer, **some form of coordination is required**.
- The current chosen model is valid: same-path subprocesses can become secondaries and proxy writes to the primary.
- The failure was because the primary briefly exposed an RPC server before all required handlers were installed.

## Evidence in Code Supporting This Analysis

### Supports the single-writer architecture
- `cmd/root.go`
  - primary/secondary branching around lock acquisition
  - secondary uses read-only DB + `dbproxy`
  - primary registers `dbproxy` handlers
- `internal/ipc/dbproxy/handlers.go`
  - `db.write` handler dispatches `CreateSession` and other writes
- `.kb/pando/plans/inter_instance_phase4_completed.md`
  - explicitly documents this design as completed

### Supports that Mesnada `pando` is a CLI subprocess
- `internal/mesnada/agent/manager.go`
  - `EnginePando` routes to `pandoCLISpawner`
- `internal/mesnada/agent/spawner_pando_cli.go`
  - launches the current Pando binary with `-p`
- `internal/mesnada/orchestrator/orchestrator.go`
  - comments state `EnginePando` is non-ACP path

### Shows semantic inconsistency still exists
- `pkg/mesnada/models/task.go`
  - comment says `EnginePando` is "Pando itself as an ACP subagent"
- other code and tool metadata say `EnginePando` is CLI subprocess

This inconsistency remains a maintenance risk, although it was not the direct cause of the observed failure.

## Remaining Risks / Follow-Up

1. The semantic contradiction around `EnginePando` should still be resolved in docs/code comments.

2. A higher-level integration test would still be valuable:
   - start a primary Pando instance in a temp project path;
   - spawn `engine=pando` task in the same path;
   - assert `CreateSession` succeeds and the task runs.

3. `SetupIPC` ordering remains special in `root.go`; if similar app-level IPC state is ever needed in other entrypoints, that startup contract should be made explicit and centralized.

## Tests Run for the Fix

- `go test ./internal/ipc/dbproxy ./internal/mesnada/agent ./internal/app`
- `go test ./cmd ./internal/ipc/dbproxy ./internal/app`

## Final Assessment

The single-writer SQLite + primary/secondary IPC design is still the correct framing for same-path Pando subprocesses. The concrete Mesnada `engine=pando` failure was caused by a handler-registration race in the primary startup path, and the same startup-order hardening pattern has now been extended to the other main entrypoints as well.