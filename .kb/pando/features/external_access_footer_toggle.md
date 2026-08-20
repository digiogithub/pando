---
created_at: 2026-08-20T17:30:02.186403183Z
updated_at: 2026-08-20T17:30:02.186403183Z
tags:
    - feature
    - webui
    - security
    - api
    - app
    - desktop
---
# Feature: External-access toggle in the WebUI footer (2026-08-20)

## Motivation

`pando serve --host 0.0.0.0` could expose the instance to the LAN (guarded by
[[webui_basic_auth]]), but `pando app` / `pando desktop` always bound the host
given at startup (default `localhost`). Reaching the same running instance from
a phone or a second machine meant restarting with a flag. The user wanted a
quick footer toggle that flips external access on the live process, so several
clients can connect at the same time.

## What changed

### Backend — live listener rebinding
- `internal/api/server.go`
  - `Server` gained `bindMu`, `bindHost`, `initialHost`, `rebinding`,
    `rebindCh`, `activeListener`.
  - `Start()` no longer calls `ListenAndServe(TLS)`. It binds through the new
    `newListener(host)` (which wraps the socket in `tls.NewListener` when TLS is
    configured) and loops around `httpServer.Serve(listener)`: a `Serve` return
    while `rebinding` is set means a listener swap, not a shutdown, so the loop
    picks the replacement off `rebindCh` and keeps serving.
  - `Rebind(host)` closes the **active listener directly** (never
    `httpServer.Close`, which would mark the server permanently shut down),
    binds the new host on the same port, and hands the listener to the Serve
    loop. Closing first is mandatory: `0.0.0.0` and `127.0.0.1` collide on the
    same port. On failure it re-binds the previous host and returns the error;
    every path feeds `rebindCh` exactly once.
  - `BindHost()` / `InitialHost()` accessors (falling back to `config.Host` so
    struct-built test servers behave).
- `internal/api/basicauth.go`, `handlers_basicauth.go`: the basic-auth gate and
  the WebUI Access panel now read `s.BindHost()` instead of the immutable
  `s.config.Host`, so enabling external access makes basic auth enforced
  immediately, with no restart.

### Backend — endpoint
- **new** `internal/api/handlers_external_access.go`:
  `GET|PUT /api/v1/config/api-server/external-access` returning
  `ExternalAccessStatus{enabled, bindHost, port, canToggle, basicAuthReady, urls}`.
  - `urls` lists non-loopback, non-link-local interface addresses so the UI can
    show where to connect.
  - Enabling is refused with `400 basic_auth_required_for_external_access`
    unless `Server.BasicAuth.Enabled` and at least one user exists — the API
    runs bash and writes files, so an unguarded LAN bind is RCE.
  - `409` when `canToggle` is false, i.e. the process was started already
    exposed via `--host` (operator's choice is not taken away).
  - The new bind is **runtime-only**, never written to the config file: a
    restart is local again unless `Server.Host` is configured.
- `internal/api/routes.go`: route registered next to the basic-auth routes.

### Frontend
- **new** `web-ui/src/components/layout/ExternalAccessToggle.tsx` — globe button
  rendered in `StatusBar.tsx` (the footer). Hidden when the endpoint is missing
  or when `!canToggle && !enabled`; disabled with an explanatory tooltip when
  the bind came from `--host`. Without credentials it toasts and navigates to
  `/settings` (WebUI Access panel) instead of exposing the agent.
- `web-ui/src/components/layout/StatusBar.tsx` — renders the toggle before the
  model selector.
- i18n `externalAccess.*` added to all 7 locales.

## Notes
- The local browser keeps working after the flip because the API token bypasses
  the basic-auth gate (`hasValidToken`), as designed in [[webui_basic_auth]].
- `pando app` / `pando desktop` serve over TLS (self-signed), so the LAN URLs
  reported are `https://`.

## Verification
- **new** `internal/api/external_access_test.go`: real listeners —
  rebind loopback → `0.0.0.0` → loopback keeps `/health` answering; a rebind to
  an unassigned address (`203.0.113.7`) fails and restores the previous bind;
  handler refuses without credentials, reports `canToggle`, and returns 409 when
  the host came from the flag.
- `go test ./internal/api ./internal/config` — pass.
- `go build ./...` — pass. `npx tsc --noEmit` and `npm run build` in `web-ui` — pass.
