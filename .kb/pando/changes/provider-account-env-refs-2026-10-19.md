---
tags:
    - config
    - provider-accounts
    - llm-proxy
---
# Env references in provider account secrets; env providers kept beside accounts

## Date
2026-10-19

## What changed
1. `ProviderAccount.APIKey` and every `ExtraHeaders` value may be `env:NAME`
   (`NAME` = `[A-Za-z_][A-Za-z0-9_]*`). The reference is resolved from the
   process environment at use (`ResolveSecretRef`, `ProviderAccount.Resolved`,
   `GetResolvedProviderAccounts`; `ResolveProviderAccountByID/ForType` and
   `AccountsForProviderType` return resolved copies). The stored account keeps
   the reference: saves, the REST API and the age encryption layer leave it
   untouched (references are not encrypted and not masked). An unset variable
   resolves to empty and logs one warning naming the variable.
2. `mergeEnvProvidersIntoAccounts` (run on load after migration/decryption):
   when `providerAccounts` exist, legacy/environment providers with credentials
   (`providerEntryHasCredentials`) whose type has no account get an in-memory
   account with ID = type. Never duplicates a type, never written to the file.
   `syncProvidersFromAccounts` now stores the resolved key.
3. llm-proxy `/v1/embeddings`: the target account and upstream model now come
   from `resolveModel` (same as chat), the request `model` is rewritten to the
   bare upstream name (no `<account>.`/`<type>.` prefix), account `extraHeaders`
   are forwarded, and the fallback skips accounts whose type has no embeddings.
   The proxy, dynamic-model refresh and `/api` model listing use resolved
   accounts.

## Files and symbols
- `internal/config/secret_ref.go` (new): `IsSecretRef`, `ResolveSecretRef`,
  `ProviderAccount.Resolved`, `GetResolvedProviderAccounts`,
  `mergeEnvProvidersIntoAccounts`
- `internal/config/config.go`: `ProviderAccount` docs, `Load`,
  `syncProvidersFromAccounts`, account validation, `Resolve*`
- `internal/config/agecrypto.go`: `encryptSecretString` skips references
- `internal/api/handlers_provider_accounts.go`: `providerAccountToResponse`
- `internal/api/handlers_models.go`, `internal/app/app.go`,
  `internal/llmproxy/resolve.go`: use resolved accounts
- `internal/llmproxy/handlers_embeddings.go`: `resolveEmbeddingsTarget`,
  `rewriteEmbeddingsModel`, `providerSupportsEmbeddings`
- `docs/configuration.md`: "Provider accounts" section

## Verification
`go vet ./...`; `go test ./internal/config/... ./internal/llmproxy/...
./internal/llm/...`. New tests: `internal/config/provider_account_env_refs_test.go`
(resolution set/unset, no persisted secret, env provider kept),
`internal/llmproxy/openai_compatible_account_test.go` (httptest fake: model
listing, chat and embeddings routed to the account baseUrl with bare model).
