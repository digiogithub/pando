---
created_at: 2026-06-29T08:04:25.150377482Z
updated_at: 2026-06-29T08:04:25.150377482Z
tags:
    - fix
    - desktop
    - pwa
    - service-worker
    - web-ui
---
# Fix: Desktop shows stale web-UI after a binary update (until restart)

**Date:** 2026-06-29
**Status:** Done

## Symptom
Launching Pando in desktop (Wails) mode after updating the binary shows the
PREVIOUS web-UI. You had to close the desktop window and reopen it for the
latest UI to appear.

## Root cause
The desktop wrapper (`desktop/main.go` + `internal/desktop`) is just a WebView
that loads the Pando server URL (`http://localhost:8765`); the actual UI is the
embedded web-UI served by `internal/api` from `webui/dist`. That web-UI is a
**PWA** built with `vite-plugin-pwa` (Workbox): `sw.js`, `registerSW.js`,
`workbox-*.js`, `manifest.webmanifest`.

`vite.config.ts` uses `registerType: 'autoUpdate'`, and the generated `sw.js`
correctly contains `skipWaiting` + `clientsClaim`. **But** the source never
imported `virtual:pwa-register`, so vite-plugin-pwa only auto-injected the
*bare* `registerSW.js` (`navigator.serviceWorker.register('/sw.js')`) with **no
reload-on-update logic**.

Result of the classic service-worker lifecycle:
1. Update binary → new `sw.js` + new content-hashed bundles.
2. First launch: old SW serves the cached app shell → OLD UI renders. The new
   SW installs/activates in the background but the already-rendered page is
   never reloaded.
3. Close + reopen: the now-active new SW serves the new shell → NEW UI.

## Fix
1. **`web-ui/src/main.tsx`** — explicitly register the SW with auto-update +
   reload: `import { registerSW } from 'virtual:pwa-register'` then
   `registerSW({ immediate: true })`. Combined with the autoUpdate worker, the
   new worker takes control and the virtual module reloads the page on
   `controllerchange`. Verified rebuilt bundle now contains `controllerchange`
   (workbox-window) + `location.reload`, and `index.html` no longer references
   the bare `registerSW.js`.
2. **`web-ui/src/vite-env.d.ts`** — added
   `/// <reference types="vite-plugin-pwa/client" />` so TS resolves the virtual
   module.
3. **`internal/api/server.go`** — defensive HTTP cache headers so the WebView's
   HTTP cache can never shadow a SW update:
   - `serveStaticAsset`: `assets/` (content-hashed) →
     `Cache-Control: public, max-age=31536000, immutable`; everything else
     (sw.js, registerSW.js, workbox-*, manifest, icons) → `Cache-Control: no-cache`.
   - `serveIndexHTML`: `Cache-Control: no-cache` (SPA/PWA entry point must always
     revalidate).

## Files touched
- `web-ui/src/main.tsx`
- `web-ui/src/vite-env.d.ts`
- `internal/api/server.go` (`serveStaticAsset`, `serveIndexHTML`)
- Rebuilt embedded assets: `internal/api/webui/dist/*`

## Verification
- `go build ./internal/api/` OK
- `go test ./internal/api/` OK
- `make web-ui-embedded` (bun + tsc + vite + compress) OK; rebuilt
  `dist/sw.js`, `registerSW.js` and bundle. Confirmed `controllerchange` +
  `location.reload` present in the new JS bundle and `index.html` references the
  hashed entry chunk instead of the bare register script.

## Note
The fix only takes effect once the web-UI is rebuilt and re-embedded (the
`dist` is committed). A standard `make build` / `make web-ui-embedded` does this.
