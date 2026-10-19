# llm-proxy: list models of keyless OpenAI-compatible accounts, lazily per account

## Problem
`/v1/models` of `pando llm-proxy` listed only `gemini.*` when a keyless
`[[providerAccounts]] type='openai-compatible'` (a private Ollama gateway) was
configured beside an environment Gemini key.

## Root causes
1. `config.Load` validation marked any key-requiring account without a key as
   `Disabled` in memory; `providerAccountRequiresAPIKey` / `providerRequiresAPIKey`
   treated openai-compatible as key-requiring, so the account was never active.
2. Both refresh paths (`app.RefreshDynamicModels`, `llmproxy.refreshAccountModels`)
   skipped listing-capable accounts without a key, except Ollama/llama.cpp.
3. `/v1/models` refreshed on demand only when the registry had no model for ANY
   configured provider, so a populated static provider hid a missing account.

## Change
- `config.go`: openai-compatible no longer requires an API key (base URL identifies it).
- `models.ProviderListsModelsWithoutAPIKey` (Ollama, llama.cpp, openai-compatible), used by
  `app.RefreshDynamicModels` (also per-account 20 s timeout so one dead account does not
  delay the others) and `llmproxy/refresh.go`.
- `llmproxy/refresh.go`: `refreshMissingAccountModels` fetches concurrently, 5 s per account,
  only for accounts with no registry model (own AccountID or a static model of the type),
  at most once per 30 s per account; `handlers_models.go` uses it instead of the
  all-providers-empty check. Model ids unchanged: prefix is the provider type for one
  account of a type, the account id when several share it.

## Verification
`TestListModelsFetchesKeylessAccountBesideStaticProvider` (httptest fake, keyless account,
an unreachable account, Gemini static models present). go vet; go test on llmproxy, config,
app, llm/models, cmd. Smoke test with the rebuilt binary against a local stub.
