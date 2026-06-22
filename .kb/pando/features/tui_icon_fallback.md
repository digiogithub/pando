---
created_at: 2026-06-19T14:03:55.445393848Z
updated_at: 2026-06-19T14:03:55.445393848Z
tags:
    - feature
    - tui
    - icons
    - nerdfonts
    - config
---
# Feature: TUI Nerd Font icon fallback system (2026-06-19)

## Motivation
The TUI prints Nerd Font glyphs (Private Use Area codepoints) for all icons in
`internal/tui/styles/icons.go`. These are rendered by the **terminal emulator's
font**, not by the pando binary, so embedding a `.ttf` with `go:embed` cannot
help: a TUI app cannot inject a font into the terminal's render pipeline. On
terminals without a patched Nerd Font the icons appear as tofu. The fix is a
runtime-swappable fallback icon set using widely-supported BMP/ASCII symbols
(non-PUA), which every terminal renders without a special font.

## What changed
- **`internal/tui/styles/icons.go`** (rewritten): introduced an unexported
  `iconSet` struct bundling every glyph plus `SpinnerFrames`/`GenericFile`. Two
  instances: `nerdIconSet` (historical defaults) and `asciiIconSet` (fallback,
  e.g. Check, Error, ArrowRight, Folder, spinner frames `|/-\`). The exported
  icon identifiers (`CheckIcon`, `FolderIcon`, ..., and `SpinnerFrames`) were
  changed from `const` to package `var`s so the active set can be swapped
  without touching any call site. New API:
  - `SetNerdFonts(enabled bool)` — swaps the active set (uses `applyIconSet`).
  - `NerdFontsEnabled() bool` — atomic read of the current mode.
  - `init()` defaults to the Nerd Font set (preserves behavior).
  - `FileIconFor` now returns `DocumentIcon` for every file when fallback mode
    is active (the per-extension maps are all Nerd Font glyphs).
- **`internal/config/config.go`**: added `TUIConfig.NerdFonts *bool`
  (nil/absent = enabled, the default). Added method `(*Config).NerdFontsEnabled()`
  that honors env override `PANDO_NERD_FONTS` (0/1/true/false/on/off) first, then
  the config field, defaulting to true. Added `UpdateNerdFonts(enabled bool)`
  persisting helper (mirrors `UpdateShowHiddenFiles`).
- **`internal/app/app.go`**: new `initIcons()` called right after `initTheme()`
  in app startup; calls `styles.SetNerdFonts(config.Get().NerdFontsEnabled())`.
- **`internal/tui/page/settings.go`**: added a "Nerd Font Icons" toggle in the
  General section (`tui.nerdFonts`); save handler calls `UpdateNerdFonts` and
  `styles.SetNerdFonts` for live hot-swap.
- **`internal/api/handlers_settings.go`**: added `nerd_fonts` to the settings
  GET response and PUT request; PUT applies live via `styles.SetNerdFonts`.
- **web-ui**: `types/index.ts` (`nerd_fonts: boolean`), `stores/settingsStore.ts`
  default `true`, `components/settings/GeneralSettings.tsx` new Toggle, and i18n
  keys `settings.general.nerdFonts(/Description)` in en/es/de/fr/pt/zh/ja.

## Configuration surface
- TOML/JSON: `tui.nerdFonts = false` to use fallback.
- Env override: `PANDO_NERD_FONTS=0` (highest priority).
- TUI: General settings -> "Nerd Font Icons" toggle (live swap).
- Web-UI/API: General settings toggle / `PUT /api/v1/settings {"nerd_fonts":false}`.

## Verification
- `go build ./...` — OK.
- `go test ./internal/llm/agent ./internal/api ./internal/tui/styles ./internal/config` — all pass.
- New tests in `icons_test.go`: `TestSetNerdFontsSwapsIconSet`,
  `TestFileIconForFallsBackWithoutNerdFonts`.
- All 7 locale JSON files validated.

## Key insight (do not regress)
Do NOT attempt to embed a font with `go:embed` to "fix" missing icons in the
TUI — the terminal owns font rendering. The only correct lever is the
codepoints pando emits, hence this fallback set. (go:embed + @font-face only
works in the GUI/Wails desktop, where the process rasterizes the font itself.)
