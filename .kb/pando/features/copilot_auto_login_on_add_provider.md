---
created_at: 2026-06-19T11:21:58.780232611Z
updated_at: 2026-06-19T11:21:58.780232611Z
tags:
    - feature
    - copilot
    - auth
    - tui
    - webui
    - providers
---
# Auto-launch Copilot login when a Copilot provider account is added

Date: 2026-06-19

## What changed
When the user adds a provider account of type `copilot` (GitHub Copilot) from either
the TUI settings or the Web UI, the GitHub Copilot OAuth **device-code login flow now
starts automatically**, showing the verification URL + user code, instead of requiring
the user to manually run the separate "Copilot Login" command afterwards.

Copilot authenticates via OAuth device flow (no API key), so adding the account alone
previously left it unauthenticated until the user found and ran the login command.

## Files / symbols touched

### TUI (Go)
- `internal/tui/components/dialog/add_provider.go`
  - New message type `StartCopilotLoginMsg struct{}` (dialog package, importable by both
    `page` and `tui`).
- `internal/tui/page/settings.go`
  - In the `dialog.ProviderAccountCreatedMsg` handler: after `config.AddProviderAccount`
    succeeds, if `msg.Account.Type == models.ProviderCopilot`, returns
    `tea.Batch(util.ReportInfo(...), util.CmdHandler(dialog.StartCopilotLoginMsg{}))`.
- `internal/tui/tui.go`
  - New case in top-level `appModel.Update`: `case dialog.StartCopilotLoginMsg:`
    returns `copilotLoginCommand()`. This reuses the existing device-code flow
    (`copilotLoginCommand` -> `copilotDeviceCodeMsg` -> persistent alert +
    `copilotPollCommand` -> `copilotLoginDoneMsg`) defined in
    `internal/tui/copilot_commands.go`.

### Web UI (TypeScript/React)
- `web-ui/src/components/settings/ProviderAccountsSettings.tsx`
  - New helper `startCopilotLogin()` that POSTs `/api/v1/auth/providers/copilot/login`,
    opens `verificationUri` in a new tab, and shows a persistent (ttl=0) info toast with
    the `userCode` — mirroring the existing `copilot:login` command in
    `web-ui/src/services/commandLauncher.ts`.
  - In `handleSave()`, after a NEW (non-edit) account is created with `form.type ===
    'copilot'`, it awaits `startCopilotLogin()`.

## Why
"cuando se añade un provider de tipo copilot, se debe de lanzar automáticamente la
rutina de login con copilot que muestra el código generado para la autenticación sin
tener que usar el comando manualmente." Improves UX so the device code appears right
after adding the account.

## Backend reuse
No new backend endpoints. Reuses:
- TUI: `auth.StartCopilotDeviceFlow` / `auth.CompleteCopilotDeviceFlow`.
- Web: existing `POST /api/v1/auth/providers/copilot/login` (`handleCopilotLoginStart`
  in `internal/api/handlers_auth.go`), which already opens the browser, returns
  `verificationUri`/`userCode`, and on completion refreshes dynamic models +
  publishes a provider-account-changed event.

## Verification
- `go build ./...` — OK.
- `go test ./internal/api` — OK.
- `npx tsc --noEmit` in `web-ui/` — OK.
- Note: the embedded web UI (`internal/api/webui/dist`) is a build artifact produced by
  `make web-ui-embedded` (`bun run build:embedded`); it is not git-tracked, so the
  change ships once the embedded build is regenerated (`make build`).
