---
created_at: 2026-10-21T15:31:04.912225518Z
updated_at: 2026-10-21T15:31:04.912225518Z
tags:
    - pando
    - fix
    - webui
    - tokens
    - context-window
---
# Fix: WebUI context bar alternates between two windows after a model switch

Date: 2026-10-21. Related: [[pando/features/realtime_context_token_counter]], [[pando/features/rich_token_usage_panel]].

## Symptom
In the WebUI, after switching the session to a model with a different context window, the composer/sidebar context bar kept jumping between the old and the new window size during a run.

## Cause
Two `token_usage` emitters in `internal/llm/agent/agent.go` disagreed on the model:
- Provisional per-tool-result estimates (`streamAndHandleEvents`) took the window from `tokenEstimateBase`, which used `a.provider.Model()` — the agent's configured model.
- Confirmed usage on `EventComplete` used `requestProvider.Model()` — the per-session override set by the WebUI model switcher.
With a session override these differ, so each event overwrote `session.context_window` in the WebUI store with alternating values.

## Changes
- `internal/llm/agent/agent.go`: `tokenEstimateBase(ctx, sessionID, model models.Model)` now takes the request provider's model; caller passes `requestProvider.Model()`.
- `web-ui/packages/pando-client/src/stores/sessionStore.ts`: new `setSessionContextWindow(id, contextWindow)` action.
- `web-ui/src/components/overlays/ModelSwitcher.tsx`: on a session-scoped switch, sets the session's `context_window` to the picked model's `contextWindow` immediately (no wait for the next run). An agent `ContextWindowOverride` still wins on the next confirmed event.

## Not changed (note)
`processGeneration` history trimming (~line 1301) still budgets with `a.provider.Model()`; `ensureHistoryFitsBeforeSend` later re-fits with the request provider, so it is not a correctness bug for the bar.

## Verification
`go build ./...`, `go test ./internal/llm/agent ./internal/api` ok; web-ui `tsc` clean; vitest pando-client + overlays 25/25 pass. No manual browser check.
