# Plan: Multi-Account Provider System in Pando

**Created**: 2026-04-23
**Status**: planned
**Goal**: Allow multiple accounts per provider, add OpenAI-compatible providers with URL, API key and custom headers, and update TUI + Web-UI to manage them.

## Context and motivation

The current architecture uses `Config.Providers map[models.ModelProvider]Provider`, which only allows **one account per provider type**. To support two Anthropic accounts, two OpenAI accounts, etc., a "named accounts" model is needed where each account has a unique ID and a provider type.

### Current structure (limitation)
- `Config.Providers map[models.ModelProvider]Provider` → key = type ("anthropic"), value = {APIKey, BaseURL, Disabled, UseOAuth}
- Only one provider of the same type throughout the system
- No support for additional HTTP headers

### Proposed structure
- `Config.ProviderAccounts []ProviderAccount` → list of named accounts
- Each account: `{ID, DisplayName, Type, APIKey, BaseURL, ExtraHeaders, Disabled, UseOAuth}`
- New provider type: `openai-compatible` for generic OpenAI-API-compatible endpoints
- Model selector shows `account: model` when there are multiple accounts of the same type

---

## Implementation phases

### Phase 1: ProviderAccount data model and config migration
**Fact ID**: `multi_account_providers_phase1_data_model`

- New struct `ProviderAccount` in `internal/config/config.go`
- Replace `Config.Providers` with `Config.ProviderAccounts []ProviderAccount`
- Automatic migration in `Load()`: if `providerAccounts` empty and `providers` has data → auto-migrate
- CRUD functions: `AddProviderAccount`, `UpdateProviderAccount`, `DeleteProviderAccount`, `GetProviderAccounts`, `GetProviderAccount`
- New `ProviderOpenAICompatible ModelProvider = "openai-compatible"` in `models/models.go`
- Migration tests in `config_test.go`

### Phase 2: Provider layer and model registration
**Fact ID**: `multi_account_providers_phase2_provider_layer`

- `providerClientOptions` gains field `extraHeaders map[string]string`
- New function `NewProviderFromAccount(account config.ProviderAccount, ...)` in `provider.go`
- `ProviderOpenAICompatible` support in the `NewProvider` switch
- `Model` struct gains field `AccountID string`
- New function `RefreshProviderModelsForAccount(ctx, account)` in `registry.go`
- Model ID prefix logic: if 1 account of the type → prefix = type (backward compatible); if 2+ → prefix = account.ID
- Function `DisplayLabel(allAccounts []ProviderAccount) string` in `Model`

### Phase 3: Wiring in App, Agent and model selector
**Fact ID**: `multi_account_providers_phase3_wiring`

- `app.refreshDynamicModels()` iterates `ProviderAccounts` instead of `Providers`
- New function `config.ResolveProviderAccount(model)` for lookups by model
- Replace direct accesses `cfg.Providers[model.Provider]` → `config.ResolveProviderAccount(model)`
- `handleListModels` includes `accountId` in `ModelInfo` and uses `DisplayLabel()`
- Update `handlers_models.go` to operate with accounts

### Phase 4: REST API for provider account management
**Fact ID**: `multi_account_providers_phase4_rest_api`

New routes in `routes.go`:
```
GET    /api/v1/config/provider-accounts         → list all accounts
POST   /api/v1/config/provider-accounts         → create account
GET    /api/v1/config/provider-accounts/{id}    → get account by ID
PUT    /api/v1/config/provider-accounts/{id}    → update account
DELETE /api/v1/config/provider-accounts/{id}    → delete account
POST   /api/v1/config/provider-accounts/{id}/test → test connectivity
GET    /api/v1/config/provider-types            → list supported types with metadata
```

- New file `internal/api/handlers_provider_accounts.go`
- Backward compatibility: `GET/PUT /api/v1/config/providers` still works (reads/writes to ProviderAccounts)
- Test endpoint returns `{ok: bool, modelCount: int, error?: string}`

### Phase 5: TUI configuration panel
**Fact ID**: `multi_account_providers_phase5_tui`

- New "Provider Accounts" section in `internal/tui/page/settings.go`
- New dialog `internal/tui/components/dialog/provider_account_dialog.go`
  - Form: ID, DisplayName, Type, APIKey, BaseURL, ExtraHeaders (dynamic key-value list), Disabled, UseOAuth
  - Inline "Test" action from the dialog
- Keys in the section: `a`/`+` add, `e`/`Enter` edit, `d`/`Delete` delete, `t` test, `space` toggle
- Model selector in chat: uses `DisplayLabel()` → "mywork: Claude Sonnet 4.5"

### Phase 6: Web-UI configuration panel
**Fact ID**: `multi_account_providers_phase6_webui`

- `web-ui/src/api/providerAccounts.ts` — typed API client
- `web-ui/src/components/settings/ProviderAccountsSection.tsx` — accounts table
- `web-ui/src/components/settings/ProviderAccountDialog.tsx` — add/edit modal with dynamic headers
- `web-ui/src/components/ModelSelector.tsx` — update labels with account
- ID validation (slug `/^[a-z0-9-]+$/`)
- Inline connectivity test with loading indicator
- Hot-reload via SSE `config_changed`

---

## Design considerations

### Backward compatibility
- Existing configs with `providers: { anthropic: {apiKey: ...} }` are automatically migrated on load
- Migrated accounts receive `id = string(providerType)` (e.g., "anthropic")
- Model IDs don't change if there's only 1 account of the type (full backward compatibility)
- Legacy API endpoints still work mapping over `ProviderAccounts`

### Model ID scheme with multiple accounts
```
# Case 1: Only 1 Anthropic account → backward compatible
anthropic.claude-sonnet-4-5  (same as now)

# Case 2: 2 Anthropic accounts → new prefix by account ID
work.claude-sonnet-4-5
personal.claude-sonnet-4-5

# Case 3: Custom OpenAI-compatible
my-llm.gpt-4o   (if the endpoint reports "gpt-4o")
```

### Supported provider types
| Type | Requires APIKey | Requires BaseURL | Supports custom Headers |
|------|-----------------|------------------|------------------------|
| anthropic | Yes (or OAuth) | No | No |
| openai | Yes | No | No |
| openai-compatible | Yes/Optional | Yes (required) | Yes |
| ollama | No | Optional | No |
| copilot | OAuth | No | No |
| gemini | Yes | No | No |
| groq | Yes | No | No |
| openrouter | Yes | No | No |
| xai | Yes | No | No |
| azure | Yes | Yes | No |
| bedrock | AWS credentials | No | No |
| vertexai | GCP credentials | No | No |

---

## Example resulting TOML config

```toml
[[providerAccounts]]
id = "anthropic"
type = "anthropic"
apiKey = "sk-ant-..."
displayName = "Anthropic (default)"

[[providerAccounts]]
id = "anthropic-work"
type = "anthropic"
apiKey = "sk-ant-work..."
displayName = "Anthropic Work"

[[providerAccounts]]
id = "local-llm"
type = "openai-compatible"
baseUrl = "http://localhost:1234/v1"
displayName = "Local LM Studio"

[[providerAccounts]]
id = "my-vllm"
type = "openai-compatible"
baseUrl = "http://my-server:8000/v1"
apiKey = "vllm-key"
displayName = "Production vLLM"

[providerAccounts.my-vllm.extraHeaders]
  "X-Custom-Header" = "value"
  "Authorization" = "Bearer special-token"
```