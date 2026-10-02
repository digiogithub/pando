---
created_at: 2026-10-02T09:07:33.967121328Z
updated_at: 2026-10-02T09:07:33.967121328Z
tags:
    - feature
    - projects
    - delegation
    - ipc
---
# Feature: delegation reuses the project web child over IPC (PANDO-US-0113)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-02. Builds on [[project_web_instance_manager]], [[project_child_mode]].

## Why
A project web child (`pando serve` from `Manager.OpenWeb`) is the IPC primary for its directory (holds `.pando/ipc.lock`, runs the ZMQ bus, answers `delegation.run`). Spawning the ACP delegation child in the same directory would add a second process that becomes an IPC secondary.

## What changed
- `internal/project/delegation.go`, `delegate_external.go`: when a manager-owned `WebInstance` runs for the project, `EnsureInstance`/`WarmDelegate`/`Delegate` route over IPC to it regardless of `allowExternal` and never spawn `pando acp`. RPC endpoint = the project lock file, accepted only when the lock PID equals the tracked web child PID; short bounded retry while the child's bus comes up. Result marked `DelegateResult.Web=true, External=false` (metrics in `internal/mesnada/orchestrator/{warm,metrics}.go`).
- `internal/project/delegation_slots.go` (new): slot/queue bookkeeping shared by `Instance` and `WebInstance` (cap, bounded FIFO, close-and-wake). Web-routed delegations count in `DelegationInfo`, publish `EvDelegationChanged`, show in `GET /api/v1/projects/web` `delegations` and in `web/close` / `stop` `cancelled_delegations`.
- `Runtime()` reports a manager-owned web child as `running, external=false` with its PID (so `Stop` works instead of `ErrExternalInstance`).
- `Activate` with a web child running only switches the active pointer. `OpenWeb` with an ACP child running stops it when idle; with in-flight delegations it returns `ErrDelegationsInFlight` -> API 409 `delegations_in_flight`.
- Idle GC only considers ACP children. Recovery/cancel paths (`cmd/bridge_delegation.go`, `ExternalDelegationRecoverer`) cover web-routed delegations; closing the child mid-delegation ends the parent run with a terminal conclusion.

## Verification
`go build ./...`, `go vet` on touched packages, `go test ./internal/project/... ./internal/app/... ./internal/api/... ./internal/llm/agent/... ./internal/mesnada/... -count=1` ok (0 failures), `go test -race ./internal/project/... -count=1` ok. Not exercised live: a real delegated run needs a configured LLM provider in the isolated test HOME.
## Update 2026-10-02
- The child token used for IPC-adjacent reuse is the parent-minted in-memory token; orphans are never adopted (children exit with their parent), so reuse only applies to children started by this parent process.
