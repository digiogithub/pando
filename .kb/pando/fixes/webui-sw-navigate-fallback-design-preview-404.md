---
created_at: 2026-09-29T15:32:28.640488035Z
updated_at: 2026-09-29T15:32:43.068326646Z
---
# Fix: Design previews/canvas rendered the SPA 404 under the PWA service worker (2026-09-29)

## Symptom
Design Studio screens (artifact preview iframe, pop-out preview, canvas window) showed the WebUI "404 / Not found" page instead of the design. Decks also rendered without the design-system CSS.

## Fix 1: service-worker navigation fallback
`web-ui/vite.config.ts` VitePWA `generateSW` defaults `navigateFallback` to `index.html` with no denylist. Once the service worker controls the origin, every navigation (iframes and windows to `/preview/<token>/...` and `/preview/_canvas/<token>/`) is answered from the precached `index.html`, and React Router renders `NotFound`.

The service worker only registers on a trusted secure context:
- `http://localhost`, including the Wails desktop window: the bug shows up.
- `https` with the self-signed certificate: the worker fails to register, so the bug is hidden there.

Added `workbox.navigateFallbackDenylist: [/^\/preview\//, /^\/api\//, /^\/health$/]`.

Verified:
- Setup: plain-http proxy in front of `pando app`, Playwright with a persistent profile.
- **Before:** `/preview/...` returned `fromServiceWorker=true` and the Pando SPA page.
- **After:** preview and canvas both return 200 from the network with their real titles.
- The old service worker was replaced automatically (`autoUpdate`).

## Fix 2: design-system stylesheet 404 under preview
Artifacts link `../_system/system.css` (relative, see `stylesheetHref` in `internal/design/apply.go`). Under `/preview/<token>/` that resolves to `/preview/_system/system.css`: no token, and outside the grant's directory, so it returned 404. The browser also blocked the plain-text 404 body with a MIME error.

- `internal/design/preview/preview.go`:
  - New `Options.System func() (segment, dir string)`.
  - `systemDir` and `serveSystem` serve `/preview/<segment>/<file>` confined with `safeJoin`, applying the security headers and `no-store`. Directories return 404.
  - The route is off without a resolver.
- `internal/design/preview_link.go`: `PreviewOptions` wires `previewSystemDir`, which reads `ServiceFor("").Layout()` and returns `SystemDir` and `SystemPath()`, so a custom `Design.SystemDir` works.
- Tests:
  - `TestSystemDirectoryIsServedWithoutAToken`
  - `TestSystemRouteIsOffWithoutAResolver`
- Verified:
  - `go test ./internal/design/...` and `go test ./internal/api -run Design` pass.
  - Live check: `/preview/_system/system.css` returns 200 `text/css`, and traversal attempts do not leak files.
  - Playwright: the Studio deck iframe loads the `_system/system.css` stylesheet.

Related: [[pando/features/design_canvas_and_designer_brief.md]]
