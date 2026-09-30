---
created_at: 2026-09-30T21:27:25.190705879Z
updated_at: 2026-09-30T21:27:25.190705879Z
tags:
    - fix
    - webui
    - model-routing
---
# Fix: Auto mode settings route editor layout (2026-09-30)

User feedback on [[pando/features/model-auto-mode-implementation.md]] WebUI settings.

Changes (web-ui):
- Route primary/fallback model pickers now reuse `ModelCombobox` (`web-ui/src/components/shared/ModelCombobox.tsx`, same as AgentsSettings). Added optional props `ariaLabel`, `invalid`, `excludeIds` (used to hide the synthetic `auto` entry); Agents usage unchanged. Removed the plain `<select>` helper and panel `/models` fetch.
- New `RouteModelFields` stacks primary, fallback 1 and fallback 2 vertically; fallbacks appear via "Add fallback" [+] button (max 2, hidden at 2), each with × remove (fallback 2 shifts up); empty entries never saved.
- Ollama pull suggestions (tev1:0.8b, tev1, nimble) rendered as a vertical list, one row each (name, Pull button, progress/error).
- Files: ModelAutoModeSettings.tsx, ModelAutoModeSettings.test.tsx (+3 cases), ModelCombobox.tsx, e2e/model-auto-mode.spec.ts.

Verification: bun typecheck, vitest (14 pass), build, eslint (0 errors); `make build` refreshed embedded UI in ./pando. Playwright not run.
