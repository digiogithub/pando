---
created_at: 2026-09-24T22:09:17.855409821Z
updated_at: 2026-09-24T22:09:17.855409821Z
tags:
    - feature
    - webui
    - design
    - theme
---
# WebUI native redesign (epic PANDO-EP-0010, milestone PANDO-M-0003) — summary

**Status:** complete (P1–P7, 2026-09-24/25). Plan: [[pando/plans/webui_native_redesign_plan.md]].
Phase docs: [[pando/features/webui_redesign_p1_p2_foundations.md]], [[pando/features/webui_redesign_p3_app_shell.md]],
[[pando/features/webui_redesign_p4_chat_experience.md]], [[pando/features/webui_redesign_p5_settings_appearance.md]],
[[pando/features/webui_redesign_p6a_editor_terminal_agentvcs_design_overlays.md]], [[pando/features/webui_redesign_p6b_secondary_views.md]].

## Goal
The WebUI (`web-ui/`, React 19 + Vite + Tailwind 4 + zustand) looked improvised ("childish"). Target: a calm, native
desktop look close to Claude Desktop and Zeron: neutral surfaces, ONE restrained accent, soft radii, hairline borders,
subtle shadows, Inter / JetBrains Mono, no emoji or decorative gimmicks.

## Design system
- **Tokens** `web-ui/src/styles/tokens.css` (v2 only; the v1 aliases `--primary`, `--sidebar-bg`, `--bg-secondary`,
  `--fg-dim`, `--error`, `--space-*`… were deleted in P7 after a repo-wide grep showed 0 users):
  surfaces `--bg/--bg-shell/--bg-raised/--bg-card/--bg-input/--bg-overlay/--bg-hover/--scrim`, text `--fg/--fg-muted/--fg-faint`,
  lines `--border/--border-strong`, accent `--accent/--accent-fg/--accent-soft/--accent-hover/--focus-ring`, status + `-soft`,
  radii xs..pill, shadows sm/md/lg per mode, type scale, motion, control heights.
- **Tailwind** `@theme inline` in `src/index.css` maps tokens to utilities (`bg-shell`, `text-muted`, `bg-accent-soft`…).
- **CSS layering:** `ui.css` is imported as `layer(components)`; the reset and element defaults (`code/kbd`, global
  `:focus-visible`) live in `@layer base`; Tailwind utilities win over primitives; area CSS files (`styles/<area>.css`,
  prefixed `.chat-*`, `.shell-*`, `.settings-*`, `.editor-*`, `.view-*`…) are unlayered and override primitives.
- **Primitives** `src/components/ui/*` (Button, IconButton, Input, Textarea, Select, Switch, Checkbox, Card, Dialog,
  Popover, Menu, Tabs, SegmentedControl, Badge, Tooltip, Kbd, Spinner, Divider, SettingsSection/Row, EmptyState);
  usage guide `src/components/ui/README.md`. Icons: lucide only via `@/components/ui/icons` (FontAwesome fully removed,
  `@fortawesome/*` deps dropped in P7).

## Theme model
`family x mode x accent`, zustand store `src/hooks/useTheme.ts` (`setFamily/setMode/setAccent/toggleMode`).
Families pando (default, zinc + muted gold), paper, slate, forest; modes light|dark|system; accent presets
gold, terracotta, violet, blue, green, rose, graphite. Persisted in localStorage (`pando_theme`, `pando_accent`,
`pando_ui_size`); the `index.html` boot script applies attributes before paint. Backend `theme` (`family-mode`) is
still written through Settings > Appearance (shared with the TUI `cfg.TUI.Theme`).

### How to add a theme family or accent
1. Add the palette block to `tokens.css` (`[data-theme-name=X]` and `[data-theme-name=X][data-theme=dark]`), or for an
   accent the `:root[data-accent=A]` light/dark pair.
2. Mirror the hex values in `src/styles/themes.ts` (`FAMILY_PALETTES` / `ACCENT_PALETTES`, used by previews and chrome)
   and, for families, in the `bgs` map of the `index.html` boot script (and the accent list there).
3. Add the id to `THEME_FAMILIES` / `ACCENT_PRESETS` types in `useTheme.ts`.
4. Run `bun run check:contrast` — it checks WCAG contrast for every family x mode x accent and that themes.ts +
   index.html mirror tokens.css.

## Per-area summary
- **Shell** (P3): 44px title bar (brand 木 mark + "Pando"; version only in the brand tooltip since P7), sidebar
  (quiet "New session" row with SquarePen icon on a raised fill, search, collapsible Navigate/Sessions, icon rail,
  mobile drawer), 26px status bar (single connection indicator; model button hidden on chat routes since the composer
  chip shows it; Ctrl+O still opens the switcher), theme toggle Ctrl/Cmd+Shift+L.
- **Chat** (P4): no avatars, centered column, user bubble, grouped tool-activity rows, pill composer with model chip,
  empty state greeting + suggestions, token-based highlight.js theme, info sidebar with calm sentence-case headers
  and a status dot for the sandbox line.
- **Settings** (P5): native left-nav settings window, Appearance panel (mode, font size, family cards, accent swatches),
  all 22 panels on primitives. P7 added `.settings-page-header--row` (+ `-text`/`-actions`) used by MCP Servers,
  Providers and LSP, and a `.settings-list` row pattern (MCP servers list replaced a clipped 7-column table).
- **Secondary views** (P6): editor (Monaco theme from tokens), terminal (xterm theme from tokens), Agent VCS, design,
  overlays, splash (now the 木 mark), auth, orchestrator, logs, snapshots, evaluator, projects, instances, shared.

## P7 QA pass (PANDO-US-0057) — what was fixed
- Title bar version moved to tooltip; New session button de-golded; duplicate sidebar connection status removed;
  status-bar model hidden on chat routes; Simple Chat footer no longer repeats the model; splash uses the 木 mark.
- Sidebar shows "Loading…" instead of "No sessions yet" while the first fetch runs.
- Calm labels everywhere: removed uppercase/letter-spaced micro-labels (chat info sidebar, settings nav group, table
  headers, metric/detail/form labels, overlay/model-combobox group labels, Agent VCS panels, editor Explorer).
- Decorative accent removed from header icons (editor, terminal, Agent VCS, design, projects, instances), terminal
  active tab, cost value, UCB score, token counts; settings nav active item = raised fill + accent icon.
- Bugs: `/editor` overflowed the viewport (no status bar) because `#root` has no height — `.editor-shell` now uses
  `100dvh`; global `:focus-visible` and `code/kbd` rules were unlayered and beat `ui.css` (double focus ring on inputs,
  mono `<Kbd>`) — moved into `@layer base`; mobile: chat floating buttons got their own strip, Agent VCS panes and
  split panes (instances) stack, evaluator table/skills stack.
- i18n: `settings.appearance.*` (13 keys) translated in en/es/fr/de/pt/ja/zh.
- Cleanup: `@fortawesome/*` removed from package.json; v1 token aliases deleted; `ProvidersSettings.tsx` (orphan) and
  `components/shared/Tooltip.tsx` (orphan) deleted; dead CSS (`.chat-caret`, `.pwa-prompt-close`,
  `.design-export-item-hint`, `.inline-form-field--full`) removed. `.masked-input-wrap .ui-input` padding override kept
  (unlayered, harmless, still needed for the toggle).

## Verification (P7)
`bun run typecheck` 0 errors; `bun run lint` 0 errors / 4 pre-existing warnings (react-refresh in KeyValueEditor,
ModelCombobox); `bun run check:contrast` 762 checks OK; `bun run build` and `bun run build:embedded` OK (assets copied to
`internal/api/webui/dist`); `go build ./...` OK. Visual QA with headless Chrome (playwright-core driving
`/usr/bin/google-chrome`) of every route in light + dark at 1440x900 and 390x844, settings panels, paper/slate/forest
families, violet/rose accents and small/large font sizes. Note: Playwright's bundled chromium-1187 reports the
fontsource `@font-face`s as `error` (falls back to system font); real Chrome loads Inter fine — use system Chrome for
screenshots.

## Known follow-ups
- Composer attachments need backend support; git branch in composer meta needs an API.
- Separate TUI vs WebUI theme config field (both share `cfg.TUI.Theme`).
- macOS hidden-inset title bar in `desktop/main.go` (Wails) not yet enabled.
- Accent and UI font size are local-only (not persisted to the backend).
- xterm logs a `Cannot read properties of undefined (reading 'dimensions')` page error on `/terminal` mount
  (StrictMode double mount / renderer timing), harmless.
- Log level and instance-mode badges stay uppercase by convention; hardcoded English strings remain in several settings
  panels (MCP, Providers, LSP…).
