---
created_at: 2026-10-01T09:40:58.9301045Z
updated_at: 2026-10-01T09:40:58.9301045Z
tags:
    - fix
    - webui
    - model-routing
---
# WebUI: model chip shows Auto mode and the routed model (2026-10-01)

Follow-up to [[pando/features/model-auto-mode-implementation.md]] (US-0082 claimed "chat input, status bar show `Auto · <last routed model>`", but only the ModelSwitcher did).

## Defect
With Auto selected, the model chip in the chat composer (and the status bar model button on non-chat routes) kept showing `config.default_model`. Two causes:
- both components rendered `formatModel(defaultModel)` and never read `modelAutoModeStore`;
- `autoSelected` was only loaded when the ModelSwitcher or the Auto mode settings were opened, so it was `false` after a page load.

## Fix
- New `web-ui/src/utils/modelLabel.ts`: `formatModel` (previously duplicated in ChatInput and StatusBar), `activeModelLabel(defaultModel, autoSelected, lastRoutedModel)` and the hook `useActiveModelLabel()`. Label: `Auto` until a prompt is routed, then `Auto → <model>`; no route id nor probability (those stay in the per-prompt routing notice).
- `ChatInput.tsx` and `StatusBar.tsx` use the hook.
- `modelAutoModeStore.ts`: new `hydrateAutoSelected()` (GET `/api/v1/config/model-auto-mode`, sets only `autoSelected`); called from `MainLayout.tsx` after authentication.
- `lastRoutedModel` was already updated by `routingNotice.ts` on every `system_message` routing event, so the chip follows each turn (including failover notices).

## Known limits
- `lastRoutedModel` is global and in-memory: after a reload the chip shows plain `Auto` until the next prompt, and switching session keeps the model routed in the previous one.
- TUI needed no change: `statusCmp.autoLabel()` already renders `Auto` / `Auto → <model>`.

## Verification
- `web-ui/src/utils/modelLabel.test.ts` (4 tests); vitest 18 passed; `npm run build` OK.
- Live: isolated `pando app` + Ollama `tev1:0.8b`, auto mode enabled over REST. Headless Chrome: chip reads `Auto` on load, then `Auto → ollama.tev1:0.8b` after sending a prompt (notice row: `Auto → ollama.tev1:0.8b · quick_question · p=0.81`).
