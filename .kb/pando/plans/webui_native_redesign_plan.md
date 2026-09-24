---
created_at: 2026-09-24T21:00:20.89275713Z
updated_at: 2026-09-24T21:00:20.89275713Z
tags:
    - plan
    - webui
    - design
    - theme
---
# Plan: WebUI native redesign ("Pando Desktop" look)

**Date:** 2026-09-24
**Status:** In progress
**Tracker:** gintrack milestone + epic in project PANDO (see epic "WebUI native redesign")

## Motivation

The WebUI (`web-ui/`, React 19 + Vite + Tailwind 4 + zustand) grew by improvisation and looks "childish":
sharp 0px radii on the default theme, heavy gold, a mascot watermark behind the chat, avatar bubbles,
FontAwesome solid icons, ~1840 ad-hoc inline `style={{}}` objects (only ~32 `className` usages).
Goal: a serious, clean, native-feeling app close to Claude Desktop and to Zeron
(https://github.com/zeronsh/zeron, Rust/GPUI): neutral surfaces, one restrained accent,
generous whitespace, centered reading column, pill composer, compact tool-activity rows,
light/dark switchable with one button, colour scheme still configurable.

## Reference: what Zeron does (cloned to scratchpad, `crates/theme/src/builtins.rs`, `docs/theme-system.md`)

- Theme = family with resolved light + dark variants; semantic roles only
  (`background, shell, raised, card, text, muted, faint, accent, danger, warning, success, terminal_background`).
  Zeron Dark: bg `#060606`, shell `#0d0d0d`, raised `#343438`, card `#0e0e0e`, text `#e8e8ea`, muted `#a9a9ae`, faint `#85858a`, accent `#8b7cf6`.
  Zeron Light: bg `#ffffff`, shell `#f3f3f5`, raised `#ededf0`, text `#303035`, muted `#62626a`, faint `#797981`, accent `#5b43e8`.
- Accent is an independent selection (theme default or preset) that only recolours controls/focus/selection/caret, never syntax/status/diff.
- Light and dark variant selected independently from mode; mode toggle is cheap.
- Font: Geist / Geist Mono bundled; system UI optional.
- Chat: no avatars; user message = rounded bubble right-aligned in raised surface; assistant = plain prose in a centered ~720px column;
  tool activity collapsed to one muted row ("Ran 4 commands · read 1 file · called 1 tool", chevron to expand);
  floating "Scroll to bottom" pill; composer = rounded pill with placeholder "Do anything…", model chip, attach, circular send;
  below composer: workspace + git branch meta in faint text. Slim title bar with sidebar toggle.

## Current WebUI facts (verified)

- Tokens: `web-ui/src/styles/tokens.css` (331 lines) — families pando/claude/clay/starbucks via `data-theme-name` + `data-theme` light|dark. Pando default has `--radius-*: 0px`.
- `web-ui/src/index.css` holds `.pando-mascot-watermark` (used by `components/layout/MainLayout.tsx` and `components/chat/SimpleChatView.tsx`) — REMOVE.
- `hooks/useTheme.ts` is plain `useState` per component: Header toggle and GeneralSettings picker do not share state (bug). Must become a zustand store.
- Theme persisted in backend config field `theme` (`GeneralSettings.tsx`, id like `pando-light`) and in localStorage `pando_theme`.
- Header already has a sun/moon toggle (`components/layout/Header.tsx:273`).
- Icons: FontAwesome in 53 files / 238 usages.
- Desktop (Wails) window `BackgroundColour` hardcoded `R18,G18,B18` in `desktop/main.go:46`; `index.html` theme-color `#0a0a0f`.
- Pando design craft refs usable as guidance: `internal/design/bundles/craft/{color,typography,layout,interaction,anti-ai-slop}.md`, `internal/design/examples/claude.md`.
- Build: `cd web-ui && bun run typecheck && bun run lint && bun run build` (`build:embedded` for Go embed, `build:desktop` for Wails).

## Design decisions

1. **Semantic token system v2** (CSS custom properties, keep old names as aliases during migration so nothing breaks):
   surfaces `--bg`, `--bg-shell` (sidebar/titlebar), `--bg-raised` (user bubble, hover fill), `--bg-card`, `--bg-input`, `--bg-overlay` (menus/dialogs);
   text `--fg`, `--fg-muted`, `--fg-faint`; lines `--border`, `--border-strong`; accent `--accent`, `--accent-fg`, `--accent-soft`, `--focus-ring`;
   status `--danger`, `--warning`, `--success`, `--info` (+ `-soft` variants); radii `--radius-xs 4 / sm 6 / md 10 / lg 14 / xl 20 / pill 999`;
   shadows `--shadow-sm/md/lg` (tuned per mode); type `--font-sans`, `--font-mono`, `--text-xs 12 / sm 13 / base 14 / md 15 / lg 17 / xl 20 / 2xl 26`; motion `--ease`, `--dur-fast 120ms`, `--dur 180ms`.
   Tailwind 4 `@theme` maps these so utility classes can be used.
2. **Theme model**: `family` × `mode` × optional `accent`.
   Mode = `light | dark | system` (system follows `prefers-color-scheme`).
   Families: `pando` (default: neutral graphite/zinc like Zeron, refined gold accent `#C8A04A`-ish dark / `#9A7420`-ish light — contrast checked),
   `paper` (warm parchment + terracotta, Claude-Desktop-like), `slate` (cool blue-grey, blue accent), `forest` (deep green accent).
   Legacy ids map: `claude-*` to `paper`, `clay-*` to `paper`, `starbucks-*` to `forest` (no brand names in UI).
   Accent presets (gold, terracotta, violet, blue, green, rose, graphite) override only `--accent*`/`--focus-ring`.
   Persistence: localStorage (instant, no flash) + backend config `theme` string `family-mode` (backwards compatible) and new optional `accent`.
   An inline script in `index.html` applies attributes before React mounts (no FOUC). Wails window bg + `meta theme-color` follow the mode.
3. **Typography**: bundle Inter Variable (`@fontsource-variable/inter`) + JetBrains Mono (`@fontsource-variable/jetbrains-mono`) — works offline in embedded/desktop builds. Base 14px, prose 15px/1.65 in chat.
4. **Icons**: migrate FontAwesome to `lucide-react` (thin 1.5px stroke, native look) behind a small `Icon` re-export module; remove `@fortawesome/*` when done.
5. **UI primitives** in `web-ui/src/components/ui/`: Button (primary/secondary/ghost/danger, sm/md), IconButton, Input, Textarea, Select, Switch, Checkbox, Card, Dialog (modal with overlay + focus trap), Popover/Menu, Tabs/SegmentedControl, Badge, Tooltip, Kbd, Spinner, Divider, SettingsRow/SettingsSection, EmptyState. Styled with CSS classes in `styles/ui.css` (or Tailwind utilities) using tokens — no new inline style objects.
6. **Mascot watermark removed** from chat. Empty chat = centered greeting (small Pando mark + "What should we work on?" i18n) with the composer centered, Claude-style.
7. **Theme toggle button** in the title bar (sun/moon, one click, tooltip, `Ctrl/Cmd+Shift+L` shortcut) + full Appearance section in settings.

## Phases (stories)

- **P1 Foundations**: tokens v2, theme store (zustand) + no-FOUC init, fonts, legacy mapping, contrast validator script, Wails bg sync, remove watermark CSS.
- **P2 Primitives + icons**: `components/ui/*`, `ui.css`, lucide `Icon` module, docs page of primitives (dev-only route optional).
- **P3 App shell**: MainLayout, Sidebar (Claude-like: new chat, search, recents, collapsible), Header as slim title bar (Wails drag region, macOS traffic-light inset), StatusBar slimmed, theme toggle button, mobile.
- **P4 Chat**: MessageList/MessageBubble (no avatars, centered column, user bubble), tool-activity summary rows, ChatInput pill composer, empty-state greeting, Permission/Question dialogs, SlashCommandMenu, PlanView, FileChangesBar, DiffViewer, GoalStatus, ChatInfoSidebar, SimpleChatView; markdown/code block styling (highlight.js theme from tokens).
- **P5 Settings + Appearance**: settings shell (left nav + rows), Appearance section (mode, family with palette preview, accent swatches, font size), migrate 23 settings panels to primitives.
- **P6 Secondary views**: editor (Monaco theme from tokens), terminal (xterm theme from tokens), orchestrator, logs, snapshots, evaluator, design, projects, instances, extensions, agentvcs, overlays/QuickMenu, splash, auth, shared components.
- **P7 QA + cleanup**: finish FontAwesome removal, delete unused tokens/aliases, visual check light/dark × desktop/mobile via browser screenshots, contrast check, typecheck/lint/build/build:embedded, docs + KB.

## Execution

Orchestrated by Claude Code subagents: P1+P2 first (one agent, foundation), then P3, P4, P5, P6 in parallel (disjoint folders; shared files edited only with targeted Edit, i18n keys added under own namespace), then P7.
