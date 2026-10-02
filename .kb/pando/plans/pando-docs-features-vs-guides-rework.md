---
created_at: 2026-10-02T17:53:57.683567803Z
updated_at: 2026-10-02T17:53:57.683567803Z
---
# Plan: pando-docs rework, features explain / guides teach / reference lists (2026-10-02)

Follows [[pando/changes/pando-docs-webui-screenshots.md]] and [[pando-docs-redesign-beneath-the-surface]]. Repo `../pando-docs`.

## User request
Analyse Pando from the "how do I use it" angle, then separate in the docs site the explanation of each feature from the instructions to configure and use it. Explanations stay in Features; how-to goes to Guides and documentation, always based on the Web UI (72 user captures + new ones taken with Playwright). Language must be plain, playful, with easy similes instead of technical jargon. English and Spanish.

## Analysis
- Feature pages (46 per language) mixed three things: explanation, step-by-step setup, and config/CLI reference.
- Guides section had 9 pages, 8 of them `planned: true` stubs.
- Reference lived partly in `docs/configuration/*` and partly inside feature pages.

## Target structure
- **Features** `docs/features/<slug>.md`: what it is, what it does for you, how it feels, when to use it, good to know, next steps (links to guide + reference). 40-70 lines, no config blocks.
- **Guides** `guides/<slug>.md`: one `##` per step in the Web UI with screenshots, ending with "Check it works", "If something goes wrong", optional "Prefer the terminal?".
- **Reference** `docs/configuration/<topic>.md`: keys, defaults, CLI, env vars.
- Nothing lost: every key/command moves to a guide or a reference page.

## Guides (24): slug · track · weight
Surface: install 1, first-session 2, setup-providers-models 3 (new), choose-your-surface 4, webui-tour 5 (new), projects-workspaces 6 (new), remote-access 7 (new), design-studio 8 (new).
Roots: remembrances 10, mesnada 11, goal-mode 12, model-auto-mode 13 (new), working-modes 14 (new), self-improvement 15 (new), review-and-undo 16 (new).
Soil: mcp-servers 20, web-browser-desktop-tools 21 (new), language-servers 22 (new), editors-and-other-apps 23 (new), first-skill 24, dev-containers 25, sandbox-and-permissions 26 (new), save-tokens 27 (new), update-and-diagnostics 28 (new).

## New reference pages
webui, auto-mode, delegation, self-improvement, modes, tools, lsp, mcp, providers, sandbox, containers, skills-and-extensions, diagnostics (plus existing remembrances, goal, token-optimization, acp-advanced, age-encryption, security-age, _index as hub).

## Execution
Four parallel workers (A surface, B roots + modes, C soil models/protocols/hands, D soil extend/trust/operations), each owning a disjoint file set, all following `scratchpad/DOCS_REWORK_BRIEF.md`. Coordinator owns `data/features.yaml`, `features/_index.md`, `guides/_index.md`, `docs/_index.md`, sidebar, i18n; then builds, link-checks and reviews.

## Extra captures taken (Playwright against `pando app` in ../pando-test)
logs, self-improvement, design-artifacts, chat-slash-commands, persona-selector, model-selector, settings-snapshots, settings-api-server, settings-webui-access.
