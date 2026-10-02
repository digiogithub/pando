---
created_at: 2026-10-02T14:57:41.644149682Z
updated_at: 2026-10-02T14:57:41.644149682Z
tags:
    - change
    - docs
    - pando-docs
    - blog
---
# Change: pando-docs September 2026 documentation update and blog roundup

Date: 2026-10-02. Repo: `../pando-docs`. Follows [[pando-docs-redesign-beneath-the-surface]] and [[installer-unified-install-sh]].

## What changed
Reviewed the 99 commits of September 2026 in `pando` (tags v0.700.1 to v1.2.6) and brought the docs site up to date, in English and Spanish, in end-user language.

- Redesign commit: `feat(design): Beneath the Surface redesign, guides section, release-first install` (jj change xwrpqnzp, not pushed). The `pando` repo was already clean.
- New feature pages (en + es) under `content/<lang>/docs/features/`: `sandbox.md`, `model-auto-mode.md`, `setup-assistant.md`, `remote-diagnostics.md`, `agui.md`.
- Rewritten: `self-update.md` (`pando update [version]`, where the update notice shows), `self-improvement.md` (EP-0014 design: scoring, `/feedback`, judge budget, reviewable learned skills, `pando evaluator doctor`, prompt variants).
- Extended: `desktop-app.md` (own title bar, tray, project windows, home-folder start, Linux missing libraries), `web-ui.md` (appearance/themes, simple chat, version display, unsaved-changes guard, setup assistant, Auto), `browser-automation.md` (Obscura), `extensions.md` (managed config, identity, UI policy, events), `features/_index.md` (Antigravity removed, sandbox, auto mode), `mcp/_index.md` (bearer token on HTTP transport).
- `data/features.yaml`: five new slugs (setup-assistant, model-auto-mode, agui, sandbox, remote-diagnostics), so home chips and counts update.
- Blog: `content/en/blog/pando-september-2026-roundup.md`, `content/es/blog/pando-septiembre-2026-resumen.md` (date 2026-09-30).

## Sources
`docs/sandbox.md`, `docs/telemetry.md`, `docs/model-auto-mode.md`, `docs/agui.md` in the pando repo, KB feature docs ([[webui-first-run-setup-assistant]], [[webui-native-redesign]], [[desktop_frameless_titlebar_tray]], [[update-specific-release-version]], [[self-improvement-system-analysis]]), CLI help of the v1.2.7 binary, and WebUI `es.json` for Spanish labels.

## Scope decisions
- Items committed on 2026-10-01/02 (project workspace tabs, persona decision model, unified install script, live resumed runs) are left out of the September post and only named under "What's next".
- Spanish UI labels follow the app: "Auto mode", "Self-Improvement", "Arrancar Ollama", "Ajustes > General > Diagnóstico".

## Verification
`hugo --baseURL https://example.org/pando-docs/` builds with no errors or warnings; link check over the output shows no broken link from the new or changed pages (remaining ones are older: `/docs/sdk/`). Not checked visually in a browser. Not committed.
