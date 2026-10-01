---
created_at: 2026-10-01T20:41:21.611279546Z
updated_at: 2026-10-01T20:41:21.611279546Z
tags:
    - feature
    - projects
    - api
    - security
---
# Feature: reverse proxy to project web children + prefixed UI serving (PANDO-US-0105)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01. Builds on [[project_web_instance_manager]], [[project_child_mode]], [[webui_storage_namespace_base_path]].

## Parent side
- `internal/api/handlers_projects_web.go`: `/api/v1/projects/{id}/web/` (any method/subpath) -> `httputil.ReverseProxy` to the child's `BaseURL()` over `Manager.WebTransport()` (pinned TLS). `/api/v1/projects/{id}/web` -> 308 to `/web/`. Behind the normal middleware (parent token or basic auth). Director strips browser `X-Pando-Token`, `Authorization` and `?token=`, sets `X-Pando-Token: <child token>` + `X-Pando-Client: web` (so EventSource and PTY websocket, which authenticate to the parent with `?token=`, authenticate to the child by header). Response: child `Access-Control-*`, `X-Pando-Token`, `Authorization` headers removed. `FlushInterval: -1` (unbuffered SSE), websocket upgrade forwarded. 404 `project_web_not_open` when the manager owns no running web instance for the id (never arbitrary ports), 502 `project_web_unavailable` on transport errors, 400 `invalid_project_web_path` for `.`/`..`/encoded slash segments. Test hook `Server.projectWebProxyLookup`.

## Child side
- `cmd/startup_mode.go`: `PANDO_PUBLIC_BASE` honoured only in project-child mode and only if it matches `^/api/v1/projects/[^/]+/web$` -> `api.ServerConfig.PublicBasePath`. The parent sets it in `defaultSpawnWebProcess`.
- `internal/api/server.go`: with `PublicBasePath` the child serves `index.html` uncompressed, injects `window.__PANDO_API_BASE__` and `window.__PANDO_ROUTER_BASENAME__` right after `<head>` (before the inline storage/theme bootstrap) and rewrites `<base href="/" />` to `<base href="<prefix>/" />`. `/health` reports `public_base_path`.
- `handlers_terminal_pty.go`: `checkPtyOrigin` accepts a cross-origin (parent-origin) handshake only in project-child mode AND with a valid child token; unchanged for the main instance.

## Verification
`go build ./...`, `go vet ./internal/api/... ./internal/project/... ./cmd/...`, `go test ./internal/api/... ./internal/project/... ./internal/app/... ./cmd/... -count=1` ok, `go test -race ./internal/api/ -run 'ProjectsWeb|Proxy'` ok. Tests cover header stripping/injection both ways, `?token=` replacement, unbuffered SSE, websocket echo, 404/502, traversal rejection, redirect, auth, prefixed index injection order/base rewrite/uncompressed index.