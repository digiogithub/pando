---
id: PANDO-US-0116
type: story
title: Serve project frames from a separate origin so project content cannot script the parent WebUI
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, security, api, webui]
estimate: 8
created: 2026-10-02T10:00:06Z
updated: 2026-10-02T10:00:06Z
---

## Description

Follow-up of the security review of PANDO-EP-0019 (finding M4, see KB `pando/fixes/project_workspaces_security_hardening.md`).

Today a project tab is an iframe whose `src` is `/api/v1/projects/{id}/web/` on the **same origin** as the parent WebUI (`web-ui/src/components/layout/ProjectFrameHost.tsx`, proxy in `internal/api/handlers_projects_web.go`). Any script that runs inside a child UI — a design preview, an extension panel, a rendered file from a hostile project directory — can reach `window.parent`, read the parent's `pando_token` from `localStorage` and call the parent API, which controls every project. Rendering the same content in a single instance only exposes that one project; the frame turns it into a cross-project boundary crossing.

Interim mitigations already shipped (commit `59b2ec48`): `frame-ancestors 'self'` + `X-Frame-Options: SAMEORIGIN` on proxied HTML, and the child's `/preview/…` route is refused with 403 `not_available_in_project_tab`. `sandbox` on the iframe is not an option: with `allow-same-origin` it protects nothing, without it storage and the proxy cookie break.

Goal: give project frames their own origin, so the browser's same-origin policy separates each child UI from the parent UI, while keeping what the current design gets right (pinned TLS to the child, parent-minted child token that never reaches the browser, one credential for the user).

### Proposed design

1. **Frame listener.** The parent opens a second loopback/TLS listener on its own port ("frame port", same bind host and certificate as the main listener, registered in `portguard`, rebinding together with the external-access toggle). It serves only the project web proxy: `https://<host>:<framePort>/p/{id}/…` → child. A different port is a different origin, so the frame can no longer touch `window.parent` or the parent's storage. Evaluate per-project ports versus one frame port with a path per project: one port isolates children from the parent but not from each other; decide and document (per-project isolation needs one listener per open tab, bounded by `[Projects].MaxWebInstances`).
2. **Authentication on the frame origin.** The parent WebUI obtains a short-lived, single-use ticket from `POST /api/v1/projects/{id}/web/open` (and from the list endpoint on restore) and loads the frame with `…/p/{id}/?ticket=…`; the frame listener exchanges it for an HttpOnly, SameSite=Strict, `Secure` cookie scoped to that origin and redirects to the clean URL. The cookie holds a random per-server secret (as today), never the API token. The current same-origin cookie path under `/api/v1/projects/` is removed once the frame listener is the only way in.
3. **Child prefix.** `PANDO_PUBLIC_BASE` becomes `/p/{id}` (pattern tightened accordingly); the child keeps injecting `__PANDO_API_BASE__` / `__PANDO_ROUTER_BASENAME__` and rewriting `<base href>`. Browser storage on the frame origin is already separate from the parent, so the `storageKey` namespacing stays only to separate children that share the frame origin.
4. **Bridge.** `web-ui/src/lib/projectFrameBridge.ts` and `hooks/useProjectChildBridge.ts` validate `event.origin` against the configured frame origin instead of `window.location.origin`, and post with that explicit target origin. Payload validation stays as is. Parent learns the frame origin from the open/list responses (`web_url` becomes absolute).
5. **Previews.** Once frames are cross-origin to the parent, re-enable `/preview/…` through the proxy and remove the 403.
6. **Same-origin proof / CORS.** The frame origin must not be allowed to call the parent API: verify `corsMiddleware` (today `Access-Control-Allow-Origin: *`) does not let the frame origin read parent responses with credentials, and that the parent's API token is unreachable from it. Keep the same-origin proof for cookie-only writes and websocket upgrades on the frame listener.
7. **Desktop.** Wails webview must allow the cross-origin frame (WebKitGTK / WKWebView / WebView2), including the PTY websocket and downloads; the self-signed certificate is the same one the main origin already uses, but a different port may need its own acceptance in a plain browser — detect the failure and show an actionable message in `ProjectWorkspace`.
8. **External access / basic auth.** When the parent is bound to the LAN behind basic auth, the frame listener must enforce the same basic auth and follow the same bind host.

## Acceptance Criteria

- [ ] A script running inside a project frame cannot read `window.parent.localStorage`, the parent DOM or call the parent API with the user's credential (Playwright test: evaluate inside the frame, expect a cross-origin `SecurityError` and a 401 from the parent API).
- [ ] The child token and the parent API token never appear in the frame origin's storage, cookies readable by JS, URLs after the ticket redirect, or responses (tests).
- [ ] Tickets are single-use, expire in seconds, and are bound to the project id; reuse or a wrong project returns 401 (tests).
- [ ] Project tab features still work end to end: chat streaming (SSE), terminal (PTY websocket), reload restore, keep-alive across tab switches, close with stop — `web-ui/scripts/e2e-project-tabs.sh` extended and passing.
- [ ] Theme, language, title, busy state, notifications and shortcuts still cross the bridge with the new origin checks (unit tests for `projectFrameBridge`).
- [ ] Design previews work inside a project tab again and the 403 `not_available_in_project_tab` is gone.
- [ ] Works with the external-access toggle and basic auth (handler tests), and the frame port is released on shutdown and rebinding.
- [ ] The old same-origin proxy cookie path is removed or documented as kept, with tests proving which.
- [ ] Decision on per-project versus shared frame origin recorded in the KB with its trade-off; security hardening doc and docs site page updated.
- [ ] `go test ./internal/api/... ./internal/project/... ./cmd/... -count=1`, `go test -race ./internal/project/...`, and `bun run typecheck/lint/test/build` pass.

## Notes

- Context: KB `pando/fixes/project_workspaces_security_hardening.md` (findings M3/M4 and residual risks), `pando/fixes/project_web_proxy_cookie_auth.md`, `pando/features/project_web_reverse_proxy.md`, `pando/features/webui_project_workspace_iframe.md`.
- Related open items from the same review, candidates to fold in or split out: the instance registry directory `/tmp/pando-instances` is created `0755` by the first user to get there (move to `$XDG_RUNTIME_DIR` at `0700` and check the owner); the child's stderr tail is returned to authenticated callers on startup failure; the child parent-watchdog is disabled on Windows.
- Not verified yet and worth doing in the same pass: live behaviour of nested frames in the three desktop webviews (matrix in KB `pando/features/project_workspaces_desktop_tui.md`).
- Risk: a second self-signed-TLS origin in a plain browser may need a separate certificate acceptance; that UX is the main reason the first implementation stayed same-origin.
