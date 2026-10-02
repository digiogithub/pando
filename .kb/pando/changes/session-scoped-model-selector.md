---
created_at: 2026-10-02T10:37:44.711407768Z
updated_at: 2026-10-02T10:37:44.711407768Z
tags:
    - change
    - models
    - webui
    - tui
    - api
    - agent
---
# Session-scoped model selector (2026-10-02)

## What changed
The model selector (WebUI `ModelSwitcher`, TUI model dialog) now changes the model of the **current session only**. It no longer rewrites `agents.coder.model` (nor `modelAutoMode.selected`) in the configuration. The persisted coder model stays the default of every session and is changed from Settings (WebUI `handleSetSettings` DefaultModel, TUI settings page).

The selection is stored as the existing in-memory per-session override (`agent.SessionLLMOverrides.Model` / `.AutoMode`), the same mechanism used by ACP and `pando_setup model` (see [[pando/features/pando_setup_dynamic_model_switch.md]]). It is not persisted: after a restart a session falls back to the configured coder model.

## Behaviour
- `PUT /api/v1/models/active` accepts an optional `sessionId`. With it: validates (`config.ValidateAgentModel`, so config locks still apply) and calls `agent.SetSessionModelOverride` / `agent.SetSessionAutoMode`; responds `{"model", "scope":"session"}`. Without it, or while no coder model is configured (fresh machine: an override needs a default to fall back to, see [[pando/fixes/fresh_machine_coder_model_persistence.md]]), it persists as before and responds `scope: "config"`.
- New `GET /api/v1/sessions/{id}/model` -> `{sessionId, model, override, autoSelected}`.
- `ChatRequest.Model` (`applyRequestModel`) is now applied as a session override when the model is known; unknown names are still ignored (logged at debug). `"auto"` unchanged. This carries a selection made on an empty chat to the session the first prompt creates.
- WebUI: new `sessionModelStore` (pando-client) follows `sessionStore.activeSessionId`, loads the session model from the server, keeps a pending selection while no session exists (`pendingSessionModel()` sent as `model` by `useChat.sendMessage`). `useActiveModelSelection` / `useActiveModelLabel` (status bar, chat chip, switcher highlight) read session selection first, then `default_model` / global auto.
- TUI: `dialog.ModelSelectedMsg` sets the override on the selected session; with no session it is stored under `agent.DraftSessionID` and moved by `agent.AdoptDraftSessionOverrides` in `ChatPageModel.ensureSession`. A leftover draft is cleared on `chat.SessionSelectedMsg`. Status bar reads the draft when no session is selected. A model can now be switched while the agent is busy (mid-run switch path in `model_switch.go`), since `CoderAgent.Update` is no longer involved.

## Files / symbols
- `internal/api/handlers_models.go`: `handleSetActiveModel`, `selectSessionModel`, `handleGetSessionModel`
- `internal/api/handlers_chat.go`: `applyRequestModel`
- `internal/api/routes.go`: new route
- `internal/llm/agent/session_overrides.go`: `DraftSessionID`, `AdoptDraftSessionOverrides`
- `internal/tui/tui.go` (`ModelSelectedMsg`, `SessionSelectedMsg`), `internal/tui/page/chat.go` (`ensureSession`), `internal/tui/components/core/status.go` (`modelSessionID`, `autoLabel`)
- `web-ui/packages/pando-client/src/stores/sessionModelStore.ts` (new), `hooks/useChat.ts`
- `web-ui/src/components/overlays/ModelSwitcher.tsx`, `web-ui/src/utils/modelLabel.ts`

## Verification
- `go build ./...`, `go test ./internal/llm/agent ./internal/api ./internal/tui/...` pass. New tests: `TestHandleSetActiveModelSessionScope`, `TestAdoptDraftSessionOverrides`.
- WebUI: `tsc` typecheck clean; vitest `ModelSwitcher.test.tsx` (new session-scope case) and `modelLabel.test.ts` pass.
- Not verified live in a running WebUI/TUI.

## Known limits
- TUI model dialog still pre-highlights from the global config, not the session override.
- Override is in memory only; not restored after restart.
