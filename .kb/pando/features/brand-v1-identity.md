---
created_at: 2026-09-25T08:30:36.481826765Z
updated_at: 2026-09-25T08:30:36.481826765Z
tags:
    - feature
    - brand
    - webui
    - desktop
    - tui
---
# Pando v1 brand identity (epic PANDO-EP-0011, 2026-09-25)

Adopts the styleguide in `assets/pando-brand-v1/` (source of truth, read-only) across WebUI, Desktop, TUI and the standalone Mesnada UI. Builds on [[webui-native-redesign]].

## Styleguide
- Symbol 木: monoline strokes with three circuit nodes.
- Palette:
  - Bosque `#0F2A20`
  - Marfil `#F4F1E8`
  - Álamo `#E9B949`
  - Álamo oscuro `#C68A17`
- Type: Space Grotesk for the wordmark and display text; JetBrains Mono for mono.
- Family: Remembrances 本 (the gold root stroke is stored memory) and Mesnada 众 (a solid lord node with ring retinue nodes).

## Stories
- **US-0059 Foundation (lead)**
  - `web-ui/src/components/brand/BrandMark.tsx`: `variant` = pando | remembrances | mesnada; props `size`, `tile`, `bold` (auto at 24px and below), `pulse`, `title`.
  - Bare mark: strokes use `currentColor`, nodes use `var(--brand-node)`. `tile` gives the fixed Bosque app-icon form.
  - `styles/brand.css`: `.brand-mark`, `.brand-mark--pulse` (node pulse, reduced-motion safe) and `.brand-display`.
  - Tokens `--brand-bosque/-marfil/-alamo/-alamo-dark/-node` and `--font-display` in `tokens.css`.
  - Dependency `@fontsource-variable/space-grotesk`, imported in `main.tsx`.
- **US-0060 WebUI shell**
  - BrandMark in the title bar (pulses while the agent is busy), empty chat (40px mark plus display greeting), SimpleChat header and splash (64px tile plus wordmark).
  - `useAnimatedLogo` was replaced by `hooks/useAgentBusy.ts`. The kanji cycle was dropped in the WebUI because 本 is now the Remembrances symbol.
  - `pando` theme family re-derived from the brand:
    - dark: bg `#0c1f18`, shell `#0a1a14`, fg Marfil, accent Álamo with Bosque text on accent;
    - light: bg `#fdfcf8`, shell Marfil, fg `#10251c`, accent `#8f6310`.
    - `#C68A17` fails AA as UI text, so it is used only for mark nodes.
  - `gold` preset realigned. `index.html` (favicon, apple-touch, theme-color, boot script) and the vite PWA manifest (Bosque theme/background) updated.
  - `web-ui/public/pando_mascot.svg` deleted (unused).
  - KB: [[webui_brand_b2_shell]].
- **US-0061 Section identities**
  - `components/brand/SectionHero.tsx` + `styles/sections.css`: 44px tile mark, Space Grotesk title, muted tagline, actions slot.
  - Used in `RemembrancesSettings`, `MesnadaSettings` and `OrchestratorView` ("Mesnada · Orchestrator").
  - Marks also used in the settings nav, the Sidebar orchestrator item (full and rail) and `extensions/MemorySyncIndicator.tsx` (Remembrances mark).
  - i18n `*.hero.{title,tagline}` added in all 7 locales.
  - KB: [[webui_section_identities_remembrances_mesnada]].
- **US-0062 Desktop / packaging**
  - `scripts/brand-sync.sh` (idempotent; inkscape + ImageMagick) generates:
    - `web-ui/public/{favicon.svg, pando-icon.svg, pwa-icon-192/512.png, pwa-icon-maskable.png, apple-touch-icon.png}`
    - `desktop/build/appicon.png`, `windows/icon.ico` and `darwin/iconfile.icns`
    - Mesnada UI favicons plus `mesnada-mark-light.svg`, which replaces `logo.jpg`.
  - Wails v2.16 facts:
    - macOS always regenerates the icns from `appicon.png`.
    - Windows uses `build/windows/icon.ico` as-is.
    - The release pipeline `scripts/build-macos-app` has its own `SRC_ICON`, repointed to `assets/pando-brand-v1/png/pando-icon-1024.png`.
  - Other changes:
    - `install-linux.sh` `ICON_URL` now points at the brand png-256.
    - `desktop/main.go` BackgroundColour is `{12,31,24}`.
    - `internal/mesnada/server/gin.go` now serves `.svg` as `image/svg+xml`.
    - README header uses a `<picture>` with the light/dark logos.
- **US-0063 TUI**
  - `theme/opencode.go` `PandoTheme`:
    - dark: Bosque backgrounds, Marfil text, Álamo primary;
    - light: accent darkened to `#A8730E`.
  - `styles/icons.go`: `RemembrancesIcon` 本 and `MesnadaIcon` 众 in both icon sets.
  - The orchestrator header and the settings sections "众 Subagents" / "本 KB & Code Index" render the glyph in accent via `brandSectionGlyph()`.
  - `logoAnimFrames` is now 枝葉林森 (本 removed).
  - KB: [[tui_brand_theme_glyphs]].

## Verification
- `bun run check:contrast`: 762 checks OK.
- `bun run typecheck`: clean.
- `bun run lint`: 0 errors (4 pre-existing warnings).
- `bun run build`: OK.
- `go build ./...` and desktop build: OK.
- `go test ./internal/tui/... ./internal/mesnada/...`: pass.
- Headless Chrome screenshots in light/dark and desktop/mobile reviewed by the lead.

## Follow-ups
- The standalone Mesnada UI still uses its purple `--brand: #7c5cff` accent (not re-themed).
- The Linux install icon URL only works once pushed to main.
