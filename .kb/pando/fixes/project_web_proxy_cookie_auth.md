---
created_at: 2026-10-02T08:51:18.446599085Z
updated_at: 2026-10-02T08:51:18.446599085Z
tags:
    - fix
    - projects
    - api
    - security
---
# Fix: project frames could not load through the proxy (401) — proxy cookie

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019), found by the live end-to-end run (PANDO-US-0115). Date: 2026-10-02. Extends [[project_web_reverse_proxy]].

## Symptom
The project tab showed `{"error":"unauthorized"}` instead of the child WebUI. The iframe navigation and the scripts/styles/images it loads are browser-initiated requests: they cannot carry `X-Pando-Token`, and the proxy lives under `/api/`, where `authMiddleware` requires the token. Unit tests always sent the header, so only a real browser showed it.

## Fix (decision approved by the user, option "cookie")
- `internal/api/handlers_projects_web.go`: cookie `pando_project_web` = parent API token, `HttpOnly`, `SameSite=Strict`, `Secure` when the request is TLS, `Path=/api/v1/projects/`. `setProjectWebCookie` is called by `POST /api/v1/projects/{id}/web/open` and `GET /api/v1/projects/web` (so a reload or a parent restart, which mints a new token, refreshes it).
- `internal/api/server.go` `authMiddleware`: the cookie is accepted only when `isProjectWebCookiePath(method, path)` — `/api/v1/projects/{id}/web` and below, excluding `POST .../web/open|close`. Every other `/api/` path still needs the header/query token.
- The proxy Director strips the cookie before forwarding (`stripProjectWebCookie`), keeping other cookies, so the parent token never reaches the child.
- Rejected alternative: signed ticket in the URL path (secret in history/logs).

## Related fixes found in the same run
- `pando serve` has no `--cwd` flag: the child exited at once; the spawn now relies on `cmd.Dir` (`internal/project/web_instance.go`).
- `pando serve` did not serve the embedded UI; it now loads `api.EmbeddedWebUI()` in `project-child` mode (`cmd/serve.go`).

## Verification
- Go: `TestProjectsWebProxyAcceptsCookieAndNeverForwardsIt`, `TestProjectsWebCookieIsScopedToProxyPaths`, `TestSetProjectWebCookieAttributes`; `go test ./internal/api/... -count=1` ok.
- Live (isolated HOME, `pando app` + Playwright + system Chrome): click project -> tab + workspace route; frame loads child UI under the prefix; child has no Projects/Instances nav and no tab bar, title "projA / Chat"; parent `pando_token` unchanged, child key `pando_token@<hash>`; cookie HttpOnly/Secure/Strict and not readable from JS; child Terminal opens a PTY shell in the project directory through the proxied websocket; switching to main and back keeps the child's route (keep-alive); fresh load and reload restore the tabs; unauthenticated `GET .../web/` -> 401.
## Update 2026-10-02
- CORRECTION: the cookie no longer holds the API token. Value = random per-server `Server.projectWebCookieSecret`, compared in constant time. Requests authenticated only by the cookie must be GET/HEAD non-websocket, or carry `Sec-Fetch-Site: same-origin` (or a same-origin `Origin`), else 403 `cross_origin_forbidden`. See fixes/project_workspaces_security_hardening.md.
