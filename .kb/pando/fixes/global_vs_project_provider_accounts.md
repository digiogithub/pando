---
created_at: 2026-06-18T12:59:29.497955013Z
updated_at: 2026-06-18T12:59:29.497955013Z
tags:
    - fix
    - config
    - models
    - provider-accounts
    - multi-account
    - tui
    - webui
---
# Fix: global provider account hidden by project account; same-type accounts clobber models

Date: 2026-06-18

## Symptom
With a global (profile-level) Anthropic account AND a project-level Anthropic
account: the config UI showed only the project account (global invisible); the
model selector showed the GLOBAL account's models (which couldn't be removed or
seen) and NOT the project account's models. Happened in both TUI and web-UI.

## Root causes

### 1. Viper slice merge replaces, not appends
`mergeLocalConfig` did `viper.MergeConfigMap(local.AllSettings())`. Viper deep-merges
maps but OVERWRITES slices, so a project `providerAccounts` array replaced the global
one entirely. `GetProviderAccounts()` therefore returned only the project account.

### 2. Provider-scoped (not account-scoped) model pruning
`RefreshProviderModelsForAccount` ended with `pruneDynamicModelsForProvider(type, keepIDs)`,
which deletes every dynamic model of that provider type not in keepIDs. With two
accounts of the same type, refreshing account A pruned account B's models and vice
versa, so only the last-refreshed account's models survived in the registry/cache.

## Changes

### internal/config/config.go
- `mergeLocalConfig` now captures `providerAccounts` from both the global and local
  scopes BEFORE the viper merge and re-merges them by ID via new helpers
  `toProviderAccountMaps`, `providerAccountMapID`, `mergeProviderAccountMaps`.
  Local (project) accounts come first (precedence for `ResolveProviderAccountForType`
  first-of-type); global accounts with an ID not redefined locally are appended.
- Safe for persistence: `updateCfgFile` rewrites the edited file from its own on-disk
  contents, not from the merged in-memory view, so global accounts never leak into
  the project file.

### internal/llm/models/registry.go
- New `pruneDynamicModelsForAccount(provider, accountID, keepIDs)` scoped by AccountID.
- `RefreshProviderModelsForAccount` calls it instead of the provider-wide prune, so
  refreshing one account no longer wipes another same-type account's models. Also
  clears stale entries when an account transitions prefixed<->unprefixed IDs.

### Display disambiguation (the previously-unused `Model.DisplayLabel`)
- Web-UI `/api/v1/models` already prefixed `acc.ID: name` when sameTypeCount > 1.
- TUI `internal/tui/components/dialog/models.go`: new `sameTypeAccountCount()` (distinct
  AccountIDs among the provider's models); render + fuzzy-filter now use
  `Model.DisplayLabel(count)` so two same-type accounts are distinguishable.

## Follow-up (2026-06-18, second pass): "model selector still only uses the global account"

After the merge fix, selecting any model still hit the global account, the label
showed the ID slug (not the Display Name), and the ID field accepted spaces. Four
remaining root causes + fixes:

1. **Account-less static models linger.** Anthropic/Gemini/etc. are hardcoded with
   `AccountID == ""`; with 2+ accounts they sit next to the per-account entries and
   `ResolveProviderAccountForType` sends them to the *first* account.
   → TUI `dropAmbiguousStaticModels` (`dialog/models.go`) hides `AccountID == ""`
   models for a provider once ≥2 accounts contribute models. Web-UI already emits
   only per-account models, so the change there is automatic once OAuth fetch works.
2. **OAuth-only Anthropic accounts registered no models.** `RefreshDynamicModels`
   (`app.go`) and `handleListModels` (`handlers_models.go`) skipped any account with
   `APIKey == ""`. → both now fall back to the Claude.ai OAuth bearer via new
   `auth.LoadClaudeBearerToken()` (reuses `GetClaudeToken` refresh+persist).
3. **Per-account models lost metadata.** `registry.modelFromFetchedAccountModel` now
   inherits a matching static model's metadata (CanReason/costs/context/name) via new
   `staticModelByAPIModel`, overriding only ID/Provider/APIModel/AccountID — so hiding
   the statics keeps thinking mode + costs.
4. **Label showed ID slug, ID allowed spaces.** API label now uses `acc.DisplayName`
   (fallback ID). TUI `modelDialogCmp.accountLabel` + `accountDisplayNames` map resolve
   the Display Name (models pkg can't import config). ID sanitized live as typed:
   TUI `sanitizeAccountID` in `add_provider.go`, Web-UI `sanitizeAccountId` in
   `ProviderAccountsSettings.tsx`.

## Generalised to all static-catalog providers (2026-06-18, third pass)

The second-pass logic covered Anthropic/Gemini (static **and** listing-capable). Providers
with a **static catalog but no listing API** — Azure (API-key) and Vertex AI (OAuth) — never
register per-account models, so multi-account selection still fell back to the first account.
(Bedrock has no static models in `SupportedModels`; Antigravity is intentionally account-less
and resolved by its scheduler.)

Fix (provider-agnostic):
- `models.ProviderSupportsModelListing(provider)` (fetcher.go) — single source of truth for
  which providers have a listing API.
- `models.AccountScopedStaticModels(provider, accountID, allAccountsOfType)` +
  `StaticModelsForProvider` (registry.go) — synthesize account-prefixed, `AccountID`-bound
  copies of a provider's static models (empty when ≤1 account, so single-account keeps the
  account-less statics).
- `RefreshProviderModelsForAccount`: when the provider has no listing API, register those
  account-scoped static copies (account-scoped prune) instead of erroring on the fetch.
- `app.RefreshDynamicModels`: stop skipping no-API-key accounts whose provider has no listing
  API (e.g. Vertex AI/OAuth) — static copies need no credentials.
- `handleListModels` (`staticModelInfosForAccount`): emit per-account static `ModelInfo`
  (Display-Name labelled) for non-listing providers instead of recording a fetch error.
- The TUI `dropAmbiguousStaticModels` + `accountLabel` are already provider-agnostic, so Azure/
  Vertex multi-account now hide the account-less statics and label by Display Name automatically.
- Tests: `internal/llm/models/registry_static_account_test.go`.

## Tests
- `internal/config/provider_account_merge_test.go`:
  `TestLoadMergesGlobalAndProjectProviderAccounts`,
  `TestLoadProjectAccountOverridesGlobalByID`, `TestMergeProviderAccountMaps`.
- `internal/llm/models/registry_prune_test.go`:
  `TestPruneDynamicModelsForAccountIsAccountScoped`.
- `internal/llm/models/registry_enrich_test.go`:
  `TestModelFromFetchedAccountModelInheritsStaticMetadata`,
  `TestModelFromFetchedAccountModelFallsBackWithoutStatic`.
- `internal/tui/components/dialog/models_account_test.go`:
  `TestDropAmbiguousStaticModels`, `TestAccountLabelUsesDisplayName`.
