---
created_at: 2026-10-01T19:52:43.343936677Z
updated_at: 2026-10-01T19:52:43.343936677Z
tags:
    - feature
    - projects
    - backend
---
# Feature: background `pando serve` web instance per project (PANDO-US-0103)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01.

## What changed
- `internal/project/web_instance.go` (new): `WebInstance` (`Project`, `Port`, `PID`, `Token`, `StartedAt`, `State` = `starting|running|error|stopped`), `Manager.OpenWeb` (reuse-or-spawn `pando serve --host 127.0.0.1 --port <free> --cwd <path>`, env `PANDO_PARENT_INSTANCE`, `PANDO_PROJECT_ID`, `NO_COLOR=1`, `procgroup.Ensure`), `Manager.CloseWeb` (SIGTERM, 5 s, SIGKILL), `WebInstances()`, `WebInstance(id)`, `/health` polling every 200 ms up to 20 s (`ErrChildStartupTimeout`), exit monitor, re-adoption of live children at `NewManager` (`adoptExistingWebInstances`). Injectable `spawnWebProcess` / `probeWebHealth` package vars for tests.
- `internal/project/ports.go` (new): loopback `chooseAvailablePort`, base 8800 (+10) then random.
- `internal/project/manager.go`: events `EvWebStarted`, `EvWebStopped`, `EvWebError`, `ManagerEvent.Port`; `StopReport`, `Unregister`, `Shutdown` also stop web instances; `parentInstanceID` from env `PANDO_INSTANCE_ID` (set in `cmd/{app,serve,desktop,root}.go`).
- DB: migration `20261001214000_add_project_web_runtime.sql` (`web_pid`, `web_port`), query `UpdateProjectWebRuntime`, `Service.UpdateWebRuntime`, `Project.WebPID/WebPort`. sqlc v1.29.0 panicked on generate, so `internal/db/{models,projects.sql,querier,db}.go` were hand-edited consistently.

## Runtime semantics after PANDO-US-0107
- `POST /api/v1/projects/{id}/activate` still means "start or focus the ACP delegation child". It does **not** open the browser workspace.
- `POST /api/v1/projects/{id}/web/open` / `.../web/close` are the dedicated controls for the background `pando serve` child used by the WebUI project tabs. The open response always returns a relative `web_url` (`/api/v1/projects/{id}/web/`) so the parent origin keeps auth/basic-auth in front of the child.
- `POST /api/v1/projects/{id}/stop` remains the broad shutdown operation: it stops the ACP child and the background WebUI child if this manager owns them.
- `POST /api/v1/projects/{id}/open-desktop` stays separate and only launches a native desktop window; it does not replace `activate` or `web/open`.

## Known follow-ups (owned by PANDO-US-0104)
- Done in PANDO-US-0104: the parent now pins a shared loopback certificate, fetches the child token over that pinned transport, stores it only in memory, and rejects children presenting another certificate.
- Done in PANDO-US-0104: re-adoption now uses `instanceregistry.Entry.WebPort` + `ParentInstanceID`, only adopts loopback orphans whose recorded parent is no longer alive, and no longer relies on a process-wide `PANDO_INSTANCE_ID` environment hand-off.

## Verification
`go build ./...`, `go vet ./internal/project/... ./cmd/...`, `go test ./internal/project/... ./internal/db/... ./internal/api/... -count=1` (ok), `go test -race ./internal/project/...` (ok, run by the implementing agent).