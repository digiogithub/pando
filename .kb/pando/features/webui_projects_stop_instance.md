---
created_at: 2026-06-19T05:01:03.367787161Z
updated_at: 2026-06-19T05:05:07.409369763Z
tags:
    - pando
    - feature
    - webui
    - tui
    - projects
    - acp
    - ipc
---
# Projects — Stop running instances (Web-UI + TUI) with external-instance guard

Date: 2026-06-19

## Goal

Clicking/selecting a project used to only *activate* it (spawn a child `pando acp`
instance connected to the main instance and switch the active pointer). Requirement:
a **running** project should now be **stoppable**, **unless** its instance was not
launched by this manager but **externally** (e.g. a user opened the project from an
editor in ACP mode). In that case show a message that it was launched externally and
must be closed from the application that launched it.

Implemented in both the Web-UI and the Terminal UI (TUI) for parity.

## Ownership detection (internal vs external) — shared backend

No new registry. Ownership is derived from two existing sources:

1. **Instances we launched** live in the in-memory `Manager.instances` map (keyed by
   project ID). These are ours and can be stopped.
2. **External instances**: every `pando acp` process writes an IPC lock at
   `<projectDir>/.pando/ipc.lock` with `LockInfo{PID, PubPort, RPCPort, ...}`.
   `ipc.ReadLockForPath(resolvedPath)` reads it without acquiring the flock. If the
   lock exists, its PID is alive (`Signal(0)`), and the project is **not** in
   `Manager.instances`, the instance is external.

```
Manager.Runtime(projectID, path) -> (running, external bool, pid int)
  - in instances map      -> running=true,  external=false, pid=child pid
  - lock held + PID alive  -> running=true,  external=true,  pid=lock pid
  - otherwise              -> running=false, external=false
```

## Backend (internal/project, internal/api)

- `errors.go`: new sentinel `ErrExternalInstance`.
- `manager.go`:
  - `Runtime(projectID, path)` — live runtime + ownership (above).
  - `Stop(ctx, projectID)` — terminates the child we own (cancel ctx + SIGTERM, wait
    up to 5s on `inst.errCh`, then Kill); publishes `EvProjectSwitched("")`. If not
    ours: `Runtime`; when `running && external` returns `ErrExternalInstance`;
    otherwise marks the project `stopped`.
  - helper `pidIsAlive(pid)` (Signal 0).
  - **Bug fix**: an intentional stop cancels `procCtx` (SIGKILLs the child via
    `exec.CommandContext`), so `cmd.Wait()` returns non-nil and the monitor goroutine
    used to record `StatusError`. Now the monitor checks `procCtx.Err()`: a cancelled
    context (Stop / Unregister / Shutdown) is a clean `StatusStopped`; only a process
    that exits on its own with an error becomes `StatusError`.
- `project.go`: `Project.External bool` — computed/non-persisted flag, populated on
  demand via `Manager.Runtime`; zero in DB-sourced values.
- `handlers_projects.go`: `projectResponse.external`; `enrichRuntime(*resp, p)`
  reconciles persisted status with live state (sets external, forces running when a
  live instance serves the path, corrects stale running→stopped) applied in
  list/get/active; `handleStopProject` → `POST /api/v1/projects/{id}/stop`: 200
  `{"status":"stopped"}` or 409 `{"error":"external_instance","project_id":"..."}`.
- `routes.go`: registered `POST /api/v1/projects/{id}/stop`.

## Web-UI (React + zustand)

- `types/index.ts`: `Project.external?: boolean`.
- `stores/projectStore.ts`: new `stopProject(id)` action; on 409
  `{"error":"external_instance"}` shows the external toast; generic errors fall through.
- `components/projects/ProjectsView.tsx`: `handleToggle(proj)` wired to row `onClick`
  and a per-row power button (play/stop/lock by state); external rows show an
  "external" badge with a lock next to the status; row/button tooltips explain the
  action. Click semantics: running → `stopProject` (backend decides ours vs external),
  otherwise → `activateProject`.

## TUI (internal/tui)

- Dialog `components/dialog/projects.go`:
  - New `[s] Stop` key binding (preserves `enter` = activate/switch — the TUI keeps
    switching while adding a dedicated stop action, instead of a click-toggle).
  - New `ProjectStopMsg{ProjectID, Path}`; `s` only emits it when the selected
    project's status is `running`.
  - `renderProjectItem` shows a muted `[external]` suffix when `proj.External`.
  - Footer help updated to include `[s] Stop`.
- `tui.go`:
  - `listProjectsEnriched()` lists projects and reconciles each via `Manager.Runtime`
    (sets `External`, corrects stale running→stopped). Used by `openProjectsDialog`.
  - `refreshProjectsDialogMsg{info}` re-populates the open dialog after an action and
    optionally reports info.
  - `dialog.ProjectStopMsg` → `stopProject(projectID, path)` command: on
    `ErrExternalInstance` reports a warn with the external message; on success returns
    `refreshProjectsDialogMsg{info:"Project <name> stopped"}` to refresh the list
    in-place (dialog stays open).

## Verification

- `go build ./internal/project/... ./internal/tui/... ./internal/api/...`,
  `go test ./internal/project/`, `gofmt`, `go vet` — all clean.
- `npx tsc --noEmit` in web-ui — clean.

## Notes

- IPC primitives reused: `ipc.PortsForPath`, `ipc.ReadLockForPath`,
  `<dir>/.pando/ipc.lock` (LockInfo). Related: suspended-primary kill fix in
  `internal/ipc/runtime`, project web-server-mode plan.
