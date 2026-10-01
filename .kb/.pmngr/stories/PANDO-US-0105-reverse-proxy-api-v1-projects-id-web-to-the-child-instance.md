---
id: PANDO-US-0105
type: story
title: Reverse proxy `/api/v1/projects/{id}/web/*` to the child instance (REST, SSE, WebSocket, UI bootstrap rewrite)
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, api, security]
estimate: 8
created: 2026-10-01T19:23:08Z
updated: 2026-10-01T19:23:08Z
---

## Description

Expose the whole child HTTP surface through the parent so the browser keeps one origin and one credential.

- Route `mux.Handle("/api/v1/projects/{id}/web/", ...)` in `internal/api/routes.go` (new file `handlers_projects_web.go`), behind the usual `basicAuthMiddleware`/`authMiddleware` (parent token or basic auth). Path after `/web/` is forwarded verbatim to `https://127.0.0.1:<port>/`; `/api/v1/projects/{id}/web` (no trailing slash) redirects to `/web/`.
- Implementation on `httputil.ReverseProxy` with: pinned TLS transport from the trust story; `Director` setting `X-Pando-Token: <child token>` and `X-Pando-Client: web`, stripping the browser's `X-Pando-Token` / `?token=` (the parent's token must not leak to the child either); `FlushInterval = -1` for `text/event-stream` and any chunked response so SSE events are not buffered; `ModifyResponse` to drop `Access-Control-*` headers from the child; no body size limit beyond the parent's existing one; `ErrorHandler` mapping dial failures to 502 `{"error":"project_web_unavailable"}` and publishing `EvWebError` when the child is gone.
- WebSocket: `httputil.ReverseProxy` in Go ≥1.20 forwards `Upgrade` — verify the PTY path `/api/v1/terminal/pty` works end to end; the child's `checkPtyOrigin` must accept the parent origin (forward `Origin` unchanged, child is same origin anyway after rewrite) — add a test.
- UI bootstrap rewrite: the child's `ui_assets_app.go` injects `window.__PANDO_API_BASE__` into `index.html`. Add to `ServerConfig` a `PublicBasePath string` (set from env `PANDO_PUBLIC_BASE=/api/v1/projects/<id>/web` passed by the parent) that the child uses to (a) inject `__PANDO_API_BASE__ = "<base>"`, (b) emit `<base href="<base>/">` / rewrite asset URLs, (c) set `window.__PANDO_ROUTER_BASENAME__`. Prefer this over regex rewriting in the proxy.
- The proxy must not be reachable for projects without a running web instance (404 `project_web_not_open`) and must only target instances owned by this manager.

## Acceptance Criteria

- [ ] `GET /api/v1/projects/{id}/web/api/v1/sessions` returns the child's sessions; `POST .../web/api/v1/chat/stream` streams events without buffering (test with a slow fake backend).
- [ ] `EventSource` endpoints (`/projects/events`, `/notifications/stream`) and the PTY WebSocket work through the proxy (httptest with a real `websocket.Upgrader`).
- [ ] Headers: child never sees the browser token; browser never sees the child token (header stripping tests).
- [ ] `go test ./internal/api` green; `routes_test.go` updated.
