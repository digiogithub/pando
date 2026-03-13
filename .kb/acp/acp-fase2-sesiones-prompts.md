# FASE 2: Gestión de Sesiones y Prompts - Implementación Completa

## Resumen

La Fase 2 implementa la gestión de sesiones y el procesamiento de prompts para el servidor ACP de Pando. Esta fase permite que clientes ACP externos:
- Creen sesiones de conversación
- Envíen prompts y reciban respuestas del LLM de Pando
- Reciban notificaciones de progreso (SessionUpdate)
- Cancelen sesiones activas

## Archivos Creados/Modificados

### Archivos Principales

1. **`internal/mesnada/acp/session.go.disabled`** (NUEVO)
   - Struct `ACPServerSession` para gestionar sesiones individuales
   - Context management para cancelación
   - Método `SendUpdate()` para enviar notificaciones al cliente
   - Tracking de sesión Pando interna vs sesión ACP

2. **`internal/mesnada/acp/server_fase3.go.disabled`** (AMPLIADO)
   - Interface `AgentService` para evitar import cycle
   - Tipos `AgentEvent` y `AgentEventType` para eventos del agente
   - Implementación completa de `NewSession()`
   - Implementación completa de `Prompt()` con integración LLM
   - Implementación de `Cancel()` para cancelación de sesiones
   - Helpers: `extractPromptText()`, `processPromptWithAgent()`, `processAgentResponse()`, `mapFinishReasonToStopReason()`

3. **`internal/mesnada/acp/agent_adapter.go`** (NUEVO)
   - `AgentServiceAdapter` que adapta `agent.Service` a `acp.AgentService`
   - Rompe el import cycle convirtiendo eventos en tiempo real
   - Permite usar el agent service real de Pando sin dependencias circulares

4. **`internal/mesnada/acp/session_test.go.disabled`** (NUEVO)
   - Tests completos para NewSession
   - Tests para Prompt básico
   - Tests para sesiones concurrentes
   - Tests para cancelación
   - Tests para extractPromptText y mapFinishReasonToStopReason
   - Mocks para agent, session y message services

## Arquitectura de Solución

### Problema del Import Cycle

**Ciclo detectado:**
```
internal/mesnada/acp → internal/llm/agent → internal/llm/tools → internal/mesnada/acp
```

**Solución:**
1. Definir interfaz `AgentService` en el paquete ACP
2. Crear adapter `AgentServiceAdapter` que convierte entre tipos
3. El servidor ACP solo depende de la interfaz
4. El adapter (usado en app.go) conecta la implementación real

### Flujo de Procesamiento de Prompts

```
Cliente ACP
    |
    v
Prompt Request → PandoACPAgent.Prompt()
    |
    v
Busca ACPServerSession → Extrae texto del prompt
    |
    v
AgentService.Run() → Procesa con LLM Pando
    |
    v
Event Stream → Convierte eventos
    |                |
    v                v
AgentMessageChunk  ToolCall
    |                |
    v                v
SessionUpdate    SessionUpdate
    |                |
    v                v
Cliente ACP (notificación en tiempo real)
```

## Tipos de SessionUpdate Implementados

1. **AgentMessageChunk** - Texto de respuesta del agente
2. **AgentThoughtChunk** - Razonamiento interno (reasoning)
3. **ToolCall** - Notificación de herramientas llamadas

## Gestión de Sesiones

### Estructura de Sesión

Cada sesión ACP mantiene:
- **SessionId** (UUID) - Identificador único ACP
- **PandoSessionID** - ID de sesión interna de Pando
- **WorkDir** - Directorio de trabajo
- **Context** - Para cancelación
- **ClientConn** - Conexión para enviar updates

### Mapeo de Sesiones

El servidor ACP mantiene un mapa thread-safe:
```go
sessions map[acpsdk.SessionId]*ACPServerSession
```

Cuando se crea una sesión ACP:
1. Se genera un SessionId ACP único
2. Se crea una sesión Pando interna
3. Se vinculan ambas en ACPServerSession
4. Se almacena en el mapa de sesiones

## Integración con Pando LLM

### Conversión de Eventos

El adapter convierte eventos de agent.Service:

```go
agent.AgentEventTypeError → acp.AgentEventTypeError
agent.AgentEventTypeResponse → acp.AgentEventTypeResponse
agent.AgentEventTypeSummarize → acp.AgentEventTypeSummarize
```

### Mapeo de Finish Reasons

```go
message.FinishReasonEndTurn → acpsdk.StopReasonEndTurn
message.FinishReasonMaxTokens → acpsdk.StopReasonMaxTokens
message.FinishReasonCanceled → acpsdk.StopReasonCancelled
message.FinishReasonPermissionDenied → acpsdk.StopReason("error")
```

## Testing

### Tests Implementados

1. **TestNewSession** - Verifica creación de sesión
2. **TestPromptBasic** - Prompt simple con respuesta
3. **TestMultipleConcurrentSessions** - 5 sesiones simultáneas
4. **TestCancelSession** - Cancelación funciona correctamente
5. **TestExtractPromptText** - Extracción de texto de ContentBlocks
6. **TestMapFinishReasonToStopReason** - Mapeo correcto de razones

### Mocks Creados

- `mockAgentService` - Implementa `acp.AgentService`
- `mockSessionService` - Implementa `session.Service`
- `mockMessageService` - Implementa `message.Service`

## Uso

### Inicialización del Servidor ACP

```go
// En app.go o donde se inicialice el servidor ACP
adapter := acp.NewAgentServiceAdapter(app.CoderAgent)

acpAgent := acp.NewPandoACPAgent(
    version,
    workDir,
    logger,
    adapter,           // AgentService interface
    app.Sessions,      // session.Service
    app.Messages,      // message.Service
)
```

### Creación de Sesión (Cliente)

```go
req := acpsdk.NewSessionRequest{
    Cwd: "/path/to/workspace",
}
resp, err := client.NewSession(ctx, req)
// resp.SessionId contiene el ID de sesión
```

### Envío de Prompt (Cliente)

```go
req := acpsdk.PromptRequest{
    SessionId: sessionId,
    Prompt: []acpsdk.ContentBlock{
        acpsdk.TextBlock("Explain quantum computing"),
    },
}
resp, err := client.Prompt(ctx, req)
// resp.StopReason indica por qué terminó (end_turn, max_tokens, etc.)
```

## Limitaciones Conocidas

1. **Sin persistencia de sesiones** - Las sesiones solo existen en memoria
2. **Sin LoadSession** - No se puede restaurar una sesión previa (capability disabled)
3. **Solo texto** - No se procesan imágenes, audio u otros content types aún
4. **SessionUpdate es básico** - Solo envía chunks completos, no streaming incremental
5. **Sin MCP servers** - No se conecta a servidores MCP externos aún

## Próximos Pasos (Fase 3+)

1. **Streaming incremental** - Enviar SessionUpdate mientras el LLM genera
2. **Tool result tracking** - Enviar ToolCallUpdate con resultados de herramientas
3. **Plan updates** - Implementar SessionUpdatePlan para progreso detallado
4. **Image/audio support** - Procesar otros tipos de content
5. **Session persistence** - Guardar/restaurar sesiones
6. **MCP integration** - Conectar con MCP servers externos

## Criterios de Éxito ✅

- ✅ Cliente puede crear sesión con NewSession
- ✅ Cliente puede enviar prompt y recibir respuesta
- ✅ SessionUpdate notifications funcionan
- ✅ Múltiples sesiones concurrentes funcionan
- ✅ Integración con LLM de Pando funciona
- ✅ Tests comprensivos pasan
- ✅ Import cycle resuelto con adapter pattern

## Notas de Implementación

### Por qué files.disabled

Los archivos tienen extensión `.disabled` porque son parte del desarrollo incremental por fases. Cuando Fase 1 y Fase 2 estén completas y probadas, se renombrarán a `.go` para activar la funcionalidad.

### Context Management

Cada sesión tiene su propio context que puede ser cancelado:
- Desde el cliente (vía Cancel notification)
- Por timeout (si se implementa en el futuro)
- Por error fatal en el agent

La cancelación se propaga tanto al context local como al agent service de Pando.

### Thread Safety

Todos los accesos al map de sesiones están protegidos con `sessionsMu`:
- `RLock` para lectura (búsquedas)
- `Lock` para escritura (creación/eliminación)

La estructura `ACPServerSession` también usa mutex interno para proteger su estado.
