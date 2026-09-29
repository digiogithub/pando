---
created_at: 2026-09-29T15:16:43.397520269Z
updated_at: 2026-09-29T15:16:43.397520269Z
tags:
    - fix
    - desktop
    - webui
---
# Fix: Desktop window shows no sessions / no project config (2026-09-29)

## Symptom
`pando desktop` opened with an empty UI: no sessions, project config seemingly ignored. `pando app` in the same folder worked.

## Root cause
Not a config lookup problem. The backend (`cmd/desktop.go`) loads config from the cwd exactly like `pando app`; verified via curl that `/api/v1/sessions` and `/api/v1/setup/status` returned the right folder data (repo, /www/digio-devops, $HOME).

Since the frameless/tray commit f033c1a46 ([[desktop_frameless_titlebar_tray]]) the Go wrapper re-injects the Wails runtime (`window.go`) on the Pando server origin. `isDesktop` (`web-ui/packages/pando-client/src/services/host.ts`, `'go' in window`) became true on that page. `authenticate()` in `web-ui/packages/pando-client/src/services/auth.ts` returned `''` in desktop mode, assuming `initDesktopMode` had injected a token via the `GetServerInfo` binding. That binding is stale (Go `desktop.App` no longer exposes it), so no token was ever set: every API call was 401, never POST `/api/v1/token`.

## Fix
Removed the `if (isDesktop) return ''` short-circuit in `authenticate()`: the webview is same-origin with the server and exchanges the token like the browser.

## Verification
- `bun run typecheck`, `make build` OK.
- Logging reverse proxy between the real Wails wrapper (`desktop/build/bin/pando-desktop --url ...`) and `pando app`: before the fix all `/api/v1/*` were 401 with no token and no `/api/v1/token` call; after the fix POST `/api/v1/token` 200 and sessions/settings/setup/status 200 with the project data.

## Follow-ups (not done)
- `web-ui/wailsjs/go/desktop/App.js` `GetServerInfo` binding is stale; `getDesktopConfig()` always falls back to null.
- `/api/v1/extensions/ui` is requested before auth completes (one 401, harmless).
- Launching Pando.app from Finder runs with cwd `/`; `pando desktop` from `/` on Linux fails with `mkdir ./.pando: permission denied`.
