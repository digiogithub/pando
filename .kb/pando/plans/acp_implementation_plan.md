# Plan de Implementación ACP para Pando

**Fecha:** 2026-03-30  
**Objetivo:** Conectar VS Code (y otros editores ACP) a Pando como agente ACP nativo via stdio.

---

## Diagnóstico del Problema

**Causa raíz:** Todos los editores compatibles con ACP (VS Code, Zed, JetBrains, avante.nvim) lanzan el agente como subproceso y se comunican via **stdio (JSON-RPC / ndJSON sobre stdin/stdout)**. La función `runACPServer()` en `cmd/root.go:377` devuelve: `"ACP stdio transport not yet implemented"`.

### Estado actual de la implementación ACP en Pando

| Archivo | Estado | Descripción |
|---------|--------|-------------|
| `internal/mesnada/acp/types.go` | ✅ Activo | Tipos base (ACPAgentConfig, ACPSession, etc.) |
| `internal/mesnada/acp/client.go` | ✅ Activo | MesnadaACPClient (Pando como cliente ACP) - Completo |
| `internal/mesnada/acp/client_connection.go` | ✅ Activo | Wrapper para callbacks al cliente |
| `internal/mesnada/acp/permissions.go` | ✅ Activo | Cola de permisos |
| `internal/mesnada/acp/transport_http.go` | ✅ Activo | Transporte HTTP/SSE |
| `internal/mesnada/acp/agent_interface.go` | ✅ Activo | Interfaz ACPAgent |
| `internal/mesnada/acp/agent_simple.go` | ✅ Activo | SimpleACPAgent (para tests) |
| `internal/mesnada/server/acp_handler.go` | ✅ Activo | Handler HTTP para servidor Mesnada |
| `internal/mesnada/acp/transport_stdio.go.disabled` | ❌ Deshabilitado | **Transporte stdio — NECESARIO PARA EDITORES** |
| `internal/mesnada/acp/agent_adapter.go.disabled` | ❌ Deshabilitado | Adaptador agent.Service → AgentService |
| `internal/mesnada/acp/session.go.disabled` | ❌ Deshabilitado | ACPServerSession type |
| `internal/mesnada/acp/server_fase3.go.disabled` | ❌ Deshabilitado | PandoACPAgent (agente real, parcialmente implementado) |
| `cmd/root.go:runACPServer()` | ❌ Stub | Retorna error, implementación comentada |

### Comparación con opencode (TypeScript)

opencode implementa ACP via `@agentclientprotocol/sdk` con:
- `AgentSideConnection` + `ndJsonStream` sobre stdin/stdout
- `ACP.Agent` class con todos los métodos del protocolo
- Event subscription para streaming en tiempo real
- `loadSession`, `listSessions`, `forkSession`, `resumeSession`
- `SetSessionModel`, `SetSessionMode`
- `writeTextFile` en el cliente al aprobar permisos de edición
- MCP servers del cliente pasados a opencode
- Usage updates con tokens y coste

---

## Fases de Implementación

### Fase 1: Activar Transporte Stdio (CAUSA RAÍZ)
**Fact:** `acp_plan_phase1_stdio_transport`  
**Prioridad:** CRÍTICA — sin esto ningún editor puede conectar

- Renombrar `transport_stdio.go.disabled` → `transport_stdio.go`
- Renombrar `agent_adapter.go.disabled` → `agent_adapter.go`
- Renombrar `session.go.disabled` → `session.go`
- Renombrar `server_fase3.go.disabled` → `server_fase3.go`
- Implementar `runACPServer()` en `cmd/root.go` usando la implementación comentada
- Inyectar dependencias reales: `agent.Service`, `session.Service`, `message.Service`

**Resultado:** `pando acp` funciona y acepta conexiones de editores

---

### Fase 2: PandoACPAgent Core
**Fact:** `acp_plan_phase2_pandoacpagent_core`  
**Prioridad:** ALTA

- Completar `Initialize()`: protocolo v1, capabilities básicas
- Completar `NewSession()`: crear sesión Pando real, mapear IDs
- Completar `Prompt()`: llamar a `agentService.Run()`, respuesta bloqueante
- Completar `Cancel()`: cancelar agent + contexto de sesión
- Stub `SetSessionMode()`: guardar modo, aplicar en Fase 5

**Resultado:** Se puede enviar un prompt y recibir respuesta (sin streaming)

---

### Fase 3: Event Subscription y Streaming
**Fact:** `acp_plan_phase3_event_streaming`  
**Prioridad:** ALTA — sin streaming la UX es pobre

- Modificar `Prompt()` para enviar actualizaciones mientras el agente corre
- Mapear eventos Pando → ACP session updates:
  - Texto → `agent_message_chunk`
  - Herramientas → `tool_call` / `tool_call_update`
  - TodoWrite → `plan` entries
  - Reasoning → `agent_thought_chunk`
- Mantener referencia a `AgentSideConnection` en `ACPServerSession`
- Mapear nombres de herramientas Pando → `ToolKind` ACP

**Resultado:** Streaming en tiempo real al editor (texto + tool calls + plan)

---

### Fase 4: Capacidades de Sesión Extendidas
**Fact:** `acp_plan_phase4_session_capabilities`  
**Prioridad:** MEDIA

- Habilitar `LoadSession: true` en capabilities
- Implementar `LoadSession()`: cargar sesión Pando existente
- Implementar `unstable_listSessions()`: listar sesiones
- Implementar `unstable_forkSession()`: bifurcar sesión
- Implementar `unstable_resumeSession()`: reanudar sesión
- Añadir campos `model`, `modeId`, `variant` a `ACPServerSession`
- Verificar/implementar `sessionService.Fork()` en Pando

**Resultado:** Clientes pueden cargar historial, listar y bifurcar sesiones

---

### Fase 5: Selección de Modelo y Modo
**Fact:** `acp_plan_phase5_model_mode_selection`  
**Prioridad:** MEDIA

- Completar `SetSessionMode()`: aplicar modo code/ask/architect en `agentService.Run()`
- Implementar `SetSessionModel()`: cambiar modelo por sesión (providerID/modelID)
- Retornar `availableModels` y `availableModes` en `NewSession`/`LoadSession` via `_meta`
- Pasar model override al `agentService.Run()` de cada sesión

**Resultado:** El editor puede cambiar modelo y modo desde su UI

---

### Fase 6: Capacidades Avanzadas
**Fact:** `acp_plan_phase6_advanced_capabilities`  
**Prioridad:** BAJA-MEDIA

- `writeTextFile` callback: escribir diffs en el cliente al aprobar ediciones
- MCP servers del cliente: registrar en Pando por sesión
- Usage updates: enviar tokens y coste tras cada prompt
- Soporte de imágenes en prompts (`PromptCapabilities.Image = true`)
- Contexto embebido (`EmbeddedContext = true`)
- Habilitar `McpCapabilities{Http: true, Sse: true}`

---

### Fase 7: Config, Docs y Tests
**Fact:** `acp_plan_phase7_config_docs_testing`  
**Prioridad:** MEDIA (después de Fase 1-3)

- Sección `[acp]` en `.pando.toml`
- Mejorar flags en `pando acp` (--cwd, --debug, --auto-permission)
- Documentar configuración para VS Code, Zed, JetBrains, avante.nvim
- Completar tests de integración en `test/e2e/acp_integration_test.go`
- Habilitar `.github/workflows/acp-test.yml`

---

## Orden de prioridad para MVP funcional

1. **Fase 1** → sin esto nada funciona
2. **Fase 2** → sin esto no hay prompt
3. **Fase 3** → sin esto mala UX (sin streaming)
4. **Fase 7 (parcial)** → tests básicos y docs
5. Fases 4, 5, 6 → mejoras incrementales

## Archivos clave

- `cmd/root.go:375-410` — `runACPServer()` a desbloquear
- `internal/mesnada/acp/server_fase3.go.disabled` — `PandoACPAgent` a completar
- `internal/mesnada/acp/transport_stdio.go.disabled` — transporte a activar
- `internal/mesnada/acp/session.go.disabled` — sesión ACP servidor
- `internal/mesnada/acp/agent_adapter.go.disabled` — adaptador agent.Service
- `internal/llm/agent/agent.go` — `agent.Service` a inyectar
