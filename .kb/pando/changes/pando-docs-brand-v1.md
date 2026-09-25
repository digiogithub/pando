---
created_at: 2026-09-25T08:48:41.382990944Z
updated_at: 2026-09-25T08:48:41.382990944Z
tags:
    - changes
    - documentation
    - hugo
    - pando-docs
    - brand
---
# Pando v1 brand on pando-docs: overview + lead review fixes (2026-09-25)

Extends epic [[brand-v1-identity]] (PANDO-EP-0011) to the Hugo + hextra docs site `../pando-docs`. Two parallel subagents did the work:
- Theme, assets and build plumbing: [[pando-docs-brand-v1-styleguide]] (custom.css palette rewrite, Space Grotesk/JetBrains Mono via `layouts/_partials/custom/head-end.html`, favicons, `site.webmanifest`, navbar logo light/dark, `static/images/brand/*`, old mascot/logo deleted).
- Content in en and es: [[docs-site-brand-v1-content]] (home logo swap + brand-family strip, new `docs/brand.md` page, blog post `pando-v1-brand-identity` / `pando-v1-identidad-de-marca`, docs index card).

## Lead review fixes (after the subagents finished)
- `assets/css/custom.css`: `.brand-logo-light-only` / `.brand-logo-dark-only` visibility was inverted (hextra sets `.dark` on `<html>`), so the swap was wrong. Fixed.
- `.brand-family > div` changed to `> figure` with `margin: 0` to match the markup.
- Removed the drop-shadow halo on `.pando-home-logo` in light mode (it looked like a smudge). Kept the gold glow in dark mode.
- Added a dark-mode search input (bg `#16352a`, Marfil text). It was white in dark mode.
- `content/{en,es}/_index.md`: the homepage used the legacy hextra `hx-*` class syntax. The vendored hextra needs `hx:*`, so the hero margins, flex layout and badge dot were not applied. This was a pre-existing bug. All classes converted (`sm:hx-block` became `hx:sm:block`), and the brand-page link styled with `hx:text-primary-600 hx:underline`.

## Gotcha
Raw HTML in hextra content must use the `hx:` class prefix. Classes written as `hx-` are silently ignored.

## Verification
- `hugo --gc --minify` into the scratchpad: 139 EN + 137 ES pages, 0 errors.
- Headless Chrome screenshots checked in light and dark: home (en/es), `/docs/brand/` and the navbar.
- No jj commit made.
