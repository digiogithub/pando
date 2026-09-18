---
id: PANDO-US-0046
type: story
title: Settings toggle in TUI and WebUI, plus the API
status: done
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:27Z
updated: 2026-09-18T11:01:13Z
started: 2026-09-18T09:23:52Z
closed: 2026-09-18T11:01:13Z
---

## Description

**As a** user **I want** to see sandbox status and turn it on, off or change its mode from the settings screens **so that** I control the trade-off without editing TOML.

### Implementation

- API: `internal/api/handlers_config.go` `handleConfigSandbox` (GET/PUT) returns `{config, capability}`. Route `/api/v1/config/sandbox` in `internal/api/routes.go` (near `:97`). It returns 403 when `sandbox.*` is a locked key.
- TUI: `internal/tui/page/settings.go` `buildSandboxSection(cfg)` in the "Tools" group, before Bash (`:1000-1002`). Fields:
  - `sandbox.enabled` (FieldToggle, `boolString(!Disabled)`)
  - `sandbox.mode` and `sandbox.network` (select)
  - `sandbox.autoAllowBash` (toggle)
  - `sandbox.writableRoots` and `sandbox.denyPaths` (text, comma list)
  - `sandbox.backend` (read-only status such as "landlock v5 + bwrap" or "unavailable: …")
  - save cases next to `case "bash.outputFilter":` (`:5925`)
- WebUI:
  - new `web-ui/src/components/settings/SandboxSettings.tsx`
  - `useSandboxStore` in `web-ui/packages/pando-client/src/stores/settingsStore.ts`
  - category entry in `SettingsView.tsx` (`:38-67`, render around `:349`)
  - i18n keys in all 7 locale files
- Remove `sandbox.*` from what `internal/llm/tools/pando_setup.go` can write.
- Status badge: TUI chat info sidebar and footer; WebUI chat info panel.

## Acceptance Criteria

- [ ] Toggling in the TUI or WebUI persists to config and changes the next command's behaviour without a restart.
- [ ] A locked key renders read-only in both UIs.
- [ ] `pando_setup` refuses sandbox keys (test).
- [ ] API handler tests exist (`internal/api`).
- [ ] The WebUI builds (`bun run build`) and mobile master-detail layout still works.

## Notes

Depends on: PANDO-US-0040 (the UI can be built against the API before PANDO-US-0041 and PANDO-US-0042 land).

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
