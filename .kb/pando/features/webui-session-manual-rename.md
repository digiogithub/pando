---
created_at: 2026-10-08T12:25:30.969588486Z
updated_at: 2026-10-08T12:25:30.969588486Z
tags:
    - feature
    - webui
    - desktop
    - sessions
---
# Feature: manual session rename from the WebUI/desktop sidebar — 2026-10-08

## What
Hovering a session row in the sidebar session list reveals a pencil button. Clicking it swaps the row for an inline input (pre-filled, text selected). Enter or blur saves, Escape cancels. Empty or unchanged titles are ignored. Not available in the TUI (by design).

## Files / symbols
- `web-ui/src/components/layout/Sidebar.tsx` — state `editingId`/`editTitle` + `editingRef` (sync guard so the trailing blur fired when Enter/Escape unmount the input is a no-op: no double PATCH, Escape never saves), `startRename`, `cancelRename`, `commitRename` (toast on error). Rows are now `div.shell-session-row` wrapping the session button + `button.shell-session-edit` (no nested buttons).
- `web-ui/packages/pando-client/src/stores/sessionStore.ts` — `renameSession(id, title)`: `PATCH /api/v1/sessions/{id}` `{title}`, then updates the title in `sessions` locally.
- `web-ui/src/styles/shell.css` — `.shell-session-row`, `.shell-session-edit` (opacity 0, visible on row hover/focus-visible; 0.7 on `hover: none` touch devices), `.shell-session.is-editing`, `.shell-session-input`.
- `internal/api/handlers_sessions.go` `handlePatchSession` — existing PATCH endpoint now trims the title and rejects empty with 400.
- i18n: `shell.renameSession`, `shell.renameSessionFailed` in en/es/de/fr/pt/ja/zh.

## Notes
- LLM title generation only runs on the first message of a session (`agent.processGeneration` when no messages), so a manual title is not overwritten later.
- Desktop (Wails) uses the same WebUI, so it gets the feature automatically.

## Verification
- `go test ./internal/api` ok.
- `npx tsc -b --noEmit`, eslint on touched files: clean.
- `npx vitest run src/components/layout`: 13/13 pass.
- E2E in a real browser (2026-10-08): `pando app` with isolated HOME, sessions seeded in `.pando/pando.db` (note `ListSessions` hides rows with `message_count = 0`), Playwright + headless system Chrome. Pencil opacity 0 -> 1 on hover; input pre-filled and focused; Escape sends no PATCH; Enter sends one trimmed PATCH; blur saves; whitespace-only sends nothing; titles persist after reload.
- Follow-up from E2E: editing row shrank (no meta line) and the list jumped, so `.shell-session.is-editing` got `min-height: 47px` (measured: normal row 47px, editing row 47px after fix).

Related: [[pando/features/webui-session-list-pagination.md]], [[pando/changes/session-titles-from-prompt-and-task-prompt-detail.md]]
