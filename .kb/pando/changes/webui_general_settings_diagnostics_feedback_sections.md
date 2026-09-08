---
created_at: 2026-09-11T09:49:51.556897897Z
updated_at: 2026-09-11T09:49:51.556897897Z
tags:
    - change
    - webui
    - settings
    - telemetry
---
# WebUI General settings: Diagnostics and Feedback Optimization sections (2026-09-11)

## What changed
`web-ui/src/components/settings/GeneralSettings.tsx`:
- **Diagnostics** (h3, key `settings.general.diagnosticsTitle`): this section now groups two controls.
  - The Debug Mode toggle, moved out of the generic toggles list.
  - The Remote telemetry toggle, together with the debug ID (copy and regenerate buttons) and the min level control.
- **Feedback Optimization** (h3, key `settings.general.feedbackOptimizationTitle`): a new section holding the caveman output brevity default select.
  - It sits between dividers.
  - Previously the caveman select sat directly under the telemetry block with no separator, so it looked like part of it.

i18n: in all 7 locales (en/es/fr/de/pt/ja/zh), the key `telemetryTitle` is renamed to `diagnosticsTitle` and `feedbackOptimizationTitle` is added. The JSON was validated after the edit.

## Why
User feedback. Remote diagnostics visually merged with the caveman field. The user wants Debug Mode and Remote diagnostics grouped as "Diagnostics", with caveman shown separately as "Feedback optimization".

## Telemetry filtering note
Better Stack records carry the ID in the top-level JSON field `debug_id`, in the dashed display format `1234-5678-9012-3456`. See [[pando/features/remote_telemetry_betterstack_summary.md]].

## Verification
`bun run typecheck`, `bun run lint` and `bun run build:embedded` pass.

Desktop and `pando serve` need a rebuilt binary (`make build`) to embed the new UI.
