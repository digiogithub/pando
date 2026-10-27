---
tags:
    - feature
    - desktop
    - mygo
    - native-ui
    - setup
---
# Native desktop: setup wizard (area `setup`)

Date: 2026-10-27. Part of [[native_desktop_mygo]] P3-ops. Uncommitted in jj workspace `/www/MCP/Pando/pando-native-desktop`.

## What changed
- `internal/nativeui/client/setup.go` (+ `setup_test.go`): typed wrappers for `/api/v1/setup/{status,scope,suggested-models,models,complete,remembrances,ollama/*,jobs/{id}}`, provider accounts (`/config/provider-accounts`) and the Copilot device flow (`/auth/providers/copilot/{status,login}`). DTOs mirror `config.SetupStatus` and `ollamasetup`.
- `internal/nativeui/app/startup.go`: new additive mechanism `app.OnStart(func(*Context))`; hooks run once, on the UI thread, in the first frame of `Root.View` (one line in `layout.go` + a `started` field). Any area can use it.
- `internal/nativeui/app/setup/` (`setup.go` state/actions, `view.go` rendering, `setup_test.go`): hidden area `setup` at `/setup`. The OnStart hook fetches `/setup/status` (retry/backoff ~30 s like the web) and, when `needed`, pushes `/setup` (previous path is restored on finish/cancel). Cancelling marks the wizard cancelled for the session (web `cancelled`). Child instances (project workspaces) get their own Context and thus no hook-driven redirect unless the host calls it.
- Steps (same as web): scope (global/project, project disabled in $HOME; skipped when a local config exists) -> provider (existing accounts: keep / add another; 9 guided providers; API key (password input) / base URL; Copilot device flow with code, copy, open GitHub, polling until authenticated or expired) -> models (suggested main/fast via `suggested-models?refresh=1`, Select over `/api/v1/models` + free text, save to `/setup/models`) -> Remembrances (Ollama status, install options with confirm, start, per-model pull with progress via jobs polling, enable) -> done (summary, `/setup/complete`). Back / Skip / Cancel as in the web. All text reuses web i18n keys `setup.*`.
- `nativeui.go`: blank import of `app/setup`.

## Verification
- `go vet` + `go test -race ./internal/nativeui/app/setup ./internal/nativeui/client ./internal/nativeui/app` OK (httptest fake API: auto-open when needed, full walk scope->provider->models->remembrances->done->finish with calls asserted, no open when not needed, cancel returns to previous path).
- Real server: `TestRealServerWalk` (needs `PANDO_NATIVE_IT_URL`, `PANDO_NATIVE_IT_CA`, optional `PANDO_NATIVE_IT_SHOTS`) against `pando serve` with a fresh isolated HOME; passes, `/setup/complete` verified. Screenshots (ui.Tester renderer) in the session scratchpad `screens/setup-{1-scope,2-provider-existing,2-provider,3-provider-ollama,4-models,5-remembrances,6-done}.png`.
- Client methods carry a `Setup` prefix (SetupProviderAccounts, SetupCopilotLogin, ...) to avoid collisions with other areas' client files.

## Gaps
Model pickers are a Select + text input, not the web combobox with search/badges. The setup page is a routed page (not a modal dialog over the app). Copilot code opens GitHub through `mygo.Shell.OpenExternal`.
