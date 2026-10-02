---
created_at: 2026-10-02T09:35:57.27775193Z
updated_at: 2026-10-02T09:38:52.645635207Z
tags:
    - fix
    - security
    - projects
    - api
---
# Project workspaces security hardening (EP-0019 / US-0115, 2026-10-02)

Follow-up of an independent read-only security review of [[project_workspaces_webui_tabs]]. No critical finding; review confirmed as sound: proxy target selection (no SSRF/path escape), TLS pinning, the postMessage bridge. Related: [[project_web_reverse_proxy]], [[project_web_proxy_cookie_auth]], [[project_web_instance_manager]], [[project_child_mode]].

| Id | Change | Key files |
|----|--------|-----------|
| F1 (H1) | Parent mints a random token per web child, passes it via `PANDO_CHILD_API_TOKEN` (child `os.Unsetenv`s it, uses it as `ServerConfig.APIToken`). Child token endpoint requires the token header, so other local users can no longer `GET /api/v1/token` on the child port. `fetchWebToken` and re-adoption removed. Child watchdog polls `PANDO_PARENT_PID` (3s) and exits when the parent is gone: children no longer outlive their parent. | `internal/project/web_instance.go`, `cmd/startup_mode.go`, `cmd/serve.go`, `internal/api/server.go` |
| F2 (M1,M2) | `/health` reports `pid` in project-child mode; the probe requires startup_mode/project_id/parent_instance_id/pid to match, else `EvWebError` + terminate. Child never falls back to another port; `OpenWeb` retries once on a fresh port. Only self-started PIDs are signalled; orphans are left alone (they exit by themselves). | `internal/api/handlers_base.go`, `internal/project/web_instance.go`, `cmd/serve.go` |
| F3 (M3) | Proxy cookie = per-server random secret (not the API token), constant-time compare. Cookie-only non-GET/HEAD requests and websocket upgrades need a same-origin proof (`Sec-Fetch-Site`, else `Origin`), else 403 `cross_origin_forbidden`. | `internal/api/handlers_projects_web.go`, `server.go`, `handlers_terminal_pty.go` (comment) |
| F4 (L2) | Proxy answers `<prefix>/api/v1/token` with `{"token":"proxied"}`; the child token never reaches the browser. `auth.ts` unchanged. | `handlers_projects_web.go` |
| F5 (L1) | `PANDO_PUBLIC_BASE` pattern `^/api/v1/projects/[A-Za-z0-9-]+/web$`; `html.EscapeString` into `<base href>`. | `cmd/startup_mode.go`, `server.go` |
| F6 (L3) | `Set-Cookie` stripped; in-child `Location` rewritten to the prefix; other absolute/protocol-relative `Location` -> 502 `project_web_bad_redirect`. | `handlers_projects_web.go` |
| F7 (M4, partial) | `frame-ancestors 'self'` (merged into an existing CSP) + `X-Frame-Options: SAMEORIGIN` on proxied HTML. Child `/preview/...` refused with 403 `not_available_in_project_tab`: previews are served from the same unsandboxed origin (`handlers_design.go` `setupPreview`). | `handlers_projects_web.go` |

## Tests
- `internal/project`: `TestDefaultProbeWebHealthChecksIdentity`, `TestOpenWebIdentityMismatchFailsStartup`, `TestOpenWebRetriesOnceOnFreshPort`, `TestNewManagerDoesNotAdoptOrSignalOrphan`, spawn env test. Test DB now single-connection (in-memory sqlite).
- `internal/api`: cookie secret/not-token, cookie same-origin matrix, token placeholder, preview refusal, response hardening, child token endpoint, health pid, base href escaping.
- `cmd`: child token consumed + unset, odd project id rejected, parent watchdog.
- Lead verification: `go build ./...`, `go vet`, `go test ./internal/project/... ./internal/api/... ./internal/app/... ./internal/config/... ./cmd/... -count=1` all ok; `go test -race ./internal/project/...` ok; WebUI typecheck + 103 tests ok; live E2E `web-ui/scripts/e2e-project-tabs.sh` -> 1 passed.
- Known, pre-existing and unrelated: `go test -race ./internal/api/` fails in ~14 config-global tests (`TestDecisionModel*`, `TestPutConfigLSP*`, `TestPlayground*`…); the same tests fail on the commit before this change and pass without `-race` or when run alone (shared global config state).

## Residual risks (open)
- Same-origin frame: hostile project content can script the parent UI and read its stored token. Recommended follow-up: serve project frames from a separate-origin parent listener, then re-enable design previews in project tabs.
- `/tmp/pando-instances` registry directory permissions are pre-existing (0755, first creator wins).
- The child's stderr tail is returned to authenticated callers on startup failure.
- Orphans from a crashed parent stay up for ~3s plus shutdown; on Windows the watchdog is disabled.
- Desktop webviews (WebKitGTK / WKWebView / WebView2) not verified live: see [[project_workspaces_desktop_tui]].