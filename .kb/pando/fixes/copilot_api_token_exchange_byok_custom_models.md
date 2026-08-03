---
created_at: 2026-08-03T21:58:21.566311864Z
updated_at: 2026-08-03T21:58:21.566311864Z
tags:
    - fix
    - copilot
    - auth
    - models
    - byok
---
# Fix: Copilot API token exchange — organization BYOK custom models now visible

Date: 2026-08-03

## Problem

The GitHub Copilot provider never listed the organization's BYOK "custom models"
(ids like `madeindigio/OpenRouter/qwen%2Fqwen3.7-max`, `.../moonshotai%2Fkimi-k3`,
`.../z-ai%2Fglm-5.2`, plus `madeindigio/gemini_ai_copilot/*`), even though VS Code
showed them in the same picker as the Copilot-hosted models.

## Root cause

Pando sent the **raw GitHub OAuth token** (`gho_…`) as the bearer for
`https://api.githubcopilot.com/models` (`internal/llm/models/fetcher.go`) and for
chat requests (`internal/llm/provider/copilot.go`).

Editors instead exchange that OAuth token at
`GET https://api.github.com/copilot_internal/v2/token`, which returns:

- a short-lived Copilot API token (`tid=…;exp=…`, ~30 min),
- `endpoints.api` — the per-seat host (here `https://api.business.githubcopilot.com`),
- `organization_list` / `sku` (here `copilot_for_business_seat_quota`).

Only that exchanged token carries the seat/organization context, and only it makes
the enterprise/org BYOK custom models appear. Measured on the affected machine:

| bearer | host | models |
|---|---|---|
| raw `gho_` OAuth token | api.githubcopilot.com | 29 (0 custom) |
| exchanged Copilot API token | api.business.githubcopilot.com | 50 (20 custom) |

Two secondary findings:

1. Pando's own device-flow token (client id `Ov23li8tweQw6odWQebz`, scope
   `read:user`) **cannot** be exchanged — `copilot_internal/v2/token` answers 404.
   The editor-issued tokens in `~/.config/github-copilot/apps.json` can.
2. The per-seat hosts reject requests without editor headers:
   `GET https://api.business.githubcopilot.com/models` returns **400** without
   `Editor-Version` / `Copilot-Integration-Id`, and 200 with them.

## Changes

### `internal/auth/copilot_apitoken.go` (new)

- `ExchangeCopilotAPIToken(ctx, oauthToken, enterpriseURL)` — performs the
  exchange, in-memory cache keyed by (enterprise, token) until 2 min before
  `expires_at`, plus a 10 min negative cache so tokens that cannot be exchanged
  are not retried on every request.
- `ResolveCopilotAPIAccess(ctx, token, enterpriseURL, configuredBaseURL)` —
  single entry point returning `(bearer, baseURL)`. Falls back to the raw token
  and the default host whenever the exchange is unavailable. An explicitly
  configured base URL always wins over `endpoints.api`.
- `exchangeAnyLocalToken` — when the supplied token cannot be exchanged (Pando's
  device-flow token), walks the other locally available GitHub OAuth tokens and
  uses the first one that can. Same seat, so the resulting catalog is the user's.
- `IsCopilotAPIToken` (detects `tid=`), `CopilotTokenExchangeDisabled`
  (`PANDO_COPILOT_TOKEN_EXCHANGE=0|false|off|no` restores the legacy behaviour),
  `copilotTokenExchangeURL` (GHES uses `https://<host>/api/v3/...`).

### `internal/auth/copilot.go`

- `GitHubOAuthTokenCandidates()` — new; returns every locally discovered OAuth
  token (env vars, pando session, `~/.config/github-copilot/{hosts,apps}.json`,
  gh CLI) in the previous priority order, de-duplicated and tagged with a source.
  `LoadGitHubOAuthToken()` is now `candidates[0]`, so its behaviour is unchanged.
- `loadLegacyCopilotTokens()` — returns all editor tokens (sorted host order for
  determinism) instead of only the first; `loadLegacyCopilotToken` wraps it.
- `ValidateCopilotToken` and `CheckCopilotModelsAPI` now send `Editor-Version`,
  `Editor-Plugin-Version` and `Copilot-Integration-Id` — without them the
  business/enterprise hosts answer 400 and the provider was disabling itself.

### `internal/llm/models/fetcher.go`

- `fetchCopilotModels` resolves the access via `auth.ResolveCopilotAPIAccess`,
  targets `<resolved host>/models`, and sends the editor headers.
- On failure it retries the legacy call (raw token, default host) before giving
  up — split into a reusable `requestCopilotModels` helper.
- `copilotModelsURL` now defaults to the new `defaultCopilotModelsURL` const; when
  it has been overridden (tests, custom deployments) the exchange is skipped and
  the given URL is used verbatim.

### `internal/llm/provider/copilot.go`

- `loadCopilotCredentials` returns a `copilotCredentials` struct
  (`sourceToken`, `bearerToken`, `baseURL`, `enterpriseURL`) so the short-lived
  bearer can be renewed later; it resolves through `ResolveCopilotAPIAccess`.
- `copilotClient` keeps `sourceToken` / `enterpriseURL`; `refreshBearerToken()`
  runs on every `requestClient()` call and renews the exchanged token near
  expiry (cache hit, no network while valid).
- `newCopilotClient`: if `CheckCopilotModelsAPI` rejects the exchanged token or
  the advertised host, it retries with the raw token against the default host and
  only disables the provider when that also fails.
- `reloadCredentials` updated for the new struct.

## Verification

- `go build ./...`, `go test ./internal/...` — all green.
- New unit tests: `internal/auth/copilot_apitoken_test.go` (parsing, expiry
  margin, exchange URL for github.com/GHES, fallback paths, configured base URL
  wins) and `internal/auth/copilot_candidates_test.go` (candidate order, dedup,
  PAT rejection, no-source error) with `XDG_CONFIG_HOME`/`HOME` isolated.
- Live probes (temporary, removed afterwards):
  - `fetchCopilotModels` through the real stack: **32 models, 20 custom**
    (deepseek-v4-pro/flash, kimi-k3, qwen3.7/3.8, glm-5.2, minimax-m3,
    xiaomi mimo-v2.5, gemini/gemma via `gemini_ai_copilot`).
  - With `PANDO_COPILOT_TOKEN_EXCHANGE=0`: 12 models, 0 custom — legacy path
    intact.
  - Full provider send against `madeindigio/OpenRouter/z-ai%2Fglm-5.2`:
    `response="PONG" finish=end_turn`, `baseURL=https://api.business.githubcopilot.com`.

## Notes

- Model ids keep their URL-encoded form (`%2F`); they travel in the JSON body,
  not the path, so no extra escaping is required.
- Related: [[fix_copilot_endpoint_metadata_routing]],
  [[copilot_model_endpoint_and_capability_metadata]].
