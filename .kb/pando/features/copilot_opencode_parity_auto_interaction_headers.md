---
created_at: 2026-11-19T22:29:24.492072274Z
updated_at: 2026-11-19T22:29:24.492072274Z
tags:
    - feature
    - copilot
    - provider
    - auth
    - models
---
# Feature: Copilot provider parity with opencode (API version, endpoint discovery, interaction headers, Auto, utility titles)

Date: 2026-11-19. Builds on [[copilot_prefer_responses_when_both_endpoints]], [[copilot_api_token_exchange_byok_custom_models]], [[copilot_model_endpoint_and_capability_metadata]].

Reference: opencode `packages/core/src/plugin/provider/github-copilot.ts` and
`packages/core/src/github-copilot/models.ts` at commit a21895aee6.

## What changed

### 1. `X-GitHub-Api-Version: 2026-08-01` on every Copilot API request
- New `auth.CopilotAPIVersion`, `auth.CopilotClientHeaders()`, `auth.ApplyCopilotClientHeaders()`
  (`internal/auth/copilot_user.go`): single source for `User-Agent`, `Editor-Version`,
  `Editor-Plugin-Version`, `Copilot-Integration-Id`, `X-GitHub-Api-Version`.
- Used by `ValidateCopilotToken`, `CheckCopilotModelsAPI`, `models.requestCopilotModels`,
  the chat/responses clients and the `/auto` request. The token-exchange request keeps its own headers.

### 2. API endpoint discovery from the account record
- `auth.FetchCopilotUser` reads `GET api.github.com/copilot_internal/user`
  (`X-GitHub-Api-Version: 2025-04-01`); `auth.DiscoverCopilotAPIEndpoint` caches
  `endpoints.api` (30 min, failures 10 min).
- `auth.ResolveCopilotAPIAccess`: the token exchange is still tried first; when it is
  unavailable the raw OAuth token is now sent to the account's own host instead of the
  default `api.githubcopilot.com`. A configured base URL still wins.
- Login: `CompleteCopilotDeviceFlow` rejects accounts with `chat_enabled == false`
  (`auth.CopilotEntitlementError`); a failed lookup never denies. `ValidateCopilotToken`
  validates against the discovered host.

### 3. Interaction headers
- `copilotClient.requestHeaders(ctx, messages)` adds `X-Interaction-Type` and
  `X-Interaction-Id` (session id from `tools.SessionIDContextKey`).
- `x-initiator` is `agent` for every non-agent-loop interaction.
- `provider.WithCopilotInteractionType`; `agent.copilotAgentOptions` maps agents:
  coder/cli-assist -> `conversation-agent`, task -> `conversation-subagent`,
  summarizer -> `conversation-compaction`, title/persona-selector/context-enricher -> `conversation-background`.
- `provider.WithCopilotOptions` now appends instead of replacing.
- Not covered: mesnada child sessions running the coder agent still report `conversation-agent`.

### 4. Copilot "Auto" model (`copilot.auto`)
- `models.requestCopilotModels` parses `billing.auto_discount` and appends a synthetic
  `auto` model when the account has an Auto pool (picker-enabled, tool-calling models);
  limits are the smallest of the pool. `models.CopilotAutoModelID`, `models.IsCopilotAutoModel`.
- `internal/llm/provider/copilot_auto.go`: `forRequest` returns a per-request copy of the
  client bound to the model picked by `POST {base}/auto` (`prompt`, `has_image`), sends
  `Copilot-Session-Token`, caches the pick per session until 5 min before `expires_at`,
  re-routes after a compaction request or an account change. A pick missing from the catalog
  is built from the router's own `selected_model` metadata. Router failure with a previous
  pick keeps that model without the token; without one the request fails with an error
  (opencode instead falls back to the `is_chat_fallback` model; Pando does not persist that flag).

### 5. Session titles on Copilot's free utility model
- `provider.WithCopilotUtilityModel` / `CopilotUtilityTitleModel = "gpt-4o-mini"`:
  the title agent, when its model is a Copilot model, first tries `gpt-4o-mini` over
  `/chat/completions` (non-streaming, 20 s). Any failure falls back to the configured title
  model and is remembered for 30 min.
- Difference from opencode: opencode only does this when the title agent has no explicit
  model; Pando's title agent always has one, so it applies whenever that model is Copilot's.

### Already present before this change
`COPILOT_GITHUB_TOKEN` as first token candidate; dropping `policy.state == "disabled"` models.

## Files
`internal/auth/copilot_user.go` (new), `internal/auth/copilot.go`, `internal/auth/copilot_apitoken.go`,
`internal/llm/models/copilot.go`, `internal/llm/models/fetcher.go`,
`internal/llm/provider/copilot.go`, `internal/llm/provider/copilot_auto.go` (new),
`internal/llm/provider/provider.go`, `internal/llm/agent/agent.go`, plus tests
(`copilot_user_test.go`, `copilot_apitoken_test.go`, `copilot_endpoints_test.go`, `copilot_auto_test.go`).

## Verification
- `go test ./internal/auth/ ./internal/llm/... ./internal/api` -> ok.
- Dev build, one-shot prompt with an `ls` tool call, all ok: `copilot.auto`, `copilot.mai-code-2`,
  `copilot.gpt-6-luna`, `copilot.gemini-4-argon`, `copilot.claude-haiku-5.5`,
  `copilot.madeindigio/OpenRouter/moonshotai%2Fkimi-k3`.
- `gpt-4o-mini` over `/chat/completions` with the new headers answers 200 (curl).
- Not exercised live: device-flow login with the entitlement check, GitHub Enterprise hosts,
  title generation end to end inside a TUI session.
