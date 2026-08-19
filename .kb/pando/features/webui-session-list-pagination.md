---
created_at: 2026-08-19T11:14:36.685885Z
updated_at: 2026-08-19T11:14:36.685885Z
tags:
    - feature
    - webui
    - api
    - pagination
    - sessions
---
# Feature: paginated session lists (WebUI + API) — 2026-08-19

## Motivation
Both session lists loaded every session at once: `/api/v1/sessions` returned the full table
(514 rows on the dev machine) and the Instances panel fetched every remote session over IPC.
The sidebar then hid the excess with a hard `slice(0, 20)` / `slice(0, 30)`, so old sessions
were unreachable while the payload stayed huge.

## Backend
- New `internal/api/pagination.go`: `defaultSessionPageSize = 100`, `maxPageSize = 500`,
  `paginationParams(r, def)` (reads `?limit=` / `?offset=`, clamps) and the generic
  `paginate[T](items, limit, offset)` window helper (never returns nil).
- `internal/api/handlers_sessions.go` `handleSessions`: windows the (already
  `updated_at DESC`) list and responds `{sessions, total, limit, offset, has_more}`.
  Default page = 100 when the client sends nothing.
- `internal/api/handlers_instances.go` `handleInstanceListSessions`: same windowing over the
  `session.list` RPC result, same envelope fields.

## Frontend
- `web-ui/src/stores/sessionStore.ts`: `SESSIONS_PAGE_SIZE = 100`, state
  `sessionsTotal` / `sessionsHasMore` / `sessionsLoadingMore`, `fetchSessions` requests the
  first page, new `loadMoreSessions()` appends the next one (dedupes by id, since a session
  can move between pages when it is updated mid-paging).
- `web-ui/src/stores/instancesStore.ts`: same shape for remote sessions
  (`REMOTE_SESSIONS_PAGE_SIZE`, `remoteSessions*` state, `loadMoreRemoteSessions()`).
- `Sidebar.tsx`: dropped `slice(0, 20)`; renders all loaded sessions plus a
  "Load more (n/total)" button (i18n `common.loadMore` / `common.loading`).
- `SimpleChatView.tsx`: dropped `slice(0, 30)`; scroll-near-bottom (<80px) triggers
  `loadMoreSessions()`, with the same button as fallback.
- `RemoteSessionView.tsx`: header shows `n/total`, list lazy-loads on scroll and has a
  "Load more" button.
- i18n `common.loadMore` + `common.loading` added to en/es/de/fr/pt/ja/zh.

## Verification
- `go build ./...`, `go test ./internal/api ./internal/ipc/...` — pass.
- `npx tsc --noEmit` clean, `npm run build` OK.
- Live via `pando serve --port 8799` (HTTPS self-signed):
  `/api/v1/sessions?limit=2` -> `{total: 514, limit: 2, offset: 0, has_more: true}` with 2 rows;
  remote `/instances/{id}/sessions?limit=3&offset=0|3` -> `total: 11`, disjoint id windows.
  Test server stopped afterwards.

Related: [[webui-instances-panel-sessions]], [[project_code_search_token_optimization]]
