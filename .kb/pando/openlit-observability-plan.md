# Plan de Implementación: Observabilidad OpenLit en Pando

## Proyecto
- **Repo**: `/www/MCP/Pando/pando` (módulo: `github.com/digiogithub/pando`)
- **Go**: 1.26
- **Ya tiene**: `go.opentelemetry.io/otel v1.35.0` en go.mod

## Objetivo
Añadir observabilidad opcional a todas las llamadas LLM (mensajes, tool calls, tokens, sesiones) enviándolas a un servidor OpenLit vía OTLP. La integración es **opcional**: solo se activa si `OpenLit.Enabled = true` en la configuración.

## Tecnología
OpenLit usa OpenTelemetry estándar (OTLP). No hay SDK Go oficial de OpenLit, pero como el proyecto ya tiene OTel, se implementa:
- **Traces**: Spans por cada llamada LLM con GenAI semantic conventions
- **OTLP Exporter**: HTTP (`/v1/traces`) o gRPC al endpoint de OpenLit
- **Sem convenciones**: `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, etc.

---

## FASE 1: Configuración (Base de todo)

**Archivos a modificar**:
- `/www/MCP/Pando/pando/internal/config/config.go` — añadir `OpenLitConfig` struct y campo en `Config`
- `/www/MCP/Pando/pando/.pando.toml` — añadir sección `[OpenLit]` de ejemplo

**Struct a añadir en config.go**:
```go
type OpenLitConfig struct {
    Enabled         bool   `json:"enabled" toml:"Enabled"`
    Endpoint        string `json:"endpoint" toml:"Endpoint"`         // e.g. "http://localhost:4318"
    ServiceName     string `json:"serviceName" toml:"ServiceName"`   // e.g. "pando"
    Insecure        bool   `json:"insecure" toml:"Insecure"`         // skip TLS verify
    CustomHeaders   map[string]string `json:"customHeaders" toml:"CustomHeaders"` // auth headers
}
```

**En Config struct** (ya existe en config.go, ~413 líneas), añadir:
```go
OpenLit OpenLitConfig `json:"openlit,omitempty" toml:"OpenLit"`
```

**En .pando.toml** añadir sección:
```toml
[OpenLit]
Enabled = false
Endpoint = "http://localhost:4318"
ServiceName = "pando"
Insecure = true
```

**Defaults a añadir en la función de defaults** (busca `setDefaults` o similar en config.go):
- `Endpoint`: `"http://localhost:4318"` 
- `ServiceName`: `"pando"`
- `Insecure`: `true`

---

## FASE 2: Paquete de Observabilidad (depende de Fase 1)

**Crear**: `/www/MCP/Pando/pando/internal/observability/`

**Archivos a crear**:
- `observability.go` — inicialización del TracerProvider con OTLP exporter
- `genai.go` — helpers para GenAI semantic conventions
- `noop.go` — implementación noop cuando OpenLit está deshabilitado

**Dependencias nuevas a añadir en go.mod**:
```
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/sdk/trace
```
(verificar si ya están como indirectas)

**API del paquete**:
```go
// Inicializa el TracerProvider global con OTLP exporter
func Init(cfg config.OpenLitConfig, version string) (shutdown func(context.Context) error, err error)

// Devuelve un tracer para instrumentar LLM calls  
func Tracer() trace.Tracer

// Constantes de GenAI semantic conventions (OTel)
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

**Lógica de Init**:
1. Si `!cfg.Enabled`, registrar un NoopTracerProvider global y retornar noop shutdown
2. Crear OTLP HTTP exporter apuntando a `cfg.Endpoint + "/v1/traces"`
3. Configurar `resource` con `service.name = cfg.ServiceName`, `service.version`
4. Crear `TracerProvider` con BatchSpanProcessor
5. Registrar como global: `otel.SetTracerProvider(tp)`
6. Retornar función shutdown

---

## FASE 3: Instrumentación de Providers (depende de Fase 2)

**Crear**: `/www/MCP/Pando/pando/internal/llm/provider/instrumented.go`

**Patrón**: Decorator/Wrapper sobre la interfaz `Provider` existente:

```go
// Provider interface actual (provider.go):
type Provider interface {
    SendMessages(ctx, messages, tools) (*ProviderResponse, error)
    StreamResponse(ctx, messages, tools) <-chan ProviderEvent
    Model() models.Model
}

// Nuevo wrapper instrumentado:
type instrumentedProvider struct {
    inner  Provider
    tracer trace.Tracer
}

func NewInstrumentedProvider(inner Provider) Provider {
    if !observability.IsEnabled() {
        return inner // sin overhead si OpenLit está deshabilitado
    }
    return &instrumentedProvider{inner: inner, tracer: observability.Tracer()}
}
```

**Instrumentación de SendMessages**:
- Crear span: `"chat {model}"` (gen_ai.operation.name = "chat")
- Atributos en el span:
  - `gen_ai.system` = provider name (anthropic, openai, gemini, etc.)
  - `gen_ai.request.model` = model API name
  - `gen_ai.request.max_tokens` = maxTokens
  - `gen_ai.request.message_count` = len(messages)
  - `gen_ai.request.tool_count` = len(tools)
- En respuesta exitosa:
  - `gen_ai.usage.input_tokens`
  - `gen_ai.usage.output_tokens`
  - `gen_ai.response.finish_reasons`
  - `gen_ai.response.tool_calls_count`
- En error: `span.RecordError(err)`, `span.SetStatus(codes.Error, ...)`

**Instrumentación de StreamResponse**:
- Crear span al inicio del stream
- Acumular eventos del canal
- Al recibir EventComplete con ProviderResponse: añadir atributos de usage
- Al recibir EventError: RecordError
- Cerrar span cuando el canal se cierra

**Integración en NewProvider** (provider.go):
```go
func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
    // ... código existente ...
    p, err := createBaseProvider(providerName, clientOptions)
    if err != nil {
        return nil, err
    }
    return NewInstrumentedProvider(p), nil  // ← añadir esta línea
}
```

**Tool calls como eventos de span**:
```go
// Cuando se recibe EventToolUseStart en el stream:
span.AddEvent("gen_ai.tool.call", trace.WithAttributes(
    attribute.String("gen_ai.tool.name", toolCall.Name),
    attribute.String("gen_ai.tool.call.id", toolCall.ID),
))
```

---

## FASE 4a: TUI Settings (depende de Fase 1, paralela con 4b)

**Archivos a modificar**:
- `/www/MCP/Pando/pando/internal/tui/components/settings/settings.go`

**Añadir sección "OpenLit" al TUI de settings** con los campos:
- `Enabled` (checkbox/boolean)
- `Endpoint` (text, default: `http://localhost:4318`)
- `ServiceName` (text, default: `pando`)
- `Insecure` (checkbox/boolean)

**Cómo se hace**: En settings.go existe una función que construye las secciones. Añadir una nueva `Section` con `Title: "OpenLit Observability"` y los `Field`s correspondientes.

Busca el patrón de otras secciones como "Remembrances" o "Server" para seguir el mismo estilo.

El handler `SaveFieldMsg` en `page/settings.go` (ya existe) se encarga de persistir el campo modificado en config. Asegurarse de mapear los keys de los fields a las rutas correctas del Config.

---

## FASE 4b: API Web UI (depende de Fase 1, paralela con 4a)

**Archivos a modificar**:
- `/www/MCP/Pando/pando/internal/api/routes.go` — verificar si ya existe endpoint para servicios/observabilidad
- `/www/MCP/Pando/pando/internal/api/handlers_config.go` (o similar) — añadir soporte para `openlit` en los handlers de config

**Objetivo**: Que los endpoints `/api/v1/config/services` (o el que corresponda) devuelvan y acepten la config de OpenLit.

Buscar cómo están implementados otros servicios similares (Remembrances, Mesnada) en los handlers para seguir el mismo patrón.

Si hay un frontend React (`ui/web/src/`), añadir los campos de OpenLit al panel de configuración correspondiente.

---

## FASE 5: Integración en App (depende de Fases 2, 3, 4a, 4b)

**Archivos a modificar**:
- `/www/MCP/Pando/pando/internal/app/app.go` — inicializar observabilidad en startup
- `/www/MCP/Pando/pando/main.go` — gestionar shutdown de OTLP exporter

**En app.go** (función `New` o `Init`):
```go
import "github.com/digiogithub/pando/internal/observability"

// Después de cargar config:
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

**Graceful shutdown** (en app cleanup o main.go):
```go
if app.openlitShutdown != nil {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = app.openlitShutdown(ctx)
}
```

---

## Grafo de Dependencias

```
Fase 1 (Config)
    ├── Fase 2 (Observability pkg)
    │       └── Fase 3 (Provider instrumentation)
    │                   └── Fase 5 (App integration) ←──┐
    ├── Fase 4a (TUI Settings) ──────────────────────────┤
    └── Fase 4b (Web UI/API) ───────────────────────────┘
```

**Paralelización**:
- Fase 1 → lanzar Fase 2, 4a y 4b en PARALELO
- Fase 2 completa → lanzar Fase 3
- Fase 3 + 4a + 4b completas → lanzar Fase 5

---

## Notas de Implementación

1. **No hay SDK Go de OpenLit**: Se usa OTLP directo con GenAI semantic conventions (OTel Semconv v1.27+)
2. **Overhead mínimo**: Si `Enabled=false`, el wrapper retorna el provider original sin wrapping
3. **El proyecto YA TIENE OTel**: `go.opentelemetry.io/otel v1.35.0` — solo añadir el SDK y el OTLP exporter HTTP
4. **Config hot-reload**: El EventBus de config ya existe; si OpenLit se habilita en runtime, se puede reinicializar el TracerProvider
5. **Compatibilidad**: OpenLit acepta OTLP estándar en el puerto 4318 (HTTP) o 4317 (gRPC)
