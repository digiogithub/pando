---
created_at: 2026-10-28T15:32:18.272797319Z
updated_at: 2026-10-28T15:32:18.272797319Z
tags:
    - fix
    - desktop
    - mygo
    - native-ui
    - settings
---
# Native desktop (MyGo) settings follow-ups after v1.5.0

Date: 2026-10-28. Uncommitted in /www/MCP/Pando/pando. Builds on [[native_desktop_settings]], [[native_desktop_integration]], plan [[native_desktop_mygo]].

## Bugs fixed
1. **Language change broke saving** ("HTTP 400: invalid telemetry_min_level ... got empty"). `Doc` KSelect values were stored as the *translated label* (`val.sel`) and mapped back with `a.T`; after `i18n.SetLanguage` the old label matched no option, so the field became "" and dirty. Fix (`form.go`): KSelect keeps the stored value in `val.s`; the label is derived per frame for `kit.Select` and mapped back immediately. `val.sel`/`selValue` removed; `Doc.Set` added.
2. **`kit.Switch(...).Changed()` never fires** (it returns the row around `ui.Switch`). Broke Mesnada engine toggles, WebUI-access enable switch and decision-model "show all". Callers now compare before/after; doc comment on `kit.Switch` warns. Pando native engines (`nativeEngines`) render a "Built-in" badge only.

## Features
- **Model autosuggest** (`settings/modelpicker.go`): `suggestField` over `ui.ComboboxBase` (free text + popup grouped by provider with name, id, meta line: ctx window, max output, $/1M, thinking, description; "provider.model" filter syntax; `auto` excluded). `Settings.catalog` loads GET /api/v1/models once. New field kind `KModel` (+ `Field.Plain`, `Field.Picked`). Used by: General default model, Agents model (+ meta line under it), Model auto mode route primary/fallbacks (`amNoModel` is now ""), Self-improvement judge model (picking sets `provider`), Mesnada default model (KModel only when default engine is Pando, `isPandoEngine`; plain text otherwise). Mesnada default-engine options also include detected/custom engines.
- **Judge prompt template**: "Show/Hide built-in default template" under the field (`pickers.go judgeTemplateField`), text from new `GET /api/v1/evaluator/judge-prompt/default` (`evaluator.DefaultJudgePrompt()`, `client.DefaultJudgePrompt`).
- **Remembrances**: KB path has Browse… (`folderControl`: native `mygo.Dialog.Open` Directory at the working dir, result stored relative via `relativeTo`, still editable as text; `DesktopSettings.WorkingDirectory` added). Embedding model fields are inline autosuggest over the provider's embedding models with refresh button + hint, provider change reloads the list, "Test connection" row inside each group; the separate "Embedding models & connection" card at the bottom was removed.
- Generic `Field.Control` (custom editor, also for KStatic action rows) and `Field.Extra` (content below a row).
- **Esc in settings** returns to /chat through the unsaved-changes guard (`Settings.backToChat`; popups/dialogs take Escape first).
- **Orchestrator detail panel** resizable: drag its left edge (`View.detailW`, clamp 320..window-360).
- **Chat sessions in the sidebar**: new `app.SidebarPanel` interface (guard.go) drawn by `Root.sidebar` below the nav items (above Settings). Chat implements it: expanded sidebar lists sessions (header + collapse chevron, search, list) and the chat drops its own column (`sessHosted`); icon rail shows a toggle that shows/hides the chat's sessions column. Header "show sessions" button and the pane's hide button removed.

## Verification
`go vet ./internal/nativeui/... ./internal/api/`; `go test ./internal/nativeui/... ./internal/api/ ./internal/evaluator/` green. New tests `settings/fixes_test.go` (language change keeps select value and PUT sends "warn"; engine switch click toggles; Mesnada model plain for external engines; model combo suggests+Enter chooses; match syntax; relativeTo; Esc -> guard -> /chat) and rewritten `chat/layout_test.go TestSessionsPaneToggle`. Real window not checked: shell has no DISPLAY/WAYLAND_DISPLAY.
