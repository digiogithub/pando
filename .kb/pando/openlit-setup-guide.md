# Configurar OpenLit con Pando

OpenLit añade observabilidad a todas las llamadas LLM de Pando: trazas por conversación, tokens consumidos, tool calls, latencia y proveedor. Es **opcional** — si no se configura, no tiene ningún impacto en el rendimiento.

## ¿Qué se captura?

Por cada llamada a un proveedor LLM (Anthropic, OpenAI, Copilot, Gemini, Ollama, etc.) se genera una **traza OpenTelemetry** con:

- `gen_ai.system` — proveedor (anthropic, openai, gemini…)
- `gen_ai.operation.name` — siempre `chat`
- `gen_ai.request.model` — modelo exacto usado
- `gen_ai.request.max_tokens` — límite de tokens configurado
- `gen_ai.request.message_count` — número de mensajes en el contexto
- `gen_ai.request.tool_count` — número de tools disponibles
- `gen_ai.usage.input_tokens` — tokens de entrada consumidos
- `gen_ai.usage.output_tokens` — tokens de salida generados
- `gen_ai.response.finish_reasons` — razón de fin (stop, tool_use, max_tokens…)
- `gen_ai.usage.cache_read_input_tokens` — tokens leídos de caché (Anthropic)
- `gen_ai.usage.cache_creation_input_tokens` — tokens escritos en caché (Anthropic)
- Eventos `gen_ai.tool.call` por cada tool call con `gen_ai.tool.name` y `gen_ai.tool.call.id`

Los datos se envían vía **OTLP HTTP** al servidor OpenLit.

---

## Levantar OpenLit (Docker)

```bash
docker run -d \
  --name openlit \
  -p 3000:3000 \
  -p 4318:4318 \
  -e INIT_DB_HOST=localhost \
  ghcr.io/openlit/openlit:latest
```

- Puerto **3000** → dashboard web (http://localhost:3000)
- Puerto **4318** → OTLP HTTP receiver (donde apunta Pando)

Credenciales por defecto: `user@openlit.io` / `openlituser@1`

Con Docker Compose:

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

## Configurar Pando

### Opción 1 — Archivo `.pando.toml`

Añade o edita la sección `[OpenLit]` en tu archivo de configuración:

```toml
[OpenLit]
Enabled = true
Endpoint = "http://localhost:4318"
ServiceName = "pando"
Insecure = true
```

Para un servidor OpenLit remoto con HTTPS y autenticación:

```toml
[OpenLit]
Enabled = true
Endpoint = "https://openlit.mi-empresa.com"
ServiceName = "pando-produccion"
Insecure = false

[OpenLit.CustomHeaders]
Authorization = "Bearer mi-api-key"
```

### Opción 2 — API REST (Web UI / programática)

```bash
# Ver configuración actual
curl http://localhost:8765/api/v1/config/openlit \
  -H "X-Pando-Token: TU_TOKEN"

# Activar OpenLit
curl -X POST http://localhost:8765/api/v1/config/openlit \
  -H "X-Pando-Token: TU_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "endpoint": "http://localhost:4318",
    "serviceName": "pando",
    "insecure": true
  }'
```

### Opción 3 — Panel de configuración TUI

En el TUI de Pando, ve a **Settings** (tecla `s` o desde el menú lateral) y busca la sección **"OpenLit Observability"**. Puedes activar/desactivar y cambiar el endpoint desde ahí sin reiniciar.

---

## Parámetros de configuración

| Campo | Tipo | Por defecto | Descripción |
|-------|------|-------------|-------------|
| `Enabled` | bool | `false` | Activa/desactiva la observabilidad |
| `Endpoint` | string | `http://localhost:4318` | URL base del servidor OpenLit (OTLP HTTP) |
| `ServiceName` | string | `pando` | Nombre del servicio en las trazas |
| `Insecure` | bool | `true` | Omitir verificación TLS (útil en local) |
| `CustomHeaders` | map | `{}` | Headers HTTP adicionales (autenticación, etc.) |

---

## Cómo funciona internamente

```
Pando (llamada LLM)
  └── instrumentedProvider (wrapper OTel)
        ├── Crea span con atributos GenAI
        ├── Llama al provider real (Anthropic, OpenAI, etc.)
        ├── Añade usage tokens y tool calls al span
        └── Envía traza vía OTLP HTTP → OpenLit :4318
```

El wrapper es un **decorator** sobre la interfaz `Provider`. Si `Enabled = false`, el wrapper no se aplica y no hay ningún overhead.

La inicialización ocurre en el arranque de la app (`internal/app/app.go`). El exporter OTLP hace flush de las trazas pendientes al cerrarse Pando (shutdown con timeout de 5s).

---

## Verificar que funciona

1. Abre el dashboard de OpenLit: http://localhost:3000
2. Inicia una conversación en Pando
3. En OpenLit verás las trazas aparecer con el nombre de operación `chat {modelo}` (ej: `chat claude-sonnet-4-6`)

Si no aparecen trazas, comprueba:
- Que `Enabled = true` en la config
- Que el servidor OpenLit esté accesible desde Pando: `curl http://localhost:4318/v1/traces`
- Los logs de Pando al arrancar — si hay error de conexión OTLP aparecerá un warning

---

## Conectar con otros backends OTLP

OpenLit es un backend OTLP estándar, pero puedes enviar las trazas de Pando a cualquier backend compatible:

| Backend | Endpoint |
|---------|----------|
| OpenLit local | `http://localhost:4318` |
| Jaeger | `http://localhost:4318` (con OTLP receiver habilitado) |
| Grafana Tempo | `http://localhost:4318` |
| New Relic | `https://otlp.nr-data.net:4318` (con API key en CustomHeaders) |
| Honeycomb | `https://api.honeycomb.io` (con API key en CustomHeaders) |

Para New Relic:
```toml
[OpenLit]
Enabled = true
Endpoint = "https://otlp.nr-data.net:4318"
ServiceName = "pando"
Insecure = false

[OpenLit.CustomHeaders]
"api-key" = "TU_NEW_RELIC_LICENSE_KEY"
```

---

## Referencia técnica

- Implementación: `internal/observability/observability.go`, `internal/observability/genai.go`
- Wrapper de providers: `internal/llm/provider/instrumented.go`
- Config struct: `internal/config/config.go` → `OpenLitConfig`
- API endpoint: `GET/POST /api/v1/config/openlit`
- Protocolo: OTLP HTTP (`/v1/traces`), compatible con OpenTelemetry Collector
- Semconv: OpenTelemetry GenAI Semantic Conventions v1.27
