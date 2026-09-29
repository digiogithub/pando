---
created_at: 2026-09-30T12:08:05.11681582Z
updated_at: 2026-09-30T12:08:05.11681582Z
tags:
    - fix
    - webui
    - config
    - persistence
    - persona
    - lsp
    - i18n
---
# Fix: WebUI settings persistence (persona, coder autoCompact, LSP autostart, UI language) — 2026-09-30

Analysis: [[webui-settings-persistence-bugs-2026-09-30]]. Related: [[evaluator-config-json-keys]].

User rule for persona + autoCompact: the effective value is the project `.pando.toml` value if set, else the global value; changes persist per project (through `updateCfgFile`, which writes the project file when it exists).

## Persona
- New `internal/config/persona_active.go`: `PersonaConfig{Active}` (`[Persona] Active`, json `persona.active`), sentinel `"auto"` = explicit Auto, empty = never chosen; `ActivePersonaChoice`, `EffectiveActivePersona` (fallback `assistant`), `UpdateActivePersona` (rolls back on write error).
- `Config.Persona` field added in config.go.
- `agent.SetAndPersistActivePersona` (persona_selector.go) used by the TUI (`tui.go`), ACP adapters (`cmd/root.go`, `internal/app/app.go`) and the API PUT handler (`handlers_personas.go`, 500 on persist failure).
- Startup (`app.go`) restores the effective persona; if it is unknown, logs a warning and falls back to assistant.
- `PersonaSelector.tsx` shows an error toast when the PUT fails.

## Coder autoCompact
- `Agent.AutoCompact` is now `*bool` (nil = inherit global). `config.ResolveAutoCompact(global, agent)`: the per-agent value OVERRIDES the global one (was OR).
- `setAgentModel` and `agentInheritingModel` copy the existing Agent and change only Model (they used to drop AutoCompact/Threshold/ThinkingMode/ContextWindowOverride on every model switch).
- Runtime: `agent.go` shouldCompact and the `tui.go` 95% trigger use the resolver. TUI settings shows the resolved value.
- API `handlers_config.go`: GET returns the effective value; PUT stores an explicit per-agent value only when it differs from the effective one.
- Init templates (`internal/config/init.go`, `cmd/init.go`) no longer write `AutoCompact = false` for coder.
- CAVEAT: existing TOMLs with `AutoCompact = false` per agent (written by the old bug, e.g. this repo's .pando.toml) are now explicit overrides that beat global true. Delete those lines to inherit.

## LSP
- `UpdateLSP` now copies Autostart, Languages and Filenames (clones the slices).
- `handlePutConfigLSP` preserves the stored Options (the UI never sends them).

## UI language
- `UIPrefs.Language` (`json:"language"`) in `handlers_ui_prefs.go` -> `~/.config/pando/webui-prefs.json` (global per machine); empty is ignored, >16 chars returns 400.
- settingsStore: `setLanguage` (applies immediately, not dirty, localStorage `pando_language` cache, fire-and-forget PUT `/api/v1/ui/preferences`), `hydrateLanguage` with a `languageChosen` guard (called in MainLayout after hydrateChatMode); `fetchSettings` preserves language; `saveSettings` no longer sends it.
- `GeneralSettings.tsx` uses setLanguage; `i18n/index.ts` reads the cached value at init.

## Tests / verification
New tests: `internal/config/agent_persistence_test.go`, `internal/config/persona_active_test.go`, `internal/api/handlers_config_lsp_persist_test.go`, `internal/api/handlers_personas_persist_test.go`, `TestUIPrefsLanguageRoundTrip`.
`go build ./...`, `go vet`, `go test ./internal/config ./internal/api ./internal/llm/agent ./internal/app ./internal/tui/...` all ok; `web-ui npx tsc --noEmit` ok. Not run: manual desktop check, `npm run build`.
