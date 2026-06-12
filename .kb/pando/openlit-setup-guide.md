# Setting up OpenLit with Pando

OpenLit adds observability to all Pando LLM calls: traces per conversation, tokens consumed, tool calls, latency and provider. It is **optional** — if not configured, it has no impact on performance.

## What is captured?

For each call to an LLM provider (Anthropic, OpenAI, Copilot, Gemini, Ollama, etc.), an **OpenTelemetry trace** is generated with:

- `gen_ai.system` — provider (anthropic, openai, gemini…)
- `gen_ai.operation.name` — always `chat`
- `gen_ai.request.model` — exact model used
- `gen_ai.request.max_tokens` — configured token limit
- `gen_ai.request.message_count` — number of messages in context
- `gen_ai.request.tool_count` — number of available tools
- `gen_ai.usage.input_tokens` — input tokens consumed
- `gen_ai.usage.output_tokens` — output tokens generated
- `gen_ai.response.finish_reasons` — finish reason (stop, tool_use, max_tokens…)
- `gen_ai.usage.cache_read_input_tokens` — tokens read from cache (Anthropic)
- `gen_ai.usage.cache_creation_input_tokens` — tokens written to cache (Anthropic)
- `gen_ai.tool.call` events per tool call with `gen_ai.tool.name` and `gen_ai.tool.call.id`

Data is sent via **OTLP HTTP** to the OpenLit server.

---

## Starting OpenLit (Docker)

```bash
docker run -d \
  --name openlit \
  -p 3000:3000 \
  -p 4318:4318 \
  -e INIT_DB_HOST=localhost \
  ghcr.io/openlit/openlit:latest
```

- Port **3000** → web dashboard (http://localhost:3000)
- Port **4318** → OTLP HTTP receiver (where Pando points)

Default credentials: `user@openlit.io` / `openlituser@1`

With Docker Compose:

```yaml
services:
  openlit:
    image: ghcr.io/openlit/openlit:latest
    ports:
      - "3000:3000"
      - "4318:4318"
    environment:
      INIT_DB_HOST: localhost
```

---

## Configuring Pando

### Option 1 — `.pando.toml` file

Add or edit the `[OpenLit]` section in your configuration file:

```toml
[OpenLit]
Enabled = true
Endpoint = "http://localhost:4318"
ServiceName = "pando"
Insecure = true
```

For a remote OpenLit server with HTTPS and authentication:

```toml
[OpenLit]
Enabled = true
Endpoint = "https://openlit.my-company.com"
ServiceName = "pando-production"
Insecure = false

[OpenLit.CustomHeaders]
Authorization = "Bearer my-api-key"
```

### Option 2 — REST API (Web UI / programmatic)

```bash
# View current configuration
curl http://localhost:8765/api/v1/config/openlit \
  -H "X-Pando-Token: YOUR_TOKEN"

# Enable OpenLit
curl -X POST http://localhost:8765/api/v1/config/openlit \
  -H "X-Pando-Token: YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "endpoint": "http://localhost:4318",
    "serviceName": "pando",
    "insecure": true
  }'
```

### Option 3 — TUI configuration panel

In the Pando TUI, go to **Settings** (key `s` or from the side menu) and look for the **"OpenLit Observability"** section. You can enable/disable and change the endpoint from there without restarting.

---

## Configuration parameters

| Field | Type | Default | Description |
|-------|------|-------------|-------------|
| `Enabled` | bool | `false` | Enables/disables observability |
| `Endpoint` | string | `http://localhost:4318` | OpenLit server base URL (OTLP HTTP) |
| `ServiceName` | string | `pando` | Service name in traces |
| `Insecure` | bool | `true` | Skip TLS verification (useful locally) |
| `CustomHeaders` | map | `{}` | Additional HTTP headers (authentication, etc.) |

---

## How it works internally

```
Pando (LLM call)
  └── instrumentedProvider (OTel wrapper)
        ├── Creates span with GenAI attributes
        ├── Calls real provider (Anthropic, OpenAI, etc.)
        ├── Adds usage tokens and tool calls to span
        └── Sends trace via OTLP HTTP → OpenLit :4318
```

The wrapper is a **decorator** on the `Provider` interface. If `Enabled = false`, the wrapper is not applied and there is no overhead.

Initialization happens at app startup (`internal/app/app.go`). The OTLP exporter flushes pending traces when Pando shuts down (shutdown with 5s timeout).

---

## Verifying it works

1. Open the OpenLit dashboard: http://localhost:3000
2. Start a conversation in Pando
3. In OpenLit you will see traces appear with operation name `chat {model}` (e.g., `chat claude-sonnet-4-6`)

If no traces appear, check:
- That `Enabled = true` in config
- That the OpenLit server is accessible from Pando: `curl http://localhost:4318/v1/traces`
- Pando logs at startup — if there is an OTLP connection error, a warning will appear

---

## Connecting with other OTLP backends

OpenLit is a standard OTLP backend, but you can send Pando traces to any compatible backend:

| Backend | Endpoint |
|---------|----------|
| Local OpenLit | `http://localhost:4318` |
| Jaeger | `http://localhost:4318` (with OTLP receiver enabled) |
| Grafana Tempo | `http://localhost:4318` |
| New Relic | `https://otlp.nr-data.net:4318` (with API key in CustomHeaders) |
| Honeycomb | `https://api.honeycomb.io` (with API key in CustomHeaders) |

For New Relic:
```toml
[OpenLit]
Enabled = true
Endpoint = "https://otlp.nr-data.net:4318"
ServiceName = "pando"
Insecure = false

[OpenLit.CustomHeaders]
"api-key" = "YOUR_NEW_RELIC_LICENSE_KEY"
```

---

## Technical reference

- Implementation: `internal/observability/observability.go`, `internal/observability/genai.go`
- Provider wrapper: `internal/llm/provider/instrumented.go`
- Config struct: `internal/config/config.go` → `OpenLitConfig`
- API endpoint: `GET/POST /api/v1/config/openlit`
- Protocol: OTLP HTTP (`/v1/traces`), compatible with OpenTelemetry Collector
- Semconv: OpenTelemetry GenAI Semantic Conventions v1.27