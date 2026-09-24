---
created_at: 2026-09-24T21:26:36.884604582Z
updated_at: 2026-09-24T21:26:36.884604582Z
tags:
    - feature
    - webui
---
# WebUI redesign P3 — App shell (PANDO-US-0053)

Part of [[webui_native_redesign_plan]] (epic PANDO-EP-0010). Builds on [[webui_redesign_p1_p2_foundations]].

## What changed
- New `web-ui/src/styles/shell.css` (`.shell-*`, tokens only), imported from `MainLayout.tsx`.
- `components/layout/MainLayout.tsx`: `.shell` grid (title bar / body [sidebar + `.shell-main` content frame with hairline + rounded top-left] / status bar). Sidebar variants: `full` (260px), `rail` (52px icon rail when collapsed on desktop), `drawer` (mobile <=768px overlay with scrim, Esc closes). Desktop expanded/rail state persisted in localStorage `pando_sidebar_open` via `useLayoutStore.subscribe` (a mount-time effect write clobbered the stored value under StrictMode). Drawer closes on `location.pathname` change (old effect depended on `navigate`, never fired). Removed old unscoped `<style>` block.
- `components/layout/shellHooks.ts` (new): `useMediaQuery`, `MOBILE_QUERY`, sidebar pref read/write, `isMacPlatform`, `needsMacTrafficLightInset()` (desktop + mac + outerHeight-innerHeight<4 => `data-mac-inset` 76px left inset).
- `Header.tsx` -> 44px title bar on `--bg-shell`, `--wails-draggable: drag` (controls `no-drag` in CSS). Sidebar toggle (PanelLeft/PanelLeftClose), brand glyph (animated logo) + "Pando" + short version (full in title attr), centred title (section, plus active session title on chat routes, ext panel titles), right: PersonaSelector, Simple Chat, Docs, theme toggle (Sun/Moon, tooltip "Switch to dark/light mode (Ctrl|⌘+Shift+L)"), Settings. Header nav tabs + mobile hamburger dropdown removed: navigation lives in sidebar (rail keeps it reachable when collapsed; drawer on mobile).
- `Sidebar.tsx`: New session button (also navigates to `/` from other routes), session search (client-side filter title/prompt_preview), collapsible Navigate + Sessions sections (state in `pando_sidebar_sections`), 32px lucide rows, bottom Settings + connection status. All NAV items + extension panels kept.
- `StatusBar.tsx` (26px, faint, `.shell-status-btn`), `ExternalAccessToggle.tsx` restyled; auto-accept labels now i18n.
- `App.tsx`: global Ctrl/Cmd+Shift+L -> `useThemeStore.getState().toggleMode()` (works on standalone routes too); suspense fallback uses classes.
- `components/ui/icons.ts`: added `FastForward`. i18n: new `shell.*` namespace in all 7 locales.
- Zero FontAwesome in layout/ and App.tsx.

## Verification
- `bun run typecheck`: no errors in layout/App/shell (others' transient errors elsewhere). `eslint src/components/layout src/App.tsx`: clean.
- Playwright screenshots desktop light/dark, rail, mobile, mobile drawer. Scripted check: shortcut and button toggle theme, persisted across reload, works on `/editor`.
