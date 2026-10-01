---
id: PANDO-US-0103
type: story
title: "ProjectManager: spawn and track a background `pando serve` web instance per project"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, backend]
estimate: 8
created: 2026-10-01T19:23:08Z
updated: 2026-10-01T19:23:08Z
---

## Description

Add a second instance kind to `internal/project`: a **web instance** running `pando serve --host 127.0.0.1 --port <free> --cwd <path>` in the project directory, independent from the ACP delegation child (`spawnChild`, `manager.go:196`).

- New `WebInstance` struct (`internal/project/web_instance.go`): `Project`, `cmd`, `Port`, `PID`, `Token` (filled by the TLS/token story), `StartedAt`, `errCh`, `cancel`, `State` (`starting|running|error|stopped`), stderr/stdout ring buffer for diagnostics.
- `Manager.OpenWeb(ctx, projectID) (*WebInstance, error)`: reuse-or-spawn; port chosen with a loopback `chooseAvailablePort` moved/duplicated into `internal/project/ports.go` (base 8800, then random free); poll `GET /health` every 200 ms up to 20 s, `ErrChildStartupTimeout` otherwise; env `PANDO_PARENT_INSTANCE=<id>`, `PANDO_PROJECT_ID=<id>`, `NO_COLOR=1`; `procgroup.Ensure(cmd)` so the child dies with the parent.
- `Manager.CloseWeb(ctx, projectID)`: SIGTERM, 5 s, SIGKILL (same policy as `StopReport`). `Shutdown()` closes web instances too.
- `Manager.WebInstances()` / `WebInstance(projectID)`; `Runtime()` and `DelegationInfo()` keep working.
- Events: new `ManagerEventType`s `EvWebStarted`, `EvWebStopped`, `EvWebError` (with `Port`, `Error`); exit monitor goroutine publishes them and updates the DB.
- Persistence: migration adding `web_port INTEGER NOT NULL DEFAULT 0` (and `web_pid`) to `projects`; `Service.UpdateWebRuntime(ctx, id, pid, port)`; `project.Project` gains `WebPort`, `WebPID`.
- Re-adoption after a parent restart: on startup scan `instanceregistry` for entries with `ParentInstanceID` == previous parent id or `Path` == project path and `Mode == webui` + alive PID, and attach them as web instances (needs the token handshake story to re-read the token).
- `ErrProjectNeedsInit` is returned when the path has no config (same as `Activate`).

## Acceptance Criteria

- [ ] `OpenWeb` spawns a serve child that answers `/health` on the chosen loopback port; a second call reuses it.
- [ ] `CloseWeb` stops it and `EvWebStopped` is published; a crashed child publishes `EvWebError` with the last stderr lines.
- [ ] DB reflects `web_port/web_pid`; stale values are cleared when nothing is alive.
- [ ] Re-adoption test with a fake registry entry.
- [ ] Unit tests in `internal/project` (fake `pandoBin` script, as in `manager_test.go`).
