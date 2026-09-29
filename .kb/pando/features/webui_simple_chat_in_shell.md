---
created_at: 2026-09-29T22:13:34.695118676Z
updated_at: 2026-09-29T22:13:34.695118676Z
tags:
    - feature
    - webui
    - desktop
    - chat
---
# Feature: simple chat mode inside the app shell + port-independent persistence

Date: 2026-09-30. Builds on [[pando/features/desktop_frameless_titlebar_tray.md]] and [[pando/features/webui_redesign_p3_app_shell.md]].

## Problem
- `/chat/simple` was a standalone route with its own old-style header and sessions list. In the frameless desktop window it got the slim `DesktopFrameBar` on top of its own header (two bars, old look).
- The chosen mode did not survive restarts: it lived only in `localStorage` (`pando_chat_mode`), which is per origin. The desktop webview and project instances come up on different ports (`~/.local/share/pando-desktop/localstorage/` had 8765, 8766, 8768, 8821, 9872...), so each port started in advanced mode.

## What changed
- **Route**: `chat/simple` is now nested under `MainLayout` (`web-ui/src/App.tsx`). `SimpleChatView` is only the chat pane + `ChatInfoSidebar` + small footer (commands, connection, tokens). Auth/health/shortcuts/overlays come from MainLayout.
- **Shell variant** driven by `layoutStore.chatMode === 'simple'` (`MainLayout.tsx`):
  - `Header simple`: same title bar (sidebar toggle, brand, title, window controls). Advanced actions (persona, simple-chat button, docs, settings icon) hidden; shows a "Full view" button (`LayoutDashboard` icon + label, i18n `header.fullView`/`header.fullViewHint` in 7 locales) and the theme toggle.
  - `Sidebar simple`: no Navigate section; new session, session search, session list, Settings at bottom. New session / session click navigate to `/chat/simple` when not there. No rail: collapsed = hidden, title bar toggle (Ctrl+B) reopens it.
  - `StatusBar` hidden in simple mode. Settings opens inside the simple shell.
- **Persistence**: new endpoint `GET/PUT /api/v1/ui/preferences` (`internal/api/handlers_ui_prefs.go`, route in `routes.go`) storing `{"chatMode"}` in `$XDG_CONFIG_HOME/pando/webui-prefs.json` (atomic write, mutex). `layoutStore.setChatMode` also PUTs; `hydrateChatMode()` (called by MainLayout after authenticate) adopts the server value unless the user already chose a mode in this page load. `InitialModeRedirect` redirects `/` to `/chat/simple`; `/?mode=advanced` forces advanced.
- `SimpleChatView` sets simple mode on mount (sidebar link, desktop `--simple`), and navigates to `/` if the mode later flips to advanced.
- Desktop menu "Simple Mode" off now loads `/?mode=advanced` (`internal/desktop/app.go toggleMode`) so it does not bounce back to simple.
- Removed obsolete simple header/sessions CSS (`styles/chat.css`), `.chat-simple` height now 100%; dropped `.chat-simple` from the desktop-frame rule. Added `LayoutDashboard` to `components/ui/icons.ts`.

## Verification
- `go test ./internal/api ./internal/desktop` (new `TestUIPrefsEndpointRoundTrip`).
- `tsc -p tsconfig.app.json --noEmit`, eslint on touched files.
- Playwright E2E against `pando app` (isolated HOME): switch to simple → prefs `simple`; fresh browser context (empty localStorage, simulates another port) opening `/` lands on `/chat/simple`; collapse/expand sidebar; Settings stays in simple shell; "Full view" → `/` and prefs `advanced`.
