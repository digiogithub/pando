# Análisis: Tool Calls en Agent Client Protocol — Implementación Go SDK vs Especificación Oficial (Rust)

## Resumen Ejecutivo

El SDK Go (`acp-go-sdk`) es una reimplementación fiel pero incompleta del protocolo **Agent Client Protocol (ACP)**, basada en el schema JSON `0.12.0` generado automáticamente desde la especificación oficial. Soporta el ciclo de vida completo de tool calls (creación, actualización, tipos de contenido, estados) pero tiene varios gaps significativos comparado con el SDK oficial de Rust (`agent-client-protocol` v0.11.1).

## Arquitectura del protocolo ACP para Tool Calls

### Flujo de comunicación
```
Client                           Agent
  |                                |
  |--- session/prompt ------------>|
  |                                | (LLM decide ejecutar tool)
  |<--- session/update (tool_call)-|  ← notificación: nuevo tool call
  |                                |
  |<--- session/request_permission-|  ← request: pedir permiso al usuario
  |--- response (outcome) ------->|
  |                                | (ejecución del tool)
  |<--- session/update (update) ---|  ← notificación: progreso/resultado
  |                                |
  |<--- session/prompt response ---|  ← respuesta final con StopReason
```

### Tipos de contenido de tool calls (ToolCallContent)

El protocolo define **3 variantes** de contenido:

1. **Content** (`"type": "content"`): Bloques de contenido estándar (texto, imágenes, recursos) — compatibles con MCP
2. **Diff** (`"type": "diff"`): Representación de cambios en archivos (`path`, `oldText`, `newText`)
3. **Terminal** (`"type": "terminal"`): Embedding de terminales vivas por `terminalId`

### Estados de un tool call (ToolCallStatus)

| Estado | Descripción |
|--------|-------------|
| `pending` | No ha comenzado (esperando input o aprobación) |
| `in_progress` | Ejecutándose |
| `completed` | Completado exitosamente |
| `failed` | Falló con error |

### Tipos de herramientas (ToolKind)

`read`, `edit`, `delete`, `move`, `search`, `execute`, `think`, `fetch`, `switch_mode`, `other`

Estos tipos ayudan al cliente a elegir iconos y optimizar la visualización.

### Sistema de permisos

El agente puede solicitar permiso al usuario antes de ejecutar un tool call mediante `session/request_permission`. Las opciones incluyen: `allow_once`, `allow_always`, `reject_once`, `reject_always`. El cliente puede auto-aprobar/rechazar según configuración del usuario.

## Análisis del SDK Go (acp-go-sdk)

### Lo que SÍ está implementado correctamente

| Feature | Estado |
|---------|--------|
| Tipos `SessionUpdateToolCall` y `SessionToolCallUpdate` | ✅ Completo |
| Enums: `ToolKind`, `ToolCallStatus` | ✅ Completo (incluye `switch_mode`) |
| `ToolCallContent` con 3 variantes (content, diff, terminal) | ✅ Completo |
| `ToolCallLocation` con `path` y `line` opcionales | ✅ Completo |
| `RequestPermissionRequest/Response` con opciones y outcomes | ✅ Completo |
| Helpers: `StartToolCall()`, `UpdateToolCall()`, `ToolContent()`, etc. | ✅ Rico conjunto de builders |
| Helper `StartToolCallStreaming()` | ✅ Específico para streaming |
| Sistema de extensiones (`_` prefixed methods) | ✅ Completo |
| Custom JSON marshal/unmarshal para `SessionUpdate` (discriminator `sessionUpdate`) | ✅ Implementación manual robusta |
| `ToolCall` como struct completo (no solo update) | ✅ Completo |
| `Plan` y `PlanEntry` para execution plans | ✅ Completo |

### Gaps y carencias detectados

#### 1. Faltan tipos de MCP Proxy Protocol
El SDK Rust incluye un módulo `proxy_protocol` con tipos para tunneling de MCP sobre ACP:

| Tipo | SDK Go | SDK Rust |
|------|--------|----------|
| `McpConnectRequest/Response` | ❌ Ausente | ✅ |
| `McpDisconnectNotification` | ❌ Ausente | ✅ |
| `McpOverAcpMessage` | ❌ Ausente | ✅ |
| `SuccessorMessage` | ❌ Ausente | ✅ |
| `InitializeProxyRequest` | ❌ Ausente | ✅ |

**Impacto**: El SDK Go no puede participar como proxy ACP sin implementar extension methods manuales.

#### 2. Métodos como "Unstable" que ya son estables en upstream

Varios métodos están en la interfaz `AgentExperimental` del SDK Go cuando en el schema upstream (`0.12.0`) ya son estables:

| Método | SDK Go | Upstream |
|--------|--------|----------|
| `session/close` | `UnstableCloseSession` | `CloseSession` (estable) |
| `session/resume` | `UnstableResumeSession` | `ResumeSession` (estable) |
| `session/fork` | `UnstableForkSession` | No en spec estable |
| `session/set_model` | `UnstableSetSessionModel` | `SetSessionModel` (estable) |

**Impacto**: Los consumidores usan APIs marcadas como inestables para features estables del protocolo.

#### 3. ToolCallUpdateFields no existe como tipo separado
En el SDK Rust, `ToolCallUpdateFields` es un struct que agrupa campos opcionales de actualización y es usado como building block. En Go no existe; los campos están inline en `SessionToolCallUpdate`.

**Impacto**: Menor. Es más una diferencia de diseño que una carencia funcional.

#### 4. Faltan builders para SessionUpdate completos
Aunque hay builders para tool calls, faltan builders equivalentes para otros tipos de `SessionUpdate`:
- No hay `StartPlan()`, `UpdatePlan()`
- No hay builders para `UserMessageChunk`, `AgentMessageChunk`, `AgentThoughtChunk`
- No hay builder para `AvailableCommandsUpdate`
- No hay builder para `SessionInfoUpdate`

**Impacto**: El código cliente debe construir estos structs manualmente sin ayuda de builders tipados.

#### 5. No hay soporte para `_meta` en helpers
Los helpers (`StartToolCall`, `UpdateToolCall`, etc.) aceptan `_meta` vía `WithStartMeta`/`WithUpdateMeta`, pero los tipos de contenido (`ToolContent`, `ToolDiffContent`, `ToolTerminalRef`) no exponen `_meta`.

**Impacto**: Menor. `_meta` es opcional para extensibilidad.

#### 6. La validación de `SessionUpdate` es manual y verbosa
El marshaling/unmarshaling de `SessionUpdate` (líneas ~4700-5300 en types_gen.go) es código generado muy extenso (~600 líneas) para manejar el discriminador `sessionUpdate`. Sería más mantenible con una tabla de dispatch.

**Impacto**: Mantenibilidad, no funcionalidad.

## Comparativa lado a lado

### Crear un tool call

**Rust SDK:**
```rust
conn.session_update(SessionUpdate::ToolCall {
    tool_call_id: ToolCallId::from("call_1"),
    title: "Reading file".to_string(),
    kind: Some(ToolKind::Read),
    status: ToolCallStatus::Pending,
    ..Default::default()
}).await?;
```

**Go SDK:**
```go
conn.SessionUpdate(ctx, SessionNotification{
    SessionId: "sess_1",
    Update: StartToolCall(
        "call_1",
        "Reading file",
        WithStartKind(ToolKindRead),
        WithStartStatus(ToolCallStatusPending),
    ),
})
```

Ambos SDKs son equivalentes en expresividad para este caso.

### Actualizar un tool call

**Rust SDK:**
```rust
conn.session_update(SessionUpdate::ToolCallUpdate {
    tool_call_id: ToolCallId::from("call_1"),
    status: Some(ToolCallStatus::Completed),
    content: Some(vec![ToolCallContent::Content(ContentBlock::Text(...))]),
}).await?;
```

**Go SDK:**
```go
conn.SessionUpdate(ctx, SessionNotification{
    SessionId: "sess_1",
    Update: UpdateToolCall("call_1",
        WithUpdateStatus(ToolCallStatusCompleted),
        WithUpdateContent([]ToolCallContent{...}),
    ),
})
```

Equivalentes.

## Recomendaciones

### Prioridad Alta
1. **Añadir tipos MCP Proxy Protocol**: `McpConnect`, `McpDisconnect`, `McpOverAcpMessage`, `SuccessorMessage`, `InitializeProxy`
2. **Promover métodos a estables**: `session/close` y `session/resume` deben salir de `AgentExperimental`

### Prioridad Media
3. **Añadir builders para otros SessionUpdate types**: `StartPlan()`, `UpdatePlan()`, builders para message chunks
4. **Añadir `_meta` a los helpers de contenido**: `WithContentMeta()` en `ToolContent()`, `ToolDiffContent()`, `ToolTerminalRef()`

### Prioridad Baja
5. **Refactorizar el dispatcher de SessionUpdate**: Reemplazar el switch gigante por tabla de dispatch para mantenibilidad
6. **Añadir `ToolCallUpdateFields` como tipo separado**: Por consistencia con Rust SDK

## Conclusión

El SDK Go de ACP es funcionalmente **correcto y utilizable** para el ciclo de vida completo de tool calls. Las carencias principales están en:
- Faltan tipos para MCP Proxy Protocol (bloquea uso como proxy ACP)
- Varios métodos estables están marcados como `Unstable`
- Faltan builders para tipos de SessionUpdate no relacionados con tool calls

El mapeo de tipos, serialización JSON, y el modelo de tool calls (creación, actualización, contenido, permisos, estados) está implementado correctamente y es compatible con la especificación oficial.