---
created_at: 2026-10-01T20:22:03.443141667Z
updated_at: 2026-10-01T20:22:03.443141667Z
tags:
    - feature
    - projects
    - backend
    - webui
---
# Feature: `project-child` startup mode and recursion guard (PANDO-US-0106)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Builds on [[project_web_instance_manager]].

## What changed
- `cmd/startup_mode.go` (new): `resolveStartupContext(cwd, defaultMode)` reads `PANDO_PARENT_INSTANCE` / `PANDO_PROJECT_ID` once; when set the mode is `project-child` and the project name comes from the global registry (fallback: directory base name). Used by `cmd/{serve,app,desktop}.go` and passed explicitly to `api.ServerConfig` (`StartupMode`, `ParentInstanceID`, `ProjectID`, `ProjectName`) and `app.AppOptions.ChildParentInstanceID`.
- `internal/project`: `ManagerOptions.SpawnDisabled`; `Activate`, `EnsureInstance` auto-start, `OpenWeb`, `CompleteInit` return `ErrChildInstance` in a child. Child skips `RegisterSelfAsGlobalProject`; `SeedFromGlobal` still runs (peer projects needed for external IPC delegation) but skips the child's own directory.
- `internal/api`: `/health` payload now carries `startup_mode`, `parent_instance_id`, `project_id`, `project_name`, `public_base_path` (empty until PANDO-US-0105). In child mode `/api/v1/projects*`, `/api/v1/instances*` and the external-access rebind return 409 `{"error":"not_available_in_child"}`.
- WebUI: `serverStore` reads the server info; `isProjectChildMode()` is true when the API base path is `/api/v1/projects/<id>/web` OR the server reported `project-child` (`setServerProjectChildMode`). Child hides Projects/Instances nav (Sidebar, QuickMenu), the external-access footer toggle, the first-run SetupWizard, and shows the project name in the Header.

## Verification
`go build ./...`, `go vet` on touched packages, `go test ./internal/project/... ./internal/api/... ./internal/app/... ./internal/instanceregistry/... -count=1` ok; new tests `internal/api/child_mode_test.go`, `internal/project/child_mode_test.go`, `web-ui/.../api.child-mode.test.ts`, `Sidebar.child-mode.test.tsx`; `bun run typecheck/test/build` ok.
## Update 2026-10-02
- Project-child mode requires a parent-minted API token (`PANDO_CHILD_API_TOKEN`, read once and `os.Unsetenv`'d, passed as `api.ServerConfig.APIToken`); without it `pando serve` refuses to start. `/api/v1/token` needs a valid token header in this mode. `/health` also reports `pid`. `pando serve` fails hard if the requested port is busy and exits when `PANDO_PARENT_PID` disappears. `PANDO_PUBLIC_BASE` must match `^/api/v1/projects/[A-Za-z0-9-]+/web$` and is HTML-escaped into `<base href>`.
