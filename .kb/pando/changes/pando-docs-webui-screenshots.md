---
created_at: 2026-10-02T15:52:21.973113786Z
updated_at: 2026-10-02T15:52:21.973113786Z
---
# pando-docs: Web UI screenshots added to docs (2026-10-02)

## What
- 72 Web UI captures from `../pando-test` (PNG, Pando v1.2.7) converted to web JPG (quality 80, 4:4:4, progressive, stripped, 3px edge shave) into `pando-docs/static/images/webui/pando-webui-<section>.jpg` (6.5 MB total). Three 2x3 px broken PNGs skipped.
- Redacted before publishing: debug ID (general-diagnostics, general-caveman-brevity), OpenRouter key suffix (settings-providers), Google CX id (settings-tools-search), e-mail in terminal prompt (terminal).
- `layouts/_shortcodes/shot.html`: new optional `dark="..."` param, renders a second `<img class="shot__dark">`; CSS in `assets/css/pando/45-guides.css` swaps `.shot__light` / `.shot__dark` under `.dark`.
- Dark/light pairs: chat view and the four setup assistant steps. Everything else light only.
- 64 images embedded with `{{< shot >}}` in en + es: web-ui (plus new "More screens" section), setup-assistant, model-auto-mode, sandbox, remote-diagnostics, self-improvement, project-workspaces, design-studio, agent-vcs, agent-delegation, lsp-auto-activation, mcp-authentication, browser-automation, desktop-controller, caveman-mode, tool-discovery, context-enrichment, persistent-memory, ipc, configuration/{_index,remembrances,token-optimization}, guides/{dev-containers,first-skill,mcp-servers,mesnada,remembrances}. lsp-auto-activation, mcp-authentication and caveman-mode have no es page.
- Not embedded (converted only): projects (lists private project names/paths), settings-skills (internal company skill descriptions), and redundant scroll fragments (auto-mode playground-empty, routing-tuning, container-runtime-activity, general-workspaces-delegation, remembrances-select-directory, tools-search-providers).

## How
Scratchpad scripts `shots.py` (convert + redact via ImageMagick) and `place.py` (insert after a heading located by ordinal so en/es stay aligned).

## Verified
Hugo build clean with `/pando-docs/` base; link checker 17480 refs, 0 broken; redactions inspected visually. No browser check of rendered pages. Uncommitted.

Related: [[pando/changes/pando-docs-september-2026-roundup.md]], [[feature_pando_docs_redesign_beneath_surface]]
