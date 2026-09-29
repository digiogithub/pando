---
created_at: 2026-09-30T11:59:14.006242063Z
updated_at: 2026-09-30T11:59:14.006242063Z
tags:
    - analysis
    - webui
    - config
    - persistence
    - pando
---
# Analysis: WebUI settings that do not persist (2026-09-30)

Related: [[evaluator-config-json-keys]], [[feature_webui_simple_chat_shell]], [[lsp_ondemand_runtime_and_activation_triggers]]

Four reported bugs, analysed by parallel subagents; key lines spot-checked. NOT FIXED YET.

## 1. Persona selector lost after restarting pando desktop (backend)
- `PersonaSelector.tsx` -> PUT `/api/v1/personas/active` -> `handleSetActivePersona` (`internal/api/handlers_personas.go`) -> `agent.SetActivePersona` only sets the in-memory global `activePersonaName` (`internal/llm/agent/persona_selector.go:33`). Nothing is written to disk.
- On startup `internal/app/app.go:857-860` resets it to "assistant" (and "Auto"/empty also becomes "assistant").
- Fix: persist in `webui-prefs.json` (UIPrefs `activePersona *string`, the chatMode pattern in `handlers_ui_prefs.go`) or in config with json+toml tags; restore it in app.go before the "assistant" default; TUI/ACP setters too. Decide global vs per-project.

## 2. Coder autoCompact reverts (backend)
- WebUI PUT `/api/v1/config/agents` + `UpdateAgent` work (round-trip verified).
- `setAgentModel` (`internal/config/config.go:~4650`) rebuilds `Agent{Model, MaxTokens, ReasoningEffort}`, dropping AutoCompact, AutoCompactThreshold, ThinkingMode, ContextWindowOverride, and writes it to the TOML. `agentInheritingModel` (`config.go:3961`) does the same for propagated agents. Any model change (chat picker, /model, setup, TUI) wipes the toggle.
- Fix: copy the existing agent and set Model only; add a regression test.
- Side issue: the runtime (`agent.go:~2335`) ORs global `AutoCompact` with the per-agent flag, so turning coder off does nothing while the global is true.

## 3. LSP Autostart not saved (backend)
- `LSPSettings.tsx` modal -> PUT `/api/v1/config/lsp` -> `handlePutConfigLSP` copies the fields correctly, but `UpdateLSP` (`config.go:5538`) builds `LSPConfig{Disabled, Command, Args, Options}`, dropping Autostart, Languages and Filenames. The TUI has the same bug.
- Fix: copy those fields; preserve the old Options when the request sends none; add a test.

## 4. Language reverts even when switching settings sections (frontend + backend gap)
- UI-only i18n language. `settingsStore.ts` comment: "UI-only, not persisted". The backend `SettingsResponse`/`SettingsUpdateRequest` have no language field.
- `GeneralSettings.tsx:41-43` runs `fetchSettings()` on mount, which merges `{...DEFAULTS, ...data}` and resets it to 'en'; `useLanguageSync` then applies 'en'. `i18n/index.ts:34` hardcodes `lng: 'en'`.
- Fix: a dedicated `setLanguage` that applies immediately, with a server-side UI pref (`/api/v1/ui/preferences`, like chatMode) and a localStorage cache; `fetchSettings` must not overwrite it; i18n init reads the saved value.

## Common pattern
Three are Go functions that rebuild structs field by field and silently drop newer fields (UpdateLSP, setAgentModel, agentInheritingModel). Prefer copying the existing struct and mutating it. UI state that must persist in desktop must live server-side (the random port breaks localStorage).
