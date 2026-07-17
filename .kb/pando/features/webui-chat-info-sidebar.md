---
created_at: 2026-07-17T07:51:53.606518496Z
updated_at: 2026-07-17T07:51:53.606518496Z
tags:
    - feature
    - webui
    - chat
    - sidebar
---
# Feature: WebUI chat info sidebar (right panel, collapsible)

## What was built
A web-UI counterpart of the TUI chat info sidebar
(`internal/tui/components/chat/sidebar.go`), rendered to the right of the chat in
**both** the advanced view (`ChatView`) and the simple view (`SimpleChatView`).
It is collapsible: expanded it is a 264px panel, collapsed it shrinks to a 34px
rail holding only the toggle button.

## Information shown (parity with the TUI sidebar)
- **Session** — active session title (header, with the collapse control).
- **Usage** — context window as `used / window (pct%)` plus a colored progress
  meter (primary → warning ≥70% → error ≥90%); falls back to `Total tokens` when
  no context window is known. Then `Input / Output`, `Cache read/write` (only when
  non-zero), `Reasoning` (only when non-zero), `Cost` (only when `> 0`, matching
  the TUI fix in [[tui-sidebar-hide-zero-cost]]).
- **Subagents** — `running / unfinished` from the orchestrator tasks, section
  hidden when nothing is unfinished. Polled every 5s while the panel is open.
- **Plan** — TodoWrite entries with completed/in-progress/pending icons and a
  `completed/total` badge.
- **LSPs** — enabled LSP languages as chips, with a count badge.
- **Modified files** — path + `+additions` / `-removals` per file, or a
  "No modified files" placeholder.

## Files touched
- `web-ui/src/components/chat/ChatInfoSidebar.tsx` — **new**; the whole panel,
  its `Section`/`Row` helpers, `formatCount` (K/M suffixes, mirrors the TUI
  `formatCount`) and `meterColor`.
- `web-ui/src/stores/layoutStore.ts` — added `infoSidebarOpen`,
  `toggleInfoSidebar`, `setInfoSidebarOpen`, persisted in `localStorage` under
  `pando_info_sidebar_open`. Default: open only when `window.innerWidth > 1100`,
  the stored preference wins once the user has toggled it.
- `web-ui/src/components/chat/ChatView.tsx` — root became a flex row: existing
  column wrapped in `flex:1; minWidth:0`, then `<ChatInfoSidebar plan={activePlan} />`.
- `web-ui/src/components/chat/SimpleChatView.tsx` — sidebar added inside the
  body flex row, fed with `streamingState.plan`.
- `web-ui/src/i18n/locales/{en,es,fr,de,pt,ja,zh}.json` — new `chat.info.*` block
  (18 keys) in all 7 locales.

## Data sources
`useSessionStore` (session tokens/cost/context_window), `useFileChangesStore`
(modified files), `useLSPStore` (`/api/v1/config/lsp`), `useOrchestratorStore`
(`/api/v1/orchestrator/tasks`), `useLayoutStore` (toggle). No backend change was
needed — every field was already exposed.

## Reason
User asked for the TUI right-hand panel's information to exist in the WebUI, more
visually attractive, easily collapsible, and present in both the normal and the
simple chat views.

## Verification
- `npx tsc --noEmit` — clean.
- `npx eslint` on the 4 changed/added source files — clean.
- `npm run build` — succeeds (`✓ built in 2.05s`).

Related: [[tui-sidebar-hide-zero-cost]], [[project_tui_chat_info_sidebar_plan]],
[[feature_realtime_context_token_counter]], [[webui_phase2_chat]]
