---
created_at: 2026-08-07T21:24:27.805577086Z
updated_at: 2026-08-07T21:24:27.805577086Z
tags:
    - fix
    - webui
    - desktop
    - macos
    - remembrances
    - pando_setup
    - models
---
# Fixes (2026-08-07): remembrances re-index 404, macOS clipboard shortcuts, model-switch confirmation

Three unrelated bugs reported together.

## 1. "Re-index All" returns 404 (Web UI / desktop, all OS)

**Cause**: `web-ui/src/components/settings/RemembrancesSettings.tsx` posted to
`/api/v1/config/remembrances/reindex`, a route that never existed — the remembrances endpoints
live under `/api/v1/remembrances/*` and no bulk re-index handler had ever been written.

**Change**:
- `internal/api/handlers_remembrances.go`: new `handleReindexAllCodeProjects`
  (`POST /api/v1/remembrances/reindex`). Lists every registered code project and calls
  `CodeIndexer.IndexProject(projectID, rootPath, nil)` for each. Indexing runs in a background
  goroutine with its own `context.Background()` (see `internal/rag/code/indexer.go:122`), so the
  HTTP response returns immediately and a per-project failure does not abort the rest. Response:
  `{ started: [{project_id, root_path, job_id}], failed: [{project_id, root_path, error}] }`,
  HTTP 202; 503 when the indexer is not initialised; 500 only when every project failed.
- `internal/api/routes.go`: route registered next to `remembrances/projects/index`.
- Frontend: corrected URL and the toast now reports how many projects started ("info" toast when
  there are no registered projects, instead of a misleading success).

## 2. macOS: CMD+C / CMD+V do not work (only the context menu does)

**Cause**: `desktop/main.go` ran Wails with **no application menu**. On macOS the clipboard
shortcuts are delivered by the app menu's Edit roles; WKWebView never sees Cmd+C/V/X/A when no
menu is installed. Linux (GTK/WebKitGTK) and Windows (WebView2) implement those keys internally,
which is why only macOS was affected.

**Change**: platform-split menu builder:
- `desktop/menu_darwin.go` — `appMenu()` returns `AppMenu() + EditMenu() + WindowMenu()`.
- `desktop/menu_other.go` — returns `nil` (no wasted menu bar outside macOS).
- `desktop/main.go` — `Menu: appMenu()` in `options.App`.

## 3. `pando_setup model` cost confirmation never reached the user

**Cause**: the handshake was documentation-only. `SetSessionModel` stored a proposal on the first
(unconfirmed) call and `consumeSetupModelProposal` accepted **any** later `--confirm`, including
one the agent issued immediately, in the same turn. Nothing forced the question to reach the
user, so on Web UI/desktop the switch could apply with no visible confirmation at all. The gate
was never a UI feature, so no dialog exists to be missing.

**Change** — approval must come from a real user turn, as plain text, on every surface:
- `internal/llm/agent/model_switch.go`: `modelSwitchRunState` gains a `seq` handed out by a
  process-wide `atomic.Uint64` in `beginModelSwitchRun` (one run == one user turn); new
  `currentModelSwitchRunSeq(sessionID)`.
- `internal/llm/agent/setup_bridge_model.go`: `setupModelProposal` records `QuotedRun`/`QuotedInRun`;
  `consumeSetupModelProposal` refuses a confirmation coming from the same run as the quote (new
  helpers `loadSetupModelProposal` / `hasLiveSetupModelProposal` keep the original quote alive
  instead of re-stamping it, which would otherwise let the agent loop the handshake inside one
  turn). A same-turn `--confirm` returns the quote again with `setupModelSelfConfirmNote`
  appended. With no run in flight (slash command between turns) the quote stays confirmable.
- `internal/llm/tools/pando_setup.go`: usage text and the "Confirmation required" render now tell
  the agent to ask in plain text and END THE TURN, and to repeat with `--confirm` only in a later
  turn — explicitly no dialog, no selection UI.

## Verification

- `go build ./...`, `go vet ./desktop/` (linux tags), `npx tsc --noEmit` in `web-ui`.
- `go test ./internal/llm/agent ./internal/api ./internal/llm/tools` — pass.
- `TestSetupBridgeQuoteDoesNotConsumeSwitchBudget` updated (confirm now happens in the next run)
  plus new `TestSetupBridgeConfirmNeedsANewTurn` and `TestSetupBridgeConfirmOutsideARunApplies`.
- macOS menu path cannot be compiled on this Linux host (cgo/objc); the `menu.AppMenu()/EditMenu()/
  WindowMenu()` API was checked against wails v2.12.0 sources.

Builds on [[pando/features/pando_setup_dynamic_model_switch.md]] and
[[pando/plans/pando_setup_dynamic_model_switch.md]], [[pando/plans/wails_desktop_app_plan.md]].
