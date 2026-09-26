---
created_at: 2026-09-26T23:00:17.637799758Z
updated_at: 2026-09-26T23:00:17.637799758Z
tags:
    - fix
    - pando-docs
    - hugo
    - hextra
    - brand
---
# Fix: brand logos missing on the public pando-docs site (2026-09-27)

Follow-up to [[pando-docs-brand-v1]].

## Symptom
The new logos and marks did not show on https://madeindigio.github.io/pando-docs/, although they showed in local `hugo server`.

## Cause
- GitHub Pages serves the site under the `/pando-docs/` subpath. `.github/workflows/pages.yaml` builds with `--baseURL "${{ steps.pages.outputs.base_url }}/"`.
- The brand content (home `_index.md` en/es, `docs/brand.md` en/es) used raw HTML such as `<img src="/images/brand/...">` and `<a href="/docs/brand">`.
- Raw HTML skips hextra's `render-link` / `render-image` hooks. Those hooks are what prefix root-relative Markdown links with the base path, so these paths resolved to the domain root and returned 404.
- The assets were published correctly: `/pando-docs/images/brand/*` returned 200.
- Markdown links like `[x](/docs/...)` were never affected.

## Fix
- New shortcode `layouts/_shortcodes/asset-url.html`: `{{ .Get 0 | strings.TrimPrefix "/" | relURL }}`.
- Raw HTML now uses `src="{{< asset-url "images/brand/x.svg" >}}"` for assets and `href="{{< relref "/docs/brand" >}}"` for page links. `relref` is language-aware.

## Rule for future content
Never write root-relative paths inside raw HTML in pando-docs content. Use `asset-url` for static files and `relref` for pages. Markdown links and images are fine as-is.

## Verification
- `hugo --baseURL https://madeindigio.github.io/pando-docs/`: every brand `src`/`href` is prefixed with `/pando-docs/` and no root-relative raw paths remain.
- The local build without a subpath still renders `/images/brand/...`.
- Pushed as `bcabeccd` on main, which triggered the "Deploy to GitHub Pages" workflow.
