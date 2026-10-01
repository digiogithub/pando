---
id: PANDO-US-0113
type: story
title: "Delegation reuse: route `mesnada_spawn_agent` for a project to its running web child over IPC instead of a second `pando acp`"
status: backlog
priority: medium
parent: PANDO-EP-0019
author: mcp
labels: [projects, delegation, ipc]
estimate: 5
created: 2026-10-01T19:24:43Z
updated: 2026-10-01T19:24:43Z
---

## Description

A web child is a primary IPC instance for its directory (it holds `.pando/ipc.lock`, runs the ZMQ bus and `registerBridgeHandlers` with `delegation.run`). Spawning the ACP delegation child in the same directory would make it a secondary and double the processes.

- `Manager.EnsureInstance` / `WarmDelegate`: when a `WebInstance` is running for the project, route through `DelegateExternal` (B3 path, `delegate_external.go`) to the child's RPC port read from the `WebInstance` (not from the lock file), regardless of `AllowExternalWarmTargets`; count it in `DelegationInfo` so the Projects view and tab badge show in-flight delegations; mark `External=false, Web=true` in `DelegateResult` for metrics.
- `Runtime()` must report a manager-owned web child as `running, external=false` so `Stop` works.
- Idle GC (`delegation_gc.go`) must never tear down a web instance; only ACP children remain GC candidates.
- `Activate` (TUI/`activate` endpoint) when a web child exists: switch active pointer without spawning ACP.
- Delegated session visibility: delegated runs executed in the child appear as sessions in the child's UI (they do, as `delegation.run` opens real sessions) — document; optionally tag them so the child UI can filter "delegated" sessions.
- `fix_warm_acp_pending_self_delegation` and `ExternalDelegationRecoverer` paths: make sure recovery handles a web child restart.

## Acceptance Criteria

- [ ] Integration test: open web child, run `WarmDelegate` for that project → no `pando acp` process spawned, result captured over IPC, `DelegationInfo` increments/decrements and `delegation_changed` SSE fires.
- [ ] GC test: idle web instance survives `StartIdleGC`.
- [ ] `go test ./internal/project ./internal/llm/agent ./internal/mesnada/...` green.
