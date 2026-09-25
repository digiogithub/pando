---
created_at: 2026-09-25T08:25:59.649707923Z
updated_at: 2026-09-25T08:25:59.649707923Z
tags:
    - fix
    - feature
    - tui
    - brand
    - theme
---
# TUI brand identity: pando theme palette + Remembrances/Mesnada glyphs (PANDO-US-0063)

## What changed

Implemented story PANDO-US-0063 (epic PANDO-EP-0011, "B5 TUI brand") for the
Bubble Tea TUI, area `internal/tui/**` only.

### 1. `internal/tui/theme/opencode.go` — `PandoTheme` / `NewPandoTheme()`
Re-derived the "pando" theme's dark/light palette from the brand colors in
`assets/pando-brand-v1/README.md`:
- Bosque `#0F2A20` (brand bg/ink), Marfil `#F4F1E8` (stroke on dark),
  Álamo `#E9B949` (accent on dark), Álamo oscuro `#C68A17` (accent on light).
- Dark mode: `Background`/`BackgroundSecondary`/`BackgroundDarker` are Bosque
  and two lighter/darker Bosque tints (`#0F2A20`/`#16352A`/`#0A1D16`);
  `Text`=Marfil `#F4F1E8`; `Primary`/`Accent`/emphasized text use Álamo and two
  tints (`#E9B949`/`#F0C868`/`#D9A73E`); `Secondary` and borders use a
  Bosque-family green (`#5EAE86` / `#2E5A46`).
- Light mode: background is a near-white Marfil tint (`#FAF8F3`), text is a
  Bosque-tinted near-black (`#1C2B23`); `Primary`/`Accent`/emphasized text use
  Álamo oscuro **darkened** (`#A8730E` / `#8F6210` / `#B37D12`) because the
  literal `#C68A17` only reaches ~2.8:1 contrast against the light background
  (fails WCAG AA even for large text) — the story brief explicitly allowed
  "or darker for readability"; `Secondary`/borders use a deep Bosque green
  (`#1F6B4A` / `#D9D3C4` muted-marfil for BorderNormal).
- Status colors (Error/Warning/Success/Info) were left as generic semantic
  hues — the brief only asked for accent/text/background/border families to
  follow the brand, and shifting these would not add brand signal.
- `HasBackground()` stays `true` (unchanged, inherited from `BaseTheme`) since
  this theme already painted its own background before this change.
- Contrast was checked by hand (relative luminance / WCAG contrast ratio) for
  the new Primary/Secondary/TextMuted pairs in both modes; all land at ≥3.4:1
  (large/bold text) to ~8.4:1 (dark-mode Álamo-on-Bosque, matching the brand's
  own signature combo).

### 2. `internal/tui/styles/icons.go` — new glyphs
Added `Remembrances`/`Mesnada` fields to `iconSet`, `RemembrancesIcon`
("本") and `MesnadaIcon` ("众") to **both** the Nerd Font set and the
ASCII/fallback set (both are plain CJK Unicode, not Nerd Font Private Use
Area codepoints, so they render identically and safely in the fallback too).
Wired through `applyIconSet` like the existing `PandoIcon`.

### 3. `internal/tui/components/settings/settings.go` — brand-glyph-aware section titles
Added `brandSectionGlyph(title string) (glyph, rest string, ok bool)`: detects
a leading `RemembrancesIcon`/`MesnadaIcon` + space and splits it off. Used in
both `renderSidebar()` (section list item) and `renderContent()` (active
section header) to render the glyph in `t.Accent()` and the rest of the title
in the existing color (Text/Primary depending on active state), instead of
painting the whole "glyph + title" string one color. Falls back to the
original single-style render for every other (non-brand) section, so none of
the ~20 other settings sections changed behavior. Concatenation pattern
(`styleA.Render(x) + " " + styleB.Render(y)`, then an outer Width/Padding-only
wrapper) mirrors existing precedent in this codebase
(`internal/tui/components/dialog/cronjobs.go:206`,
`internal/tui/components/evaluator/metrics.go`).

### 4. `internal/tui/page/settings.go` — section titles
- `buildMesnadaSection`: `Title: styles.MesnadaIcon + " Subagents"` (was
  `"Subagents"`).
- `buildRemembrancesSection` (both return points — the early-disabled-return
  and the normal end): `Title: styles.RemembrancesIcon + " KB & Code Index"`
  (was `"KB & Code Index"`).
- `Section.Title` is only ever compared/used in-memory at runtime (active
  section index tracking, `SetActiveField`/`SaveFieldMsg.SectionTitle`
  matching) — never persisted to disk/config — so changing the literal string
  is safe; verified via `grep` across the repo and the test suite.

### 5. `internal/tui/page/orchestrator.go` — Mesnada glyph in the dashboard header
`View()`'s `headerText` ("Orchestrator Dashboard (N tasks)…") is now prefixed
with `styles.MesnadaIcon` rendered in `t.Accent()`, followed by the title text
in `t.Primary()` — same "glyph + space + title, glyph in accent color" pattern
as the settings sections, keeping it subtle.

### 6. `internal/tui/page/maintabs.go` — busy-logo animation
Removed `"本"` from `logoAnimFrames` (was `{"本","枝","葉","林","森"}`, now
`{"枝","葉","林","森"}`) since 本 now identifies the Remembrances section and
would be ambiguous inside the generic busy-animation. Kept 枝葉林森 (branch →
leaf → grove → dense forest) as the "growth of a colony" animation. Updated
the doc comment accordingly. `logoGlyphWidth`, `AdvanceLogo`, `logoGlyph()`
and `maintabs_test.go` are all frame-count-agnostic (`len(logoAnimFrames)`),
so no other code or test needed changes — the existing test suite passed
unmodified.

Window title "木 Pando" (`PandoIcon`) was left untouched per the story.

## Reason / motivation
Brand identity rollout (epic PANDO-EP-0011): give the TUI the same Bosque/
Marfil/Álamo palette and 木/本/众 section-mark system already going into the
WebUI and Desktop (see `assets/pando-brand-v1/README.md` and the shared
brief). 本 (Remembrances) needed to be freed from the busy-logo animation
once it became a dedicated section glyph, to avoid ambiguity.

## Verification
- `go build ./...` — clean, no errors.
- `go test ./internal/tui/...` — all packages pass (`tui`, `page`,
  `styles`, `theme`, `components/settings`, `components/chat`,
  `components/dialog`, `components/core`, `components/filetree`,
  `components/snapshots`, `util`; several packages have no test files).
  `maintabs_test.go` (logo animation) passed unmodified since it derives
  everything from `len(logoAnimFrames)`.
- `gofmt -l` clean on every touched file.
- `go vet ./internal/tui/...` clean.
- Manual visual check: a throwaway test (created under
  `internal/tui/components/settings/`, run, then deleted — not committed)
  printed the resolved hex values for the new "pando" theme (confirmed
  `Primary=#E9B949 Accent=#F0C868 Secondary=#5EAE86
  Background=#0F2A20 Text=#F4F1E8 TextMuted=#B9B6A9
  BorderNormal=#2E5A46 BorderFocused=#E9B949` in dark mode) and exercised
  `brandSectionGlyph` on both new section titles, confirming the glyph/rest
  split works (`本`/`KB & Code Index`, `众`/`Subagents`). Actual ANSI color
  bytes aren't visible through `go test`'s non-tty stdout (lipgloss disables
  color there), so this is a logic/value check, not a rendered-pixel check —
  full color rendering can only be confirmed by running the TUI in a real
  terminal with `PANDO_THEME=pando` (or via Settings → theme picker).

## Files touched
- `internal/tui/theme/opencode.go`
- `internal/tui/styles/icons.go`
- `internal/tui/components/settings/settings.go`
- `internal/tui/page/settings.go`
- `internal/tui/page/orchestrator.go`
- `internal/tui/page/maintabs.go`

## Cross-area notes for other B-story agents (not touched here)
- Nothing outside `internal/tui/**` was touched, per the shared brief's rule 1.
- The WebUI/Desktop agents' `BrandMark` component and CSS tokens are a
  separate system; nothing here depends on or duplicates that work.
