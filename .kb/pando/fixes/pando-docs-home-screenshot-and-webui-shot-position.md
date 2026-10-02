---
created_at: 2026-10-02T15:58:53.885052671Z
updated_at: 2026-10-02T15:58:53.885052671Z
---
# pando-docs: home page showed a placeholder, Web UI shot was below the fold (2026-10-02)

Follow-up to [[pando/changes/pando-docs-webui-screenshots.md]]. User reported not seeing the images nor the Web UI capture on the home page.

## Cause
- Home: `layouts/_partials/home/surface.html` renders a mock placeholder unless `params.home.desktopShot` is set; it was never set.
- Docs: images did render, but on `docs/features/web-ui.md` the chat capture sat after the long feature list, far below the fold.

## Fix
- `hugo.toml`: new `[params.home]` with `desktopShot` (chat light) and `desktopShotDark` (chat dark).
- `surface.html`: renders both images with `shot-img--light` / `shot-img--dark`; `assets/css/pando/30-home.css` swaps them under `.dark`.
- `content/{en,es}/docs/features/web-ui.md`: chat shot moved right after the first paragraph.

## Verified
Headless Chrome screenshots of the local `hugo serve` (port 1313): home shows the real capture in the Native Desktop App card; Web UI page shows the capture under the intro. Dark mode swap not checked visually. Uncommitted.
