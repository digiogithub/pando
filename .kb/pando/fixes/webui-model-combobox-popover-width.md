---
created_at: 2026-09-25T12:10:05.028905508Z
updated_at: 2026-09-25T12:10:05.028905508Z
---
# Fix: WebUI model selector dropdown clipped (Settings > Agents)

Date: 2026-09-25

## Symptom
In Settings > Agents, opening the model selector of any agent showed a dropdown whose border did not span the trigger width; model ids and badges spilled outside the bordered box.

## Cause
`ModelCombobox` sets the inner `.model-combo-panel` width to the trigger width (~620px), but the shared `Popover` wrapper (`.ui-popover` in `web-ui/src/styles/ui.css`) has `max-width: min(360px, calc(100vw - 16px))`. The popover (border/background) was capped at 360px while the panel content overflowed it. Regression from the EP-0010 ui primitives migration ([[feature_webui_native_redesign]]).

## Change
- `web-ui/src/components/shared/ModelCombobox.tsx`: pass `className="model-combo-popover"` to `Popover`; panel width = trigger width - 2 (popover 1px border each side) so edges align.
- `web-ui/src/styles/shared.css`: `.ui-popover.model-combo-popover { max-width: calc(100vw - 16px); }` and `.model-combo-panel { max-width: 100%; }` (still clamps to viewport on narrow screens).

## Verification
- Reproduced on running server (https://localhost:8765) via headless Chrome/CDP: popover 360px vs panel 622px.
- Injected the CSS live: popover width matches panel; screenshot confirmed borders span full width.
- `npx tsc -b` clean. Needs WebUI rebuild (`build:embedded`) + server restart to ship.
