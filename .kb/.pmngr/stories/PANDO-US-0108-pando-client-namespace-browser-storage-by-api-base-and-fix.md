---
id: PANDO-US-0108
type: story
title: "pando-client: namespace browser storage by API base and fix base-URL bypasses so the child UI can run under the proxy path"
status: backlog
priority: high
parent: PANDO-EP-0019
author: mcp
labels: [projects, webui, pando-client]
estimate: 5
created: 2026-10-01T19:24:42Z
updated: 2026-10-01T19:24:42Z
---

## Description

The embedded child WebUI will be served from `/api/v1/projects/{id}/web/` on the **same origin** as the parent, so `localStorage`/`sessionStorage` are shared. Today every key is global (`pando_token`, `pando_language`, `pando_sidebar_open`, theme, chat mode, drafts, collapsed sections…): the iframe would log the parent out or swap its language.

- `packages/pando-client/src/services/storage.ts` (new): `storageKey(name)` → `${name}` for the main instance, `${name}@${apiBasePathHash}` when `window.__PANDO_API_BASE__` is a non-empty path. Migrate every direct `localStorage.getItem/setItem` in `packages/pando-client` and `web-ui/src` to it (grep: `localStorage`, `sessionStorage`). Wrap in try/catch as today.
- Base URL correctness: `hooks/useChat.ts:553` must use `getBaseURL()` for `/api/v1/chat/stream`; `useChat.ts:602` and `components/instances/RemoteSessionView.tsx:88` must use `getBaseURL()` instead of `window.__PANDO_API_BASE__`; audit `fetch(` / `new EventSource(` / `new WebSocket(` call sites and `services/terminalPty.ts`.
- Router basename: `src/App.tsx` `createBrowserRouter(routes, { basename: window.__PANDO_ROUTER_BASENAME__ ?? '/' })`; Vite `base: './'` or runtime `<base href>` so hashed assets resolve under the proxy path; service worker/PWA registration disabled in child mode.
- `api.ts` 401 handling reloads the page — in child mode it must reload only the iframe (it does, `location.reload()` inside the frame) and must not clear the parent's key (covered by namespacing).
- Extension panel host (`src/lib/pandoUI.ts`) exposes `apiBase` — keep consistent.

## Acceptance Criteria

- [ ] Running the built UI under a prefix (`PANDO_PUBLIC_BASE=/x/y` on `pando serve`) loads, authenticates and chats; main-instance storage keys are untouched (Playwright check of `localStorage` keys).
- [ ] No remaining direct reads of `window.__PANDO_API_BASE__` outside `services/api.ts` (lint rule or grep test).
- [ ] `bun run build` and `bun run test` pass; existing E2E (`web-ui/e2e`) still green.
