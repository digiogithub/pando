# pando llm-proxy — Implementation Plan

## Overview

`pando llm-proxy` is a new CLI mode that starts an OpenAI-compatible HTTP proxy server.
It routes requests to all configured Pando providers, exposes a unified model listing endpoint,
supports chat completions (streaming + non-streaming), embeddings, and optionally injects web
search (google → exa → brave fallback) when the selected provider doesn't support it natively.

## CLI Usage

```
pando llm-proxy [--host localhost] [--port 11434] [--debug] [--api-key <key>]
```

Flags:
- `--host`     Bind address (default: localhost)
- `--port`     Listen port (default: 11434 — same convention as Ollama)
- `--debug`    Enable debug logging
- `--api-key`  Optional: require Bearer token for proxy authentication

## Package Layout

```
cmd/llm_proxy.go                    — cobra command
internal/llmproxy/
  server.go                         — LLMProxyServer, Start/Shutdown
  config.go                         — ProxyConfig struct
  routes.go                         — registerRoutes (OpenAI paths)
  handlers_models.go                — GET /v1/models, GET /v1/models/{id}
  handlers_chat.go                  — POST /v1/chat/completions
  handlers_embeddings.go            — POST /v1/embeddings
  handlers_health.go                — GET /health, GET /v1/
  convert.go                        — OpenAI ↔ internal message conversion
  search.go                         — web search fallback chain integration
  middleware.go                     — CORS, optional Bearer auth
```

## OpenAI-Compatible Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET    | /health | Proxy health check |
| GET    | /v1/   | Version info |
| GET    | /v1/models | List all models from all connected providers |
| GET    | /v1/models/{id} | Get specific model metadata |
| POST   | /v1/chat/completions | Chat completions (stream and non-stream) |
| POST   | /v1/embeddings | Text embeddings |

## Implementation Phases

### Phase 1 — CLI Command + Server Scaffold
**Files:** `cmd/llm_proxy.go`, `internal/llmproxy/server.go`, `internal/llmproxy/config.go`, `internal/llmproxy/middleware.go`, `internal/llmproxy/routes.go`

Tasks:
1. Create `cmd/llm_proxy.go` with cobra command `llm-proxy`
   - Flags: host, port, debug, api-key
   - Load config, connect DB (for settings), setup context + signal handling
   - Same shutdown pattern as `cmd/serve.go`
2. `internal/llmproxy/server.go`: `LLMProxyServer` with `Start()`, `Shutdown(ctx)`
3. `internal/llmproxy/config.go`: `ProxyConfig{Host, Port, APIKey, Debug}`
4. `internal/llmproxy/middleware.go`: CORS + optional Bearer auth middleware
5. `internal/llmproxy/routes.go`: stub routes registered in mux
6. Register command in `cmd/root.go` init() → `rootCmd.AddCommand(llmProxyCmd)`

### Phase 2 — OpenAI Models API
**Files:** `internal/llmproxy/handlers_models.go`

Tasks:
1. `GET /v1/models` — fetch models from all non-disabled provider accounts in parallel
   - Reuse `models.FetchModelsFromProvider()` and `config.GetProviderAccounts()`
   - Return OpenAI `ListModelsResponse{Object:"list", Data:[]ModelObject}`
   - Each model: `{id, object:"model", created, owned_by}`
   - Fall back to static `models.SupportedModels` if dynamic fetch fails
2. `GET /v1/models/{id}` — find model by ID, return single `ModelObject` or 404

OpenAI model object format:
```json
{
  "id": "anthropic.claude-sonnet-4-6",
  "object": "model",
  "created": 1715000000,
  "owned_by": "anthropic"
}
```

### Phase 3 — Chat Completions API
**Files:** `internal/llmproxy/handlers_chat.go`, `internal/llmproxy/convert.go`

Tasks:
1. Parse `OpenAIChatRequest`:
   ```go
   type OpenAIChatRequest struct {
     Model    string               `json:"model"`
     Messages []OpenAIChatMessage  `json:"messages"`
     Stream   bool                 `json:"stream"`
     MaxTokens *int64              `json:"max_tokens,omitempty"`
     Temperature *float64          `json:"temperature,omitempty"`
     Tools    []OpenAITool         `json:"tools,omitempty"`
   }
   ```
2. `convert.go`: OpenAI messages → `[]message.Message` (internal format)
   - role: system → message.System, user → message.User, assistant → message.Assistant
   - tool_call / tool content parts mapped correctly
3. Resolve provider from model ID:
   - Find model in `models.SupportedModels` or dynamic registry
   - Find matching provider account via `config.GetProviderAccounts()`
   - Call `provider.NewProviderFromAccount(account, model, maxTokens, systemMsg)`
4. Non-streaming: call `provider.SendMessages()` → convert to OpenAI `ChatCompletionResponse`
5. Streaming: call `provider.StreamResponse()` → write SSE events
   - `data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."}}]}`
   - Final: `data: [DONE]`
6. Error handling: 400 for missing model, 404 for unknown model, 500 for provider error

### Phase 4 — Embeddings API
**Files:** `internal/llmproxy/handlers_embeddings.go`

Tasks:
1. Parse `OpenAIEmbeddingRequest{Model, Input string|[]string, EncodingFormat}`
2. Route to OpenAI-compatible provider (openai, azure, ollama, llama-cpp, openrouter)
3. Make direct HTTP call to provider's `/v1/embeddings` endpoint with passthrough
   - For openai/azure/ollama/openrouter: forward request with proper auth headers
   - For unsupported providers: return 422 with descriptive error
4. Return `OpenAIEmbeddingResponse` in standard format

### Phase 5 — Web Search Integration
**Files:** `internal/llmproxy/search.go`

Tasks:
1. Check config for web search availability:
   - `cfg.InternalTools.GoogleSearchEnabled && cfg.InternalTools.GoogleAPIKey != ""`
   - `cfg.InternalTools.ExaSearchEnabled && cfg.InternalTools.ExaAPIKey != ""`
   - `cfg.InternalTools.BraveSearchEnabled && cfg.InternalTools.BraveAPIKey != ""`
2. If request contains `tools` with `type:"web_search"` or special `web_search` model flag:
   - Inject web search as a tool-call simulation layer
   - Fallback chain: try Google first → Exa if Google fails/unconfigured → Brave as last resort
3. Implement `SearchFallback(ctx, query string) ([]SearchResult, error)`:
   - Reuse existing `tools.NewGoogleSearchTool`, `tools.NewExaSearchTool`, `tools.NewBraveSearchTool`
   - Inject results into the conversation as tool-result messages before re-calling provider
4. When provider supports tool-calling natively (openai, anthropic): pass search as native tool
5. When provider doesn't support tools: append search results inline in user message

### Phase 6 — Auth, Health, Instance Registry
**Files:** `internal/llmproxy/handlers_health.go`, updates to `server.go`

Tasks:
1. `GET /health` → `{"status":"ok","version":"...","providers":N,"models":M}`
2. `GET /v1/` → `{"name":"pando-llm-proxy","version":"...","openai_compatible":true}`
3. Bearer auth middleware (when --api-key is set):
   - Check `Authorization: Bearer <key>` header
   - Return 401 if missing/wrong (skip for health endpoint)
4. Register proxy instance in `instanceregistry`:
   - Add `ModeProxy = "proxy"` to `instanceregistry` modes
   - Announce on start, Revoke on shutdown
5. Graceful shutdown: SIGINT/SIGTERM → `server.Shutdown(5s ctx)`
6. Write memory entry for this plan

## Key Internal Dependencies

- `internal/config` — `config.Get()`, `config.GetProviderAccounts()`, `config.Load()`
- `internal/llm/models` — `models.SupportedModels`, `models.FetchModelsFromProvider()`
- `internal/llm/provider` — `provider.NewProviderFromAccount()`, `provider.ProviderEvent`
- `internal/message` — message conversion types
- `internal/instanceregistry` — announce/revoke
- `internal/logging` — structured logging
- `internal/version` — version string
- `internal/llm/tools` — `NewGoogleSearchTool`, `NewExaSearchTool`, `NewBraveSearchTool`

## Testing Strategy

- Unit tests in `internal/llmproxy/` for request/response conversion
- Integration test: start proxy, call `/v1/models`, verify response shape
- Mock provider for chat completions test
