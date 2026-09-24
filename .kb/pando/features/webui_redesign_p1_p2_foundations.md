---
created_at: 2026-09-24T21:16:00.40067954Z
updated_at: 2026-09-24T21:16:00.40067954Z
tags:
    - feature
    - webui
    - design
    - theme
---
# WebUI redesign P1+P2: tokens v2, theme store, fonts, UI primitives (2026-09-24)

Part of [[pando/plans/webui_native_redesign_plan.md]] (stories PANDO-US-0051, PANDO-US-0052).

## What changed
- `web-ui/src/styles/tokens.css` rewritten: semantic tokens v2 (`--bg`, `--bg-shell`, `--bg-raised`, `--bg-card`, `--bg-input`, `--bg-overlay`, `--bg-hover`, `--scrim`, `--fg`, `--fg-muted`, `--fg-faint`, `--border`, `--border-strong`, `--accent`, `--accent-fg`, `--accent-soft`, `--accent-hover`, `--focus-ring`, status + `-soft`, radii xs..pill, shadows, fonts, type scale, motion, `--control-h`). Old v1 names kept as aliases. Families pando (default, zinc + muted gold), paper, slate, forest; accent presets via `data-accent` (gold, terracotta, violet, blue, green, rose, graphite). Cascade: `:root` = pando light, `:root[data-theme=dark]` = pando dark + shared dark, `[data-theme-name=X]` / `[...][data-theme=dark]`, accents `:root[data-accent=A]:not([data-theme=dark])` (0,3,0).
- `web-ui/src/styles/themes.ts`: preview/chrome hex copies (family palettes, accents).
- `web-ui/src/hooks/useTheme.ts`: zustand `useThemeStore` + compat `useTheme()` (family, mode light|dark|system, resolvedMode, accent, setFamily/setMode/setAccent/setTheme/toggleMode, themeId/themeName/themeMode). localStorage `pando_theme` + `pando_accent`; legacy ids claude/clay->paper, starbucks->forest; system mode follows prefers-color-scheme live; syncs `meta theme-color`, html bg and Wails `window.runtime.WindowSetBackgroundColour` when present. `isWebThemeId`, `hasStoredTheme` helpers.
- NOTE: backend `theme` field is `cfg.TUI.Theme` (shared with TUI, default `pando-nobg`). GeneralSettings now only adopts backend value if it is a WebUI id and no local choice exists.
- `web-ui/index.html`: inline boot script applying data-theme / data-theme-name / data-accent before React (no FOUC).
- Fonts: `@fontsource-variable/inter`, `@fontsource-variable/jetbrains-mono` imported in main.tsx. `lucide-react` added; `<IconProvider size=16 strokeWidth=1.75>` in main.tsx.
- `index.css`: Tailwind `@theme inline` mapping (bg-shell, text-muted, ...), reset moved into `@layer base` (so Tailwind spacing utilities work), modern scrollbars, markdown, selection, focus-visible. Mascot watermark removed (CSS + JSX in MainLayout, SimpleChatView).
- Primitives in `web-ui/src/components/ui/` (Button, IconButton, Input, Textarea, Select, Switch, Checkbox, Card, Dialog, Popover, Menu/MenuItem/MenuSeparator/MenuLabel, Tabs, SegmentedControl, Badge, Tooltip, Kbd, Spinner, Divider, SettingsSection/SettingsRow, EmptyState), styles in `web-ui/src/styles/ui.css`, icons module `components/ui/icons.ts` (FA->lucide map), usage guide `components/ui/README.md`.
- ThemePicker rewritten (mode segmented + family cards + accent swatches); i18n keys themeSystem/accent/accentDefault in 7 locales.
- `web-ui/scripts/check-contrast.mjs` + `bun run check:contrast`: WCAG checks for all family x mode x accent, plus themes.ts/index.html sync with tokens.css.
- vite PWA theme/background colour #111113.

## Verification
bun install, typecheck, `tsc -p tsconfig.app.json`, lint (0 errors, 4 pre-existing warnings), build, check:contrast (762 checks) all pass. Dev server checked in headless browser: theme attrs, cascade for families/accents, header toggle persists, reload applies stored theme. Backend was not running (no full visual screenshot review).
