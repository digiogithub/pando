---
created_at: 2026-09-25T08:46:29.377513024Z
updated_at: 2026-09-25T08:46:29.377513024Z
tags:
    - change
    - docs
    - brand
    - webui
---
# pando-docs content updated for Pando v1 brand (2026-09-25)

Updated the CONTENT of the Hugo docs site (`/www/MCP/Pando/pando-docs`, separate repo from `pando`) for the new v1 identity from [[brand-v1-identity]] and [[webui-native-redesign]]. Ran in parallel with another agent that handled `assets/css`, `static/`, `hugo.toml`, `layouts/` in the same repo (it finished first: `static/images/brand/*` and CSS helpers `.brand-logo-light-only`/`.brand-logo-dark-only`/`.pando-home-logo`/`.brand-family` already existed by build time).

## What changed (content/ only, mirrored en+es)

- `content/{en,es}/_index.md` — replaced the old `pando_mascot.svg` raw.githubusercontent `<img>` with two logo `<img>` tags (`pando-logo-dark.svg` class `brand-logo-light-only`, `pando-logo-light.svg` class `brand-logo-dark-only`), sized 245x64 from the real viewBox (`-40 -790 4132 1080`, ratio ≈3.826). Added an optional "One root, many trunks" / "Una raíz, muchos troncos" strip using `.brand-family` with Pando/Remembrances/Mesnada icons, linking to the new brand page.
- `content/{en,es}/docs/brand.md` — new page **Brand & Identity** / **Marca e identidad**, `weight: 6` (sits after `mcp` weight 5, confirmed last in the rendered sidebar). Covers: symbol 木 meaning, palette table with inline-HTML swatch spans, a WCAG-AA warning that Álamo oscuro `#C68A17` is for marks/nodes only — not text (fails AA on Marfil/white; WebUI uses a darkened `#8f6310` for light-mode UI accent text instead), typography (Space Grotesk 500-600 wordmark/headings, JetBrains Mono for TUI/code), the symbol family table (Pando 木 / Remembrances 本 / Mesnada 众) with light/dark mark variants via the helper classes, a download table for all 12 static assets (`/images/brand/...`), and "where the brand appears" (WebUI title bar/splash/`pando` theme family, Desktop app icon, TUI `pando` theme + 本/众 glyphs + 枝葉林森 boot animation, Mesnada UI favicon).
- `content/{en,es}/docs/_index.md` — added a card linking to the new brand page in the section cards grid.
- `content/en/blog/pando-v1-brand-identity.md` + `content/es/blog/pando-v1-identidad-de-marca.md` — new announcement post, `date: 2026-09-25`, tags `["Brand","Release","WebUI","Desktop"]` (es: `["Marca","Lanzamiento","WebUI","Escritorio"]`). Covers the symbol/family, and the WebUI theme model `family (pando/paper/slate/forest) × mode (light/dark/system) × accent (7 presets)` verified against `web-ui/src/styles/tokens.css` and [[webui-native-redesign]]. Links to the brand page.
- Grepped all of `content/` for `pando_mascot`, `pando-logo.svg`, `logo.svg`, `logo.jpg`, "Oro Pando", `#D4AF37` — the only hits were the two `_index.md` mascot `<img>` tags already fixed above; nothing else needed changing.

## Known issue found (NOT fixed — out of scope, owned by the other agent)

`assets/css/custom.css` (written by the parallel agent) has the light/dark visibility of `.brand-logo-light-only` / `.brand-logo-dark-only` **inverted**: `.brand-logo-light-only { display:none }` + `.dark .brand-logo-light-only { display:inline-block }` means the "light-only" mark is hidden in light mode and shown in dark mode (and symmetrically wrong for `-dark-only`). Confirmed hextra sets `<html class="dark">`/`class="light"` explicitly (`_vendor/.../hextra/assets/js/head/theme.js`). My content follows the task's specified class usage exactly (`pando-logo-dark.svg` → `brand-logo-light-only`, `pando-logo-light.svg` → `brand-logo-dark-only`), so once that CSS is corrected (swap which block gets the `.dark` prefix) the homepage/brand page will show the correct logo per theme.

## Verification

`hugo -d <scratchpad>/docs-build-content` from `/www/MCP/Pando/pando-docs` — 139 EN / 137 ES pages, 0 build errors (only pre-existing `.Site.Data`/`.Site.Languages`/`.Site.Sites` deprecation warnings, unrelated). Confirmed in the rendered HTML: all 12 `/images/brand/*` files resolve (the other agent's static assets were already in place), `/docs/brand/` sidebar position is last (after `/docs/mcp/`), and the ES heading anchor `#dónde-aparece-la-marca-en-pando` matches the goldmark-generated id (percent-encoded in the href, which browsers resolve fine against the raw-unicode id).

No `jj`/`git` commit was made (repo uses jj VCS; commits are the user's call).
