---
created_at: 2026-10-01T19:52:43.397401454Z
updated_at: 2026-10-01T19:52:43.397401454Z
tags:
    - feature
    - webui
    - projects
    - pando-client
---
# Feature: WebUI storage namespacing and path-prefix support (PANDO-US-0108)

Part of [[project_workspaces_webui_tabs]] (epic PANDO-EP-0019). Date: 2026-10-01.

## Why
The child WebUI of a project will be served by the parent under `/api/v1/projects/<id>/web/` on the same origin and shown in an iframe. Shared origin = shared `localStorage`, so un-namespaced keys (`pando_token`, `pando_language`, theme…) would clobber the parent.

## What changed
- `web-ui/packages/pando-client/src/services/storage.ts` (new): `storageKey(name)` = `name` for the main instance, `name@<fnv1a-8hex of API base path>` when `window.__PANDO_API_BASE__` has a non-empty path; `localBrowserStorage` / `sessionBrowserStorage` safe wrappers. Every direct `localStorage`/`sessionStorage` use in `packages/pando-client/src` and `web-ui/src` migrated; main-instance key names unchanged. `index.html` inline bootstrap mirrors the derivation for theme/UI-scale before first paint.
- `services/api.ts`: `normalizeBaseURL`, `resolveAPIURL(path)`; `hooks/useChat.ts`, `RemoteSessionView.tsx`, `services/terminalPty.ts` and health fetches now go through `getBaseURL()`/`resolveAPIURL`.
- Router basename: `src/lib/runtimeConfig.ts` (`getRouterBasename`, `isRootRouterBasename`) from `window.__PANDO_ROUTER_BASENAME__`; `createBrowserRouter(..., { basename })` in `src/App.tsx`; service worker registration and PWA install prompt skipped when the basename is not `/`.
- Build: Vite `base` = `VITE_BASE_URL || './'` (relative assets) plus a static `<base href="/" />` in `index.html` so deep links (`/chat/simple`, `/design/:id`) still resolve assets from the root. A server that serves the UI under a prefix must rewrite that href to `<prefix>/`.
- Guard tests: `src/runtimeGuards.test.ts` (no `__PANDO_API_BASE__` read outside `services/api.ts`/`storage.ts`, no raw storage access outside `storage.ts`), `services/storage.test.ts`.

## Contract for the server side (PANDO-US-0105)
Inject, before the inline bootstrap script of `index.html` (i.e. right after `<head>`), `window.__PANDO_API_BASE__` and `window.__PANDO_ROUTER_BASENAME__` = `/api/v1/projects/<id>/web`, and rewrite `<base href="/">` to `<base href="/api/v1/projects/<id>/web/">`. Note `Server.serveIndexHTML` skips injection for pre-compressed (`.br`/`.gz`) index files, so prefixed mode must serve the uncompressed document.

## Verification
From `web-ui/`: `bun run typecheck` (clean), `bun run lint` (0 errors, 6 pre-existing warnings), `bun run test` (12 files, 67 tests passed), `bun run build` (ok, `dist/index.html` carries `<base href="/" />`).