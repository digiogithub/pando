---
created_at: 2026-06-17T21:31:39.923338454Z
updated_at: 2026-06-17T21:31:39.923338454Z
tags:
    - pando
    - llmproxy
    - feature
    - bugfix
    - anthropic
    - openai-compatible
---
# Pando LLM Proxy — Model Resolution Fix + Anthropic Messages API (2026-06-17)

Package: `internal/llmproxy`. CLI: `pando llm-proxy` (default port 11434).

## Root-cause bug (listed models could not be used)

There was a model-ID mismatch between listing and using:

- `GET /v1/models` (`handlers_models.go`) previously did its own live fetch and emitted **raw** provider ids via `models.NormalizeModelID(m.ID)` (e.g. `gpt-4o`).
- The background refresh loop `app.RefreshDynamicModels` → `models.RefreshProviderModelsForAccount` registers dynamic models in `models.SupportedModels` under **prefixed** ids: `dynamicModelPrefix` uses the **provider type** for a single account (`openai.gpt-4o`) or the **accountID** when >1 account shares the type (`work.gpt-4o`). `APIModel` holds the raw upstream id (`gpt-4o`).
- `POST /v1/chat/completions` resolved with `models.SupportedModels[NormalizeModelID(req.Model)]` — only the static map. For a listed `gpt-4o` → not found → **404 "model not found"**.

## Fix

### `resolve.go` — `resolveModel(id) (resolvedModel{model, account}, ok)`
4-layer resolution, accepts prefixed ids, raw upstream ids and aliases:
1. Registry + alias via `models.NormalizeModelID` lookup in `SupportedModels`.
2. Reverse lookup by `APIModel` scoped to a configured account (resolves raw ids like `gpt-4o` to the registered `openai.gpt-4o`).
3. Strip provider/account prefix (`stripAccountPrefix`) + `synthModel` (on-the-fly model for upstream models not yet in the registry).
4. Single-account fallback: with exactly one account, treat the id as an upstream APIModel.

Helpers: `activeAccounts()` (non-disabled), `accountForModel()` (prefers `m.AccountID`, else first account of `m.Provider`), `synthModel()` (id = `provider.apiModel`, sane 128k/4096 defaults).

Wired into `handleChatCompletions` and `handleAnthropicMessages`, replacing the old static lookup + manual account search.

### `handlers_models.go` — listing now mirrors normal mode
- Serves from `models.GetAllModels()` filtered to configured providers/accounts (`collectRegistryModels`), so ids are **provider/account-prefixed and collision-free**; curated static models (Anthropic/Gemini) keep clean canonical ids.
- On-demand fallback `refreshAccountModels` (`refresh.go`) mirrors `app.RefreshDynamicModels` (account-aware, prefixed) when `registryHasModelsForProviders` is false (startup race). Avoids importing the heavy `app` package; calls `models.RefreshProviderModelsForAccount` directly. Copilot bearer via `auth.LoadGitHubOAuthToken`/`auth.LoadCopilotSession`.
- Output sorted by id, deduplicated. Static fallback if registry still empty.
- `handleGetModel` resolves via `resolveModel`.

### Tool / function calling passthrough
`proxyTool` implements `tools.BaseTool` — `Info()` advertises the schema; `Run()` is a no-op error because the proxy **relays** the model's `tool_calls` to the client and never executes tools. `schemaToProxyTool(name, desc, schema)` splits JSON-Schema into `properties` (→ `ToolInfo.Parameters`) + `required` (the providers wrap these as `{type:object, properties, required}`). Request `tools`→provider; response `tool_calls` returned. Streaming emits the **full** tool calls at `EventComplete` (NOT `EventToolUseStart`, which only carries id/name — input streams later via `EventToolUseDelta`).

## New endpoint: `POST /v1/messages` (Anthropic Messages API)
Files: `handlers_messages.go` + `convert_messages.go`. Works across **any** configured provider (not just Anthropic): converts the Anthropic request to internal `message.Message`, routes through the resolved provider, converts the response back to Anthropic shape.

- Request: `model`, `messages`, `system` (string or text-block array, flattened), `max_tokens`, `temperature`, `stream`, `tools` (`input_schema`).
- Content blocks supported on input: `text`, `image` (base64 `data:` or url), `tool_use` (assistant), `tool_result` (in user turns → split into internal **Tool-role** messages, which must precede the user's follow-up text).
- Non-streaming response: `{id, type:message, role:assistant, model, content:[text|tool_use], stop_reason, stop_sequence, usage}`. `stop_reason` via `anthropicStopReason`.
- Streaming SSE protocol: `message_start` → `content_block_start/content_block_delta(text_delta)/content_block_stop` for text → per tool_use a `content_block_start(tool_use)` + `content_block_delta(input_json_delta)` + `content_block_stop` → `message_delta(stop_reason,usage)` → `message_stop`. Errors emit an `error` event.
- `toolInputJSON` normalizes tool-call args to a JSON object (`{}` if empty/invalid).

## Auth / CORS compatibility
`authMiddleware` accepts both `Authorization: Bearer <key>` (OpenAI) and `x-api-key: <key>` (Anthropic). CORS `Access-Control-Allow-Headers` includes `x-api-key`, `anthropic-version`, `anthropic-beta`.

## Routes (`routes.go`)
`GET /health`, `GET /v1/`, `GET /v1/models`, `GET /v1/models/{id}`, `POST /v1/chat/completions`, `POST /v1/messages`, `POST /v1/embeddings`. `GET /v1/` advertises `openai_compatible` + `anthropic_compatible` + endpoint list.

## Verification
Tests in `internal/llmproxy/resolve_test.go`. `go build ./...`, `gofmt`, `go vet` clean; `go test ./internal/llm/agent ./internal/api ./internal/llmproxy` pass.

## Key relationships
- Registry population: `app.StartModelRefreshLoop` (called from `cmd/llm_proxy.go`) → `RefreshDynamicModels` → `RefreshProviderModelsForAccount` (prefixed ids). The proxy reuses this; `refreshAccountModels` is just an on-demand mirror.
- Provider creation: `provider.NewProviderFromAccount(account, model, maxTokens, systemMsg)` — the canonical path also used by the agent (`internal/llm/agent/agent.go`), so Copilot OAuth etc. work unchanged.