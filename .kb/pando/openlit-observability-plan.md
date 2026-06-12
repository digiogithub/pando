# Implementation Plan: OpenLit Observability in Pando

## Project
- **Repo**: `/www/MCP/Pando/pando` (module: `github.com/digiogithub/pando`)
- **Go**: 1.26
- **Already has**: `go.opentelemetry.io/otel v1.35.0` in go.mod

## Objective
Add optional observability to all LLM calls (messages, tool calls, tokens, sessions) by sending them to an OpenLit server via OTLP. The integration is **optional**: it only activates if `OpenLit.Enabled = true` in the configuration.

## Technology
OpenLit uses standard OpenTelemetry (OTLP). There is no official Go SDK for OpenLit, but since the project already has OTel, we implement:
- **Traces**: Spans for each LLM call with GenAI semantic conventions
- **OTLP Exporter**: HTTP (`/v1/traces`) or gRPC to the OpenLit endpoint
- **Semantic conventions**: `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, etc.

---

## PHASE 1: Configuration (Foundation of everything)

**Files to modify**:
- `/www/MCP/Pando/pando/internal/config/config.go` — add `OpenLitConfig` struct and field in `Config`
- `/www/MCP/Pando/pando/.pando.toml` — add example `[OpenLit]` section

**Struct to add in config.go**:
```go
type OpenLitConfig struct {
    Enabled         bool   `json:"enabled" toml:"Enabled"`
    Endpoint        string `json:"endpoint" toml:"Endpoint"`         // e.g. "http://localhost:4318"
    ServiceName     string `json:"serviceName" toml:"ServiceName"`   // e.g. "pando"
    Insecure        bool   `json:"insecure" toml:"Insecure"`         // skip TLS verify
    CustomHeaders   map[string]string `json:"customHeaders" toml:"CustomHeaders"` // auth headers
}
```

**In Config struct** (already exists in config.go, ~413 lines), add:
```go
OpenLit OpenLitConfig `json:"openlit,omitempty" toml:"OpenLit"`
```

**In .pando.toml** add section:
```toml
[OpenLit]
Enabled = false
Endpoint = "http://localhost:4318"
ServiceName = "pando"
Insecure = true
```

**Defaults to add in the defaults function** (search for `setDefaults` or similar in config.go):
- `Endpoint`: `"http://localhost:4318"`
- `ServiceName`: `"pando"`
- `Insecure`: `true`

---

## PHASE 2: Observability Package (depends on Phase 1)

**Create**: `/www/MCP/Pando/pando/internal/observability/`

**Files to create**:
- `observability.go` — TracerProvider initialization with OTLP exporter
- `genai.go` — helpers for GenAI semantic conventions
- `noop.go` — noop implementation when OpenLit is disabled

**New dependencies to add in go.mod**:
```
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/sdk/trace
```
(check if they are already present as indirect)

**Package API**:
```go
// Initialize the global TracerProvider with OTLP exporter
func Init(cfg config.OpenLitConfig, version string) (shutdown func(context.Context) error, err error)

// Return a tracer for instrumenting LLM calls
func Tracer() trace.Tracer

// GenAI semantic convention constants (OTel)
const (
    AttrGenAISystem          = "gen_ai.system"
    AttrGenAIRequestModel    = "gen_ai.request.model"
    AttrGenAIRequestMaxTokens = "gen_ai.request.max_tokens"
    AttrGenAIResponseModel   = "gen_ai.response.model"
    AttrGenAIUsageInputTokens = "gen_ai.usage.input_tokens"
    AttrGenAIUsageOutputTokens = "gen_ai.usage.output_tokens"
    AttrGenAIOperationName   = "gen_ai.operation.name"
    AttrGenAIFinishReasons   = "gen_ai.response.finish_reasons"
    // ...
)
```

**Init logic**:
1. If `!cfg.Enabled`, register a global NoopTracerProvider and return noop shutdown
2. Create OTLP HTTP exporter pointing to `cfg.Endpoint + "/v1/traces"`
3. Configure `resource` with `service.name = cfg.ServiceName`, `service.version`
4. Create `TracerProvider` with BatchSpanProcessor
5. Register as global: `otel.SetTracerProvider(tp)`
6. Return shutdown function

---

## PHASE 3: Provider Instrumentation (depends on Phase 2)

**Create**: `/www/MCP/Pando/pando/internal/llm/provider/instrumented.go`

**Pattern**: Decorator/Wrapper over the existing `Provider` interface:

```go
// Current Provider interface (provider.go):
type Provider interface {
    SendMessages(ctx, messages, tools) (*ProviderResponse, error)
    StreamResponse(ctx, messages, tools) <-chan ProviderEvent
    Model() models.Model
}

// New instrumented wrapper:
type instrumentedProvider struct {
    inner  Provider
    tracer trace.Tracer
}

func NewInstrumentedProvider(inner Provider) Provider {
    if !observability.IsEnabled() {
        return inner // no overhead if OpenLit is disabled
    }
    return &instrumentedProvider{inner: inner, tracer: observability.Tracer()}
}
```

**SendMessages instrumentation**:
- Create span: `"chat {model}"` (gen_ai.operation.name = "chat")
- Span attributes:
  - `gen_ai.system` = provider name (anthropic, openai, gemini, etc.)
  - `gen_ai.request.model` = model API name
  - `gen_ai.request.max_tokens` = maxTokens
  - `gen_ai.request.message_count` = len(messages)
  - `gen_ai.request.tool_count` = len(tools)
- On successful response:
  - `gen_ai.usage.input_tokens`
  - `gen_ai.usage.output_tokens`
  - `gen_ai.response.finish_reasons`
  - `gen_ai.response.tool_calls_count`
- On error: `span.RecordError(err)`, `span.SetStatus(codes.Error, ...)`

**StreamResponse instrumentation**:
- Create span at stream start
- Accumulate channel events
- On receiving EventComplete with ProviderResponse: add usage attributes
- On receiving EventError: RecordError
- Close span when channel closes

**Integration in NewProvider** (provider.go):
```go
func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
    // ... existing code ...
    p, err := createBaseProvider(providerName, clientOptions)
    if err != nil {
        return nil, err
    }
    return NewInstrumentedProvider(p), nil  // ← add this line
}
```

**Tool calls as span events**:
```go
// When EventToolUseStart is received in the stream:
span.AddEvent("gen_ai.tool.call", trace.WithAttributes(
    attribute.String("gen_ai.tool.name", toolCall.Name),
    attribute.String("gen_ai.tool.call.id", toolCall.ID),
))
```

---

## PHASE 4a: TUI Settings (depends on Phase 1, parallel with 4b)

**Files to modify**:
- `/www/MCP/Pando/pando/internal/tui/components/settings/settings.go`

**Add "OpenLit" section to the TUI settings** with the fields:
- `Enabled` (checkbox/boolean)
- `Endpoint` (text, default: `http://localhost:4318`)
- `ServiceName` (text, default: `pando`)
- `Insecure` (checkbox/boolean)

**How to do it**: In settings.go there is a function that builds sections. Add a new `Section` with `Title: "OpenLit Observability"` and the corresponding `Field`s.

Look at the pattern of other sections like "Remembrances" or "Server" to follow the same style.

The `SaveFieldMsg` handler in `page/settings.go` (already exists) handles persisting the modified field to config. Make sure to map the field keys to the correct Config paths.

---

## PHASE 4b: Web UI API (depends on Phase 1, parallel with 4a)

**Files to modify**:
- `/www/MCP/Pando/pando/internal/api/routes.go` — verify if there is already an endpoint for services/observability
- `/www/MCP/Pando/pando/internal/api/handlers_config.go` (or similar) — add support for `openlit` in config handlers

**Objective**: Have the `/api/v1/config/services` endpoints (or whichever is appropriate) return and accept the OpenLit config.

Search for how other similar services (Remembrances, Mesnada) are implemented in the handlers to follow the same pattern.

If there is a React frontend (`ui/web/src/`), add the OpenLit fields to the corresponding configuration panel.

---

## PHASE 5: App Integration (depends on Phases 2, 3, 4a, 4b)

**Files to modify**:
- `/www/MCP/Pando/pando/internal/app/app.go` — initialize observability at startup
- `/www/MCP/Pando/pando/main.go` — manage OTLP exporter shutdown

**In app.go** (`New` or `Init` function):
```go
import "github.com/digiogithub/pando/internal/observability"

// After loading config:
cfg := config.Get()
if cfg.OpenLit.Enabled {
    shutdown, err := observability.Init(cfg.OpenLit, version.Version)
    if err != nil {
        logging.Warn("OpenLit observability init failed", "error", err)
    } else {
        app.openlitShutdown = shutdown
    }
}
```

**Graceful shutdown** (in app cleanup or main.go):
```go
if app.openlitShutdown != nil {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = app.openlitShutdown(ctx)
}
```

---

## Dependency Graph

```
Phase 1 (Config)
    ├── Phase 2 (Observability pkg)
    │       └── Phase 3 (Provider instrumentation)
    │                   └── Phase 5 (App integration) ←──┐
    ├── Phase 4a (TUI Settings) ──────────────────────────┤
    └── Phase 4b (Web UI/API) ───────────────────────────┘
```

**Parallelization**:
- Phase 1 → launch Phase 2, 4a and 4b in PARALLEL
- Phase 2 complete → launch Phase 3
- Phase 3 + 4a + 4b complete → launch Phase 5

---

## Implementation Notes

1. **There is no Go SDK for OpenLit**: We use OTLP directly with GenAI semantic conventions (OTel Semconv v1.27+)
2. **Minimal overhead**: If `Enabled=false`, the wrapper returns the original provider without wrapping
3. **The project ALREADY HAS OTel**: `go.opentelemetry.io/otel v1.35.0` — just add the SDK and the HTTP OTLP exporter
4. **Config hot-reload**: The config EventBus already exists; if OpenLit is enabled at runtime, the TracerProvider can be reinitialized
5. **Compatibility**: OpenLit accepts standard OTLP on port 4318 (HTTP) or 4317 (gRPC)