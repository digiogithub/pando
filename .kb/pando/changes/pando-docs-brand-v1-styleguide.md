---
created_at: 2026-09-25T08:45:50.490224854Z
updated_at: 2026-09-25T08:45:50.490224854Z
tags:
    - changes
    - documentation
    - hugo
    - pando-docs
    - brand
    - hextra
---
# Pando v1 brand applied to pando-docs (Hugo + hextra)

Date: 2026-09-25. Scope: `/www/MCP/Pando/pando-docs` build/theme files only
(`static/`, `assets/css/`, `layouts/`, `hugo.toml`). Sibling task, run in
parallel by another agent, rewrote `content/**` to reference the new asset
paths — this document covers only the styleguide/build-plumbing half of
epic [[brand-v1-identity]] (PANDO-EP-0011) extended to the docs site.

## Motivation

Apply the Pando v1 brand styleguide (`assets/pando-brand-v1/` in the `pando`
repo, read-only source of truth) to the public docs site, replacing the old
"Pando gold" theme (`#D4AF37` / `#FCFAF2` / `#121214` / `#CBB89B` etc.) with
the Bosque/Marfil/Álamo palette already shipped in the WebUI
(`web-ui/src/styles/tokens.css`, "pando" theme family).

## Files changed

- `hugo.toml` — `params.navbar.logo.path` = `images/brand/pando-mark-dark.svg`,
  added `params.navbar.logo.dark` = `images/brand/pando-mark-light.svg`
  (hextra's `navbar-title.html` already supports a `.logo.dark` override,
  swapped via `hx:dark:hidden` / `hx:dark:block`). `displayTitle` untouched.
- `assets/css/custom.css` — full rewrite. Old gold/black palette removed
  entirely (verified via grep on the built output: no `D4AF37`, `FCFAF2`,
  `121214`, `0A0A0C`, `CBB89B`, `E5C365`, `F5F2E8`, `EEE5D2`, `7A5A3A`
  remain). New rules:
  - `--primary-hue/-saturation/-lightness` derived from brand hex via HSL
    conversion: light mode `39deg 80% 31%` (from the WebUI's darkened
    Álamo `#8f6310`), dark mode `42deg 78% 60%` (from brand Álamo
    `#E9B949`). Hextra's `primary-500` formula reproduces the source hex
    almost exactly (`#8E6210` / `#E9B949`), and the actual link color class
    hextra applies to prose (`hx:text-primary-600`) computes even darker
    (light) / still-vivid (dark), see contrast section below.
  - Body bg light `#fdfcf8` / dark `#0c1f18`; navbar blur, sidebar, footer,
    borders all switched to Bosque-family greens (`#e9e3d2` / `#1c352b`
    border tones, `#f4f1e8` / `#0a1a14` shell tones) matching
    `tokens.css` exactly.
  - Navbar title (`.hx:font-extrabold`) recolored from gold to brand ink
    (`#10251c` light / `#f4f1e8` dark) — the actual wordmark SVGs draw the
    text in ink, not gold, only the three circuit nodes are gold.
  - `h1, h2, h3` + navbar title set to `--font-display` (Space Grotesk);
    `code, pre, kbd, samp, .hextra-code-block` set to `--font-mono-brand`
    (JetBrains Mono).
  - New helper classes for the content agent: `.brand-logo-light-only` /
    `.brand-logo-dark-only` (toggle via the `.dark` class hextra puts on
    `<html>`), `.pando-home-logo` (kept, restyled), `.brand-family` (flex
    row + caption block for a Pando/Remembrances/Mesnada mark strip).
- `layouts/_partials/custom/head-end.html` — **new file** (hextra's own
  hook at this vendored path exists but was empty — confirmed by reading
  `_vendor/.../layouts/_partials/custom/head-end.html`, and `head.html`
  calls `partial "custom/head-end.html"` unconditionally near the end of
  `<head>`). Adds Google Fonts preconnect + stylesheet link for
  `Space Grotesk:wght@500;600` + `JetBrains+Mono:wght@400;500`, plus two
  `<meta name="theme-color">` tags keyed by
  `(prefers-color-scheme: light|dark)` using each mode's real page
  background (`#fdfcf8` / `#0c1f18`, both Bosque-family) rather than a
  single flat Bosque value — call this out to the user if they wanted the
  literal `#0F2A20` in both.
- `static/images/brand/` — **new directory**, 12 files copied verbatim
  from `assets/pando-brand-v1/` (and its `remembrances/`, `mesnada/`
  subfolders) with the exact names the content agent expects:
  `pando-logo-dark.svg`, `pando-logo-light.svg`, `pando-mark-dark.svg`,
  `pando-mark-light.svg`, `pando-icon.svg`, `remembrances-mark-dark.svg`,
  `remembrances-mark-light.svg`, `remembrances-icon.svg`,
  `mesnada-mark-dark.svg`, `mesnada-mark-light.svg`, `mesnada-icon.svg`,
  `pando-icon-256.png`.
- `static/favicon.svg` — replaced with the brand's reinforced small mark
  (`assets/pando-brand-v1/favicon.svg`).
- `static/favicon.ico` — replaced with `assets/pando-brand-v1/pando.ico`.
- `static/favicon-16x16.png`, `static/favicon-32x32.png` — from
  `assets/pando-brand-v1/png/pando-icon-{16,32}.png`.
- `static/apple-touch-icon.png` — **new**, 180x180, generated with
  ImageMagick `convert` from `png/pando-icon-1024.png`.
- `static/android-chrome-192x192.png` (generated via `convert` from
  `png/pando-icon-512.png`) and `static/android-chrome-512x512.png`
  (copied as-is) — **new**, needed because `static/site.webmanifest`
  (**new file**, overriding the vendor's generic "Hextra" manifest)
  references them; without an override the vendored `site.webmanifest`
  would have served under the "Hextra" name with black theme colors.
  `theme_color`/`background_color` set to Bosque `#0F2A20` per the task's
  explicit instruction (distinct from the head meta theme-color choice
  above).
- Deleted `static/images/pando_mascot.svg`, `static/images/logo.svg`,
  `static/images/pando-logo.svg` — confirmed via grep across
  `layouts/**`, `hugo.toml`, `assets/**` (excluding `_vendor/`, `public/`,
  `resources/`) that nothing local referenced them once `hugo.toml`'s
  navbar logo path was repointed. The only remaining mentions were
  `content/en/_index.md` and `content/es/_index.md`, which load
  `pando_mascot.svg` via a `raw.githubusercontent.com` URL — out of scope
  here, owned by the sibling content-editing agent.

## Naming convention confirmed by reading the SVG fills

In `assets/pando-brand-v1/`, the `-dark` suffix means "dark ink stroke",
i.e. the variant **for a light background**; `-light` means "light ink
stroke" (Marfil), i.e. **for a dark background**. Confirmed by reading the
raw `<svg>` fill/stroke attributes:
- `pando-mark-dark.svg` / `pando-logo-dark.svg`: stroke `#0F2A20` (Bosque
  ink) + nodes `#C68A17` (Álamo oscuro) → light bg.
- `pando-mark-light.svg` / `pando-logo-light.svg`: stroke `#F4F1E8`
  (Marfil) + nodes `#E9B949` (Álamo) → dark bg.
- Same pattern holds for `remembrances-mark-{dark,light}.svg` and
  `mesnada-mark-{dark,light}.svg`.
- `*-icon.svg` files are self-contained app icons: Bosque rounded-square
  background baked in, Marfil strokes, Álamo nodes — usable on either
  background since they carry their own bg tile.

This is exactly the mapping `hugo.toml`'s navbar logo now uses: `path`
(shown in light mode, on a light navbar) = `pando-mark-dark.svg`; `dark`
(shown in dark mode, on a dark navbar) = `pando-mark-light.svg`.

## Contrast verification

Computed with the standard WCAG relative-luminance formula (Python,
`colorsys` for HSL→RGB, no external tool):

| Pair | Ratio | AA (4.5:1) normal text |
|---|---|---|
| Light `fg` `#10251c` vs `bg` `#fdfcf8` | 15.69:1 | pass |
| Light link/accent `hx:text-primary-600` `#80580E` vs `bg` `#fdfcf8` | 6.16:1 | pass |
| Light `primary-500` (≈ WebUI `#8f6310`) vs `bg` `#fdfcf8` | 5.24:1 | pass |
| Dark `fg` `#f4f1e8` vs `bg` `#0c1f18` | 15.18:1 | pass |
| Dark link/accent `hx:text-primary-600` `#E5AE2E` vs `bg` `#0c1f18` | 8.51:1 | pass |
| Dark `primary-500` (brand Álamo `#e9b949`) vs `bg` `#0c1f18` | 9.39:1 | pass |

Confirms the raw brand Álamo oscuro (`#C68A17`, not used as text here) was
correctly avoided for light-mode text/links, consistent with the WebUI's
own rule in `tokens.css`.

## Verification

- `hugo --gc --minify -d <scratchpad>/docs-build` from the pando-docs
  root — **built with no errors** (139 EN + 137 ES pages, only pre-existing
  Hugo `.Site.Data`/`.Site.Languages`/`.Site.Sites` deprecation warnings,
  unrelated to this change).
- Grepped the full build output (HTML + compiled/minified CSS) for the old
  palette hex codes (`D4AF37`, `FCFAF2`, `121214`, `0A0A0C`, `CBB89B`,
  `E5C365`, `F5F2E8`, `EEE5D2`, `7A5A3A`) — zero matches.
  `_vendor/github.com/imfing/hextra/static/images/{logo.svg,logo-dark.svg}`
  still exist in the build output under `images/` — these are the
  vendored theme's own unreferenced demo assets (module-mounted static
  dir), not ours to touch or delete, and nothing in this site points to
  them.
- Confirmed in the built `index.html`: navbar `<img>` src/dark-src point
  at `images/brand/pando-mark-{dark,light}.svg`; both
  `<meta name="theme-color">` variants present; Google Fonts
  preconnect/stylesheet present.
- Confirmed compiled CSS contains `primary-hue:39deg` / `primary-hue:42deg`
  and both `Space Grotesk` / `JetBrains Mono` font-family references.
- `static/site.webmanifest` in the build output carries `"name": "Pando"`
  and `"theme_color": "#0F2A20"`.

No `git`/`jj` commit made (out of scope for this task; repo uses jj VCS,
per project convention no `git stash` was used either).

Related: [[brand-v1-identity]] (epic PANDO-EP-0011), [[webui-native-redesign]].
