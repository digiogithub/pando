# Antigravity OAuth Provider Plan for Pando

## Objective

Add a new `antigravity` provider to Pando that supports Google OAuth login, multiple Google accounts, and automatic rotation across accounts and quota pools when a selected Antigravity model exhausts quota.

The target behavior should mirror the reference project `antigravity-auth`:
- Google OAuth with PKCE
- persistent multi-account storage
- per-account token refresh
- Antigravity-first routing for Gemini-family models
- automatic fallback to Gemini CLI quota when Antigravity quota is exhausted
- automatic account rotation when one account is rate-limited or exhausted

---

## Reference Analysis Summary

### OAuth flow in antigravity-auth

From `src/antigravity/oauth.ts`:

1. `authorizeAntigravity(projectId = "")`
   - generates PKCE pair
   - builds Google OAuth authorization URL
   - stores `verifier` and `projectId` inside OAuth `state`
   - requests offline access and consent

2. `exchangeAntigravity(code, state)`
   - decodes `state`
   - exchanges authorization code at `https://oauth2.googleapis.com/token`
   - fetches user info from Google
   - resolves `projectId` via `fetchProjectID(accessToken)` when not already known
   - returns refresh token, access token, expiry, email, and projectId

3. `fetchProjectID(accessToken)`
   - calls Antigravity `loadCodeAssist`
   - extracts `cloudaicompanionProject` identifier
   - used to support Gemini CLI quota access

### Multi-account and rotation behavior in antigravity-auth

From `docs/MULTI-ACCOUNT.md`, `docs/ARCHITECTURE.md`, and `src/plugin/accounts.ts`:

- Accounts persist:
  - email
  - refresh token
  - projectId
  - enabled flag
- Runtime state tracks:
  - rate limit reset times
  - cooldowns
  - failures
  - cached quota state
  - per-account fingerprint/identity metadata
- Selection strategies:
  - sticky
  - round-robin
  - hybrid
- Gemini routing uses two quota pools:
  - Antigravity
  - Gemini CLI
- Behavior:
  1. use Antigravity pool first
  2. if current account is limited, try another account on Antigravity
  3. if all accounts are exhausted on Antigravity, fall back to Gemini CLI
  4. continue rotating automatically across accounts and pools

---

## Current Pando Capabilities Relevant to This Work

Pando already provides:

1. Multi-account provider config
   - `internal/config/config.go`
   - `ProviderAccount`
   - `Config.ProviderAccounts`
   - `ResolveProviderAccountByID`
   - `ResolveProviderAccountForType`
   - `AccountsForProviderType`

2. Account-aware models
   - `internal/llm/models/models.go`
   - `Model.AccountID`

3. Dynamic model refresh per account
   - `internal/llm/models/registry.go`
   - `RefreshProviderModelsForAccount`

4. Account resolution in agent provider creation
   - `internal/llm/agent/agent.go`
   - resolves model by `AccountID` when present, else by provider type

These are strong foundations, but they are not sufficient on their own for Antigravity because runtime scheduling and OAuth account state do not yet exist.

---

## Recommended Design

## Create a new provider type: `antigravity`

Do **not** fold this into the existing `gemini` provider.

### Why

Antigravity is not only a Gemini API-key provider. It has distinct characteristics:
- Google OAuth instead of API-key authentication
- Antigravity Unified Gateway transport semantics
- optional Gemini CLI fallback pool requiring project identity
- account rotation logic at runtime
- potential support for Gemini and Claude through the same Google-backed gateway

A dedicated provider keeps the model, config, UX, and runtime behavior explicit and maintainable.

---

## Implementation Plan

## Phase 1 - Extend provider account model and config storage

### Goal
Enable OAuth-backed Antigravity accounts as first-class provider accounts.

### Changes

1. Add `models.ProviderAntigravity = "antigravity"`

2. Extend `config.ProviderAccount` with Antigravity-specific fields:
   - `OAuthRefreshToken string`
   - `OAuthAccessToken string` (optional persisted cache)
   - `OAuthExpiry int64` (optional persisted cache)
   - `ProjectID string`
   - `Email string`
   - optional future metadata fields if needed

3. Preserve existing `APIKey` and `BaseURL` fields for non-OAuth providers.

4. Extend secret encryption in `internal/config/agecrypto.go` to encrypt:
   - `OAuthRefreshToken`
   - optionally `OAuthAccessToken`

5. Update API provider types in `internal/api/handlers_provider_accounts.go`:
   - add `antigravity`
   - mark as `SupportsOAuth: true`
   - mark as `RequiresAPIKey: false`

### Result
Pando can persist Google OAuth-backed Antigravity accounts securely.

---

## Phase 2 - Implement Google OAuth + PKCE flow for Antigravity

### Goal
Allow users to log in with Google and create Antigravity provider accounts from Pando.

### New components

Suggested package:
- `internal/oauth/antigravity/`

Suggested functions:
- `AuthorizeURL(projectID string) (... )`
- `ExchangeCode(code string, state string) (... )`
- `RefreshAccessToken(refreshToken string) (... )`
- `FetchProjectID(accessToken string) (... )`

### Responsibilities

1. Generate PKCE verifier/challenge
2. Build Google OAuth authorization URL
3. Encode/decode state carrying verifier and optional projectId
4. Exchange auth code for tokens
5. Fetch user email
6. Resolve projectId using Antigravity `loadCodeAssist`
7. Normalize results into `config.ProviderAccount`

### API surface

Suggested endpoints:
- `POST /api/v1/oauth/antigravity/start`
- `GET /api/v1/oauth/antigravity/callback`
- `POST /api/v1/provider-accounts/{id}/oauth/refresh`
- `POST /api/v1/provider-accounts/{id}/oauth/verify`

### Result
Users can authenticate with Google and register multiple Antigravity accounts in Pando.

---

## Phase 3 - Implement Antigravity provider client

### Goal
Create a provider implementation that talks to the Antigravity gateway.

### New component
- `internal/llm/provider/antigravity.go`

### Responsibilities

1. Build Antigravity gateway requests
2. Use Google OAuth access tokens
3. Auto-refresh access tokens when expired
4. Support chat + streaming
5. Convert gateway responses into `ProviderResponse` and `ProviderEvent`
6. Parse error bodies for:
   - quota exhaustion
   - rate limits
   - capacity issues
   - auth failures
   - project access errors

### Scope strategy

Recommended initial scope:
- full support for Antigravity-routed Gemini models first
- architect for Claude-via-Antigravity, but optionally defer complete Claude parity to a later phase if needed

### Result
Pando can send model requests through Antigravity using OAuth-backed Google accounts.

---

## Phase 4 - Add dynamic model discovery/registration for Antigravity

### Goal
Expose Antigravity models cleanly in Pando.

### Recommended visible models
Logical aggregated models such as:
- `antigravity/gemini-3-pro`
- `antigravity/gemini-3.1-pro`
- `antigravity/gemini-3-flash`
- optionally `antigravity/claude-sonnet-4-6`
- optionally `antigravity/claude-opus-4-6-thinking`

### Important design point
Pando should avoid forcing the user to select a specific account for Antigravity models. The user should select a logical Antigravity model and let the runtime choose the best account.

### Registry work
Extend model refresh/registration so that:
- per-account models may still exist internally for diagnostics and discovery
- user-facing logical models can remain account-agnostic
- account selection happens at request time

### Result
Users see stable Antigravity model choices while Pando handles account routing automatically.

---

## Phase 5 - Build Antigravity multi-account scheduler

### Goal
Automatically rotate across Google accounts and quota pools when a selected Antigravity model exhausts quota.

### New component
Suggested:
- `internal/llm/provider/antigravity_scheduler.go`

### Runtime state per account
Track at least:
- account ID
- email
- enabled/disabled
- last used timestamp
- cooldown until
- consecutive failures
- last failure time
- quota state by pool
- quota state by model family
- health score

### Quota pools
For Gemini-family Antigravity requests, treat each account as having:
- pool `antigravity`
- pool `gemini-cli`

### Routing behavior
When an Antigravity Gemini model is selected:
1. try current or best account on Antigravity pool
2. if account is limited, try other available Antigravity accounts
3. if all Antigravity accounts are exhausted, try Gemini CLI pool
4. if all pools are exhausted, wait until nearest reset or return a clear error

### Scheduling strategies
Implement in this order:
1. sticky
2. round-robin
3. hybrid (health-score driven)

### Recommendation
For a first Pando implementation, sticky + round-robin are enough if the scheduler state is designed so hybrid can be added without redesign.

### Result
Model usage rotates automatically and transparently across Google accounts and quota pools.

---

## Phase 6 - Token lifecycle management

### Goal
Refresh access tokens automatically before or during request execution.

### Behavior
Before dispatching a request:
1. resolve the chosen account
2. check cached access token expiry
3. if missing or expired, refresh using OAuth refresh token
4. continue request with valid bearer token

### Persistence recommendation
Persist:
- refresh token (encrypted)

Optionally persist:
- access token and expiry

Preferred initial approach:
- keep refresh token persisted
- cache access token in memory
- optionally persist encrypted access token later for faster restarts

### Result
Users do not need to reauthenticate during normal operation.

---

## Phase 7 - Integrate with API and TUI flows

### Goal
Make Antigravity account login and management usable in the current Pando UX.

### API updates
- add provider type `antigravity`
- add OAuth start/callback endpoints
- add per-account test/refresh/verify endpoints
- expose email, projectId, and status safely

### TUI updates
- include Antigravity in add-provider dialog
- support "Login with Google"
- allow adding multiple Google accounts
- allow enable/disable account
- allow reauthenticate account
- optionally display pool and quota health summary

### UX principle
Selecting an Antigravity model should not require selecting an account unless the user explicitly chooses an advanced mode.

### Result
The provider is discoverable and manageable from the same places as existing providers.

---

## Phase 8 - Error handling and failover semantics

### Goal
Ensure runtime failover is correct and deterministic.

### Error categories to recognize
- quota exhausted
- temporary rate limit
- model capacity exhausted
- invalid grant / revoked refresh token
- invalid or missing project access
- verification-required or soft-blocked account

### Expected reactions
- `quota exhausted`
  - mark pool unavailable until reset
  - switch account or pool
- `rate limited`
  - apply cooldown
  - try another account
- `capacity exhausted`
  - short backoff, then retry another account if appropriate
- `invalid_grant`
  - disable account or mark reauth required
- `projectId invalid`
  - keep account eligible for Antigravity pool if possible
  - mark Gemini CLI pool unusable for that account

### Result
Failures degrade gracefully instead of blocking all Antigravity usage.

---

## Phase 9 - Logical aggregated Antigravity models

### Goal
Match the required end-user behavior exactly.

When the user selects a model such as `antigravity/gemini-3-pro`, Pando must:
- automatically select a Google account
- automatically rotate to another Google account when quota is exhausted
- automatically fall back between Antigravity and Gemini CLI pools when needed

### Implementation note
This likely requires treating Antigravity models as logical models whose account binding is decided at runtime, instead of only using `Model.AccountID`.

Recommended approach:
- keep `AccountID` empty for logical aggregated Antigravity models
- use scheduler-based runtime resolution
- retain account-bound internal models only for diagnostics and dynamic discovery

### Result
The UX matches the requested behavior exactly.

---

## Phase 10 - Testing strategy

### Unit tests

1. OAuth state encode/decode
2. PKCE flow construction
3. code exchange response parsing
4. project ID extraction from `loadCodeAssist`
5. token refresh logic
6. scheduler selection logic
7. account rotation on pool exhaustion
8. Antigravity-to-Gemini-CLI fallback logic
9. disabled account exclusion
10. invalid token / revoked account handling

### Integration tests

1. add multiple Antigravity accounts
2. select Antigravity Gemini model
3. simulate quota exhaustion on account A
4. verify switch to account B
5. simulate all Antigravity pool exhaustion
6. verify fallback to Gemini CLI pool
7. simulate revoked refresh token
8. verify account is marked unusable or reauth-required

---

## Risks and Design Constraints

1. Current Pando account resolution is mostly static by provider type or AccountID. Antigravity needs request-time account selection.
2. `ProviderAccount` is currently optimized for API-key providers and must evolve carefully.
3. Dynamic model registration currently assumes account-bound models more than logical aggregated models.
4. Antigravity compatibility depends on headers, auth style, and request shaping that should closely mirror the working reference project.
5. This provider may carry policy and account-risk considerations, so docs and UX should communicate that clearly.

---

## Recommended Delivery Order

### Milestone 1
- Add `ProviderAntigravity`
- Extend config and secure storage
- Add OAuth endpoints and token exchange
- Persist one working Antigravity account

### Milestone 2
- Implement Antigravity provider client
- Support one-account requests for Gemini models
- Register basic Antigravity models

### Milestone 3
- Add multi-account scheduler
- Add automatic rotation across accounts
- Add fallback from Antigravity to Gemini CLI pool

### Milestone 4
- Expose logical aggregated Antigravity models in UI/API
- Add TUI account management and health visibility
- Add robust error handling and verification flows

---

## Final Recommendation

The cleanest implementation in Pando is:
- a dedicated `antigravity` provider
- OAuth-backed provider accounts
- runtime scheduler-based account selection
- logical account-agnostic Antigravity model IDs
- transparent multi-account and multi-pool failover

That design fits both the reference behavior and Pando's existing multi-account foundation while minimizing confusion with the existing `gemini` API-key provider.