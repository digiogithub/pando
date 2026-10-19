---
created_at: 2026-10-16T11:36:15.371277698Z
updated_at: 2026-10-16T11:36:15.371277698Z
tags:
    - changes
    - pando-docs
    - hugo
    - footer
---
# pando-docs footer: Digio + AlchemAI Project credit (2026-10-16)

Repo: `/www/MCP/Pando/pando-docs` (Hugo + hextra). Builds on [[pando-docs-redesign-beneath-the-surface]]; conventions in [[hugo-cms-for-pando-docs]].

## What changed
- `layouts/_partials/footer.html`: `.pd-footer__bottom` left span now `T "footerMadeBy"` with template data `Digio` (link https://digio.es) and `AlchemAI` (`<strong>`), piped through `safeHTML`. Right span `T "footerLicense"` · 万物流転. Copyright line removed.
- `i18n/en.yaml` / `i18n/es.yaml`: removed `footerCopyright`, `footerMadeIn`; added `footerMadeBy`, `footerAlchemAI`, `footerLicense`.
  - en: "A project by Digio, part of the AlchemAI Project." / "MIT License"
  - es: "Un proyecto de Digio, parte del Proyecto AlchemAI." / "Licencia MIT"
- `assets/css/pando/20-chrome.css`: `.pd-footer__credit a, strong` use `--ink`, weight 600.

## Why
User asked footer to state made by Digio and part of AlchemAI Project, like https://bitacora.digio.es/ (“A project by Digio, part of Proyecto AlchemAI”) but without copyright.

## Verification
`hugo` build OK; rendered `/index.html` and `/es/index.html` footer checked; CSS rule present in bundled `pando.min.*.css`. Committed f9f703bf on `main`, pushed to origin (madeindigio/pando-docs) 2026-10-16.
