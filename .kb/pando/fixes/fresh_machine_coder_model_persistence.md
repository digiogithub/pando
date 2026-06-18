---
created_at: 2026-06-18T12:49:27.612999255Z
updated_at: 2026-06-18T12:49:27.612999255Z
tags:
    - fix
    - config
    - models
    - webui
    - copilot
    - agent
---
# Fix: coder model not selectable/persisting on a fresh machine (web-UI/app/desktop)

Date: 2026-06-18

## Symptom
On a freshly configured machine, after adding a provider account (e.g. Copilot) via
the web-UI/app/desktop, the default coder agent model would not persist and selecting
a model from the model switcher failed with "no model configured, please select a
model". Workaround was to open the TUI, pick a coder model (which persisted), then
restart in web-UI/app/desktop.

## Root causes (two bugs)

### Bug A — dynamic models not refreshed after adding an account
`app.RefreshDynamicModels` was only called at startup and every 24h. The web-UI
account handlers (`handleCreateProviderAccount`, `handleUpdateProviderAccount`),
the Antigravity OAuth callback, and the Copilot device-login completion never
triggered it. So a newly added account's dynamic models (e.g. `copilot.gpt-5.4`)
were not registered in `models.SupportedModels`, and both the model switcher and
`validateAgent` rejected them ("model not supported"). The TUI "fixed" it only
because restarting the process re-ran the startup refresh against the now-persisted
account and saved the model cache.

### Bug B — running agent provider not rebuilt on web-UI selection
The TUI selects models via `CoderAgent.Update`, which both persists config AND
rebuilds the in-memory provider. The web-UI/desktop paths called only
`config.UpdateAgentModel`, which writes the config file but leaves the already-running
`CoderAgent.provider` untouched (nil when no model was set at startup). `agent.Run`
returns `ErrNoModel` when `provider == nil`, producing "no model configured, please
select a model" even though the model looked selected.

## Changes (internal/api)
- `handlers_models.go`: new `(*Server).setCoderModel` helper that calls
  `s.app.CoderAgent.Update` when a live agent exists (rebuilds provider + persists),
  falling back to `config.UpdateAgentModel`. `handleSetActiveModel` now uses it.
- `handlers_settings.go`: `handleSetSettings` DefaultModel update uses `setCoderModel`.
- `handlers_provider_accounts.go`: new `refreshDynamicModelsAfterAccountChange()`
  (bounded 30s ctx, calls `app.RefreshDynamicModels`) invoked after create/update.
- `handlers_antigravity_oauth.go`: refresh after OAuth callback persists credentials.
- `handlers_auth.go`: Copilot device-flow completion goroutine refreshes models +
  publishes account-changed after the token is stored.

## Tests
`internal/api/handlers_set_model_test.go`:
- `TestHandleSetActiveModelUpdatesRunningAgent` — asserts the PUT routes through
  `CoderAgent.Update`.
- `TestSetCoderModelFallsBackToConfig` — nil CoderAgent still persists via config.
