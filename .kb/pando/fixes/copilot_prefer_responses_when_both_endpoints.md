---
created_at: 2026-11-19T21:44:14.333099059Z
updated_at: 2026-11-19T21:44:14.333099059Z
tags:
    - fix
    - copilot
    - models
    - provider
    - endpoints
---
# Fix: Copilot models advertising both /chat/completions and /responses now use /responses

Date: 2026-11-19. Related: [[copilot_model_endpoint_and_capability_metadata]], [[copilot_api_token_exchange_byok_custom_models]], [[copilot_stale_model_cache_endpoint_routing]].

## Symptom

Pando v1.5.6 (TUI, project `/www/rsphp`, telemetry 2026-11-19 22:27 CET):

```
failed to process events: POST "https://api.business.githubcopilot.com/chat/completions": 400 Bad Request
```

Reproduced with `pando -p "..." -m copilot.mai-code-2`. The raw response body is
`no model endpoints available given user constraints`.

## Root cause

`mai-code-2` advertises `supported_endpoints: ["/chat/completions", "/responses"]`
but only `/responses` actually serves it (200), `/chat/completions` answers 400.
`models.CopilotModelUsesResponsesAPI` kept the name heuristic (`^gpt-(\d+)`) when
both routes were advertised, so a non-gpt name was sent to Chat Completions.
On 2026-11-19 `mai-code-2` was the only model in the account catalogue (58 models)
advertising both routes.

## What did NOT change in the Copilot API (checked against live API)

- Auth/connection is fine: raw `gho_` OAuth token works as bearer on both
  `api.githubcopilot.com` and `api.business.githubcopilot.com`, `/models`,
  `/chat/completions`, `/responses`, `/v1/messages` all answer 200 with Pando's
  current headers (`Editor-Version`, `Copilot-Integration-Id: vscode-chat`, ...).
- The raw token now also lists the organization BYOK custom models (58 models),
  so the `copilot_internal/v2/token` exchange is no longer required for them.
  Exchange of Pando's device-flow token now answers 403 (was 404); fallback path works.

## opencode reference (updated to a21895aee6, 2026-11-19, re-indexed as `opencode`)

Copilot code moved to `packages/core/src/plugin/provider/github-copilot.ts` and
`packages/core/src/github-copilot/models.ts`. Differences from Pando worth knowing:

- Endpoint priority from `supported_endpoints`: `/v1/messages` > `/responses` > `/chat/completions`.
  Name fallback: gpt-5+ (except gpt-5-mini), `grok-*`, `mai-code-*` -> responses.
- Sends `X-GitHub-Api-Version: 2026-08-01` on every request (Pando sends none; with it
  `/models` returns 59 instead of 58 entries and `cache_price` is renamed `cache_read_price`).
- No token exchange: raw OAuth token as bearer; base URL read from
  `GET api.github.com/copilot_internal/user` -> `endpoints.api` (header `X-GitHub-Api-Version: 2025-04-01`),
  refreshed with the model list.
- New headers: `X-Interaction-Type` (`conversation-agent|subagent|background|compaction`),
  `X-Interaction-Id` (session id); `x-initiator: agent` for non-user turns.
- Copilot "Auto" model: `POST {base}/auto` returns `session_token` + `selected_model`,
  sent back as `Copilot-Session-Token`.
- Titles via free utility model `gpt-4o-mini` on `/chat/completions`.
- Env token is `COPILOT_GITHUB_TOKEN` (not `GITHUB_TOKEN`).
- Drops models with `policy.state == "disabled"`.

None of these are required today; they are candidates for follow-up work.

## Change

`internal/llm/models/copilot.go`:
- `CopilotModelUsesResponsesAPI`: when both routes are advertised, return true
  unless the model is in the `gpt-5-mini` family (new helper `isCopilotGPT5Mini`).
- `IsCopilotResponsesAPIModel` (no-metadata fallback): `grok-*` and `mai-code-*` -> true.

`internal/llm/models/copilot_endpoints_test.go`: cases for `mai-code-2` with both
endpoints, and `mai-code-2` / `grok-4.7` without metadata.

## Verification

- `go test ./internal/llm/models/ ./internal/llm/provider/` -> ok.
- Dev build, one-shot prompt with an `ls` tool call: `copilot.mai-code-2` (was 400, now ok),
  `copilot.gpt-6-luna`, `copilot.gemini-4-argon`, `copilot.claude-haiku-5.5`,
  `copilot.madeindigio/gemini_ai_copilot/gemini-flash-latest` all ok.

Not committed, not released.
